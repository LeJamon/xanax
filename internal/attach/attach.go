// Package attach is the raw terminal client that proxies a user's terminal to
// a session's PTY over its unix socket (SPEC.md §10). It runs as its own
// process: standalone for `rvr attach`/`new`, and shelled out to by the
// dashboard when entering interact mode. Owning the whole process means the
// terminal is always restored cleanly and no input reader leaks back into the
// dashboard.
package attach

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/LeJamon/rvr/internal/wire"
)

// resetModes returns the client's terminal to a sane state when the
// attachment ends. Terminals ignore the sequences they don't support.
//
// It must disable every input mode the harness may have enabled through the
// passthrough: the harness only turns these off when it exits, not on detach,
// so any one left set here leaks into the dashboard. The mouse group covers
// both the SGR (1006) and urxvt (1015) encodings.
var resetModes = []byte("" +
	"\x1b[<u\x1b[<u\x1b[<u" + // pop Kitty keyboard flags (no-op when stack empty)
	"\x1b[=0;1u" + //           force Kitty keyboard flags to zero
	"\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?1015l" + // mouse tracking off (SGR + urxvt)
	"\x1b[?2004l" + // bracketed paste off
	"\x1b[?1004l" + // focus reporting off
	"\x1b[?2026l" + // synchronized output off
	"\x1b[?1l" + //   application cursor keys off
	"\x1b[?1049l" + // leave the alternate screen
	"\x1b[0m\x1b[?25h") // reset attributes, show cursor

// enterSessionScreen gives each attach a private, blank screen before any
// replay or live output arrives. Without this, a slow-starting or line-based
// session can leave visible pixels from the dashboard or a previous session.
//
// Leave cursor visibility alone here: full-screen snapshots manage it while
// painting, and line-based sessions may not replay a later cursor-show sequence.
var enterSessionScreen = []byte("\x1b[?1049h\x1b[0m\x1b[2J\x1b[H")

// Result explains why Run returned.
type Result int

const (
	Detached      Result = iota // user pressed the interact-exit key
	SessionExited               // the harness ended
	Disconnected                // supervisor socket closed unexpectedly
)

const defaultExitKey byte = 0x11 // ctrl+q

// Options configures an attach session.
type Options struct {
	SocketPath string
	ExitKey    byte     // control byte that detaches (default ctrl+q, 0x11)
	In         *os.File // defaults to os.Stdin
	Out        *os.File // defaults to os.Stdout
}

// Run connects, puts the terminal in raw mode, and proxies until the user
// detaches or the session ends.
func Run(opts Options) (Result, error) {
	conn, err := net.Dial("unix", opts.SocketPath)
	if err != nil {
		return Disconnected, fmt.Errorf("connect to session: %w", err)
	}
	return runConnected(opts, conn)
}

func runConnected(opts Options, conn net.Conn) (Result, error) {
	defer conn.Close()
	if opts.In == nil {
		opts.In = os.Stdin
	}
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	if opts.ExitKey == 0 {
		opts.ExitKey = defaultExitKey
	}

	restore, err := makeRaw(opts.In)
	if err != nil {
		return Disconnected, err
	}
	defer restore()
	// The harness negotiates terminal modes with the client's terminal through
	// the passthrough (Kitty keyboard protocol, mouse tracking, bracketed
	// paste, alt screen, ...). It only disables them when *it* exits — not when
	// a client detaches — so without a reset they leak into whatever runs next.
	// (Leaked Kitty keyboard mode encodes Ctrl+C as "CSI 99;5u", which the
	// dashboard cannot recognize — quit appears broken.) This is what tmux does
	// to the client terminal on detach.
	defer opts.Out.Write(resetModes)
	opts.Out.Write(enterSessionScreen)

	sendResize(conn, opts.Out)
	stopWinch := watchWinch(conn, opts.Out)
	defer stopWinch()

	done := make(chan Result, 2)

	// Socket -> terminal, plus session-exit detection.
	go func() {
		for {
			f, err := wire.Read(conn)
			if err != nil {
				done <- Disconnected
				return
			}
			switch f.Type {
			case wire.TypeOutput:
				opts.Out.Write(f.Payload)
			case wire.TypeExit:
				done <- SessionExited
				return
			}
		}
	}()

	// Terminal -> socket, watching for a detach trigger.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := opts.In.Read(buf)
			if n > 0 {
				if i, _ := findDetach(buf[:n], opts.ExitKey); i >= 0 {
					if i > 0 {
						wire.Write(conn, wire.TypeInput, buf[:i])
					}
					wire.WriteJSON(conn, wire.TypeDetach, struct{}{})
					done <- Detached
					return
				}
				if err := wire.Write(conn, wire.TypeInput, buf[:n]); err != nil {
					done <- Disconnected
					return
				}
			}
			if err != nil {
				done <- Disconnected
				return
			}
		}
	}()

	return <-done, nil
}

// Kill connects to a session and requests termination without attaching.
func Kill(socketPath string) error {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return fmt.Errorf("connect to session: %w", err)
	}
	defer conn.Close()
	return wire.WriteJSON(conn, wire.TypeKill, struct{}{})
}

// Peek fetches a one-shot plain-text preview of a session's current screen
// without attaching. Returns "" on any error (dead supervisor, timeout).
func Peek(socketPath string, rows, cols int) string {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return ""
	}
	defer conn.Close()
	if wire.WriteJSON(conn, wire.TypeSnapshotReq, wire.Resize{Rows: uint16(rows), Cols: uint16(cols)}) != nil {
		return ""
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		f, err := wire.Read(conn)
		if err != nil {
			return ""
		}
		if f.Type == wire.TypeSnapshot {
			return string(f.Payload)
		}
	}
}

// Alive reports whether a session's supervisor is accepting connections.
func Alive(socketPath string) bool {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func makeRaw(in *os.File) (func(), error) {
	fd := int(in.Fd())
	if !term.IsTerminal(fd) {
		return func() {}, nil // piped input (tests); nothing to restore
	}
	old, err := term.MakeRaw(fd)
	if err != nil {
		return nil, fmt.Errorf("enter raw mode: %w", err)
	}
	return func() { term.Restore(fd, old) }, nil
}

func sendResize(conn net.Conn, out *os.File) {
	cols, rows, err := term.GetSize(int(out.Fd()))
	if err != nil {
		return
	}
	wire.WriteJSON(conn, wire.TypeResize, wire.Resize{Rows: uint16(rows), Cols: uint16(cols)})
}

func watchWinch(conn net.Conn, out *os.File) func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ch:
				sendResize(conn, out)
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}

// findDetach returns the offset and length of the earliest configured exit-key
// trigger in data, or (-1, 0) if none is present.
//
// The trigger is recognized in both its raw control-byte form and the CSI-u
// form emitted after a harness enables the Kitty keyboard protocol. Every other
// key, including every arrow encoding, is forwarded to the harness unchanged.
//
// The scan is left-to-right so the caller can forward the bytes preceding the
// trigger before detaching.
func findDetach(data []byte, exitKey byte) (idx, length int) {
	for i := range data {
		if data[i] == exitKey {
			return i, 1
		}
		if n := exitKeyDetachLen(data[i:], exitKey); n > 0 {
			return i, n
		}
	}
	return -1, 0
}

// exitKeyDetachLen reports the length of a Kitty keyboard-protocol CSI-u
// encoding of the configured exit key at the start of data, or 0. Under the
// disambiguate flag a control chord such as ctrl+q arrives as "ESC [ 113 ; 5 u"
// (codepoint ; 1+modifier bitmask) instead of the raw 0x11 byte the caller
// matches directly, so once a harness pushes the protocol the raw scan alone
// misses it. Only control-byte exit keys are re-encoded this way; a printable
// exit key keeps its literal byte.
func exitKeyDetachLen(data []byte, exitKey byte) int {
	if exitKey >= 0x20 {
		return 0
	}
	if len(data) < 4 || data[0] != 0x1b || data[1] != '[' {
		return 0
	}
	primary, i, ok := parseCSIInt(data, 2)
	if !ok {
		return 0
	}
	// Kitty can append the shifted and base-layout key codepoints to the
	// primary codepoint. The configured chord names the logical character, so
	// the primary stays authoritative (AZERTY ctrl+a must not become ctrl+q just
	// because that key occupies Q's position on a QWERTY layout).
	var shifted int
	var hasShifted bool
	if i < len(data) && data[i] == ':' {
		i++
		if i >= len(data) {
			return 0
		}
		if data[i] == ':' { // omitted shifted key, followed by base-layout key
			i++
			if _, i, ok = parseCSIInt(data, i); !ok {
				return 0
			}
		} else {
			if shifted, i, ok = parseCSIInt(data, i); !ok {
				return 0
			}
			hasShifted = true
			if i < len(data) && data[i] == ':' {
				i++
				if _, i, ok = parseCSIInt(data, i); !ok {
					return 0
				}
			}
		}
	}
	if i >= len(data) || data[i] != ';' {
		return 0
	}
	mods, i, ok := parseCSIInt(data, i+1)
	if !ok {
		return 0
	}
	event := 1
	if i < len(data) && data[i] == ':' {
		if event, i, ok = parseCSIInt(data, i+1); !ok {
			return 0
		}
	}
	if i >= len(data) || data[i] != 'u' {
		return 0
	}
	const (
		shiftBit    = 0b00000001
		ctrlBit     = 0b00000100
		capsLockBit = 0b01000000
		numLockBit  = 0b10000000
	)
	if mods < 1 || event != 1 {
		return 0
	}
	activeMods := mods - 1
	if hasShifted && activeMods&shiftBit == 0 {
		return 0 // a shifted alternate is valid only while Shift is held
	}

	expected := exitKeyCodepoint(exitKey)
	directMatch := primary == expected
	shiftedMatch := activeMods&shiftBit != 0 &&
		(hasShifted && shifted == expected || !hasShifted && matchesUSShiftedControl(exitKey, primary))
	allowedMods := ctrlBit | capsLockBit | numLockBit
	if shiftedMatch && !directMatch {
		allowedMods |= shiftBit
	}
	if activeMods&ctrlBit == 0 || activeMods & ^allowedMods != 0 || !directMatch && !shiftedMatch {
		return 0
	}
	return i + 1
}

// matchesUSShiftedControl covers the two accepted control characters whose
// common keys need Shift even when alternate-key reporting is not enabled.
func matchesUSShiftedControl(exitKey byte, primary int) bool {
	return exitKey == 0x1e && primary == '6' || // ctrl+^
		exitKey == 0x1f && primary == '-' // ctrl+_
}

func exitKeyCodepoint(exitKey byte) int {
	if exitKey >= 0x01 && exitKey <= 0x1a {
		return int('a' + exitKey - 1)
	}
	switch exitKey {
	case 0x1c:
		return '\\'
	case 0x1d:
		return ']'
	case 0x1e:
		return '^'
	case 0x1f:
		return '_'
	default:
		return -1
	}
}

// parseCSIInt reads a base-10 CSI parameter from data starting at i, returning
// its value, the index just past the digits, and whether any digit was present.
func parseCSIInt(data []byte, i int) (val, next int, ok bool) {
	const maxCSIParameter = 0x10ffff

	start := i
	for i < len(data) && data[i] >= '0' && data[i] <= '9' {
		digit := int(data[i] - '0')
		if val > (maxCSIParameter-digit)/10 {
			return 0, i, false
		}
		val = val*10 + digit
		i++
	}
	return val, i, i > start
}

const exitKeyAcceptedForms = `ctrl+a through ctrl+z except ctrl+h, ctrl+i, ctrl+j, ctrl+m; or ctrl+\, ctrl+], ctrl+^, ctrl+_ (the ctrl+ prefix may be omitted)`

// ParseExitKey converts a config key spec like "ctrl+q" to its control byte.
func ParseExitKey(spec string) (byte, error) {
	s := strings.ToLower(strings.TrimSpace(spec))
	key := strings.TrimPrefix(s, "ctrl+")
	if len(key) != 1 {
		return 0, fmt.Errorf("interact_exit_key %q is invalid (want %s)", spec, exitKeyAcceptedForms)
	}
	c := key[0]
	var b byte
	switch {
	case c >= 'a' && c <= 'z':
		b = c - 'a' + 1
	case c == '\\':
		b = 0x1c
	case c == ']':
		b = 0x1d
	case c == '^':
		b = 0x1e
	case c == '_':
		b = 0x1f
	default:
		return 0, fmt.Errorf("interact_exit_key %q is invalid (want %s)", spec, exitKeyAcceptedForms)
	}
	if name := unsafeExitKeyName(b); name != "" {
		return 0, fmt.Errorf("interact_exit_key %q maps to %s; choose a chord that will not detach during normal input", spec, name)
	}
	return b, nil
}

func unsafeExitKeyName(b byte) string {
	switch b {
	case '\r', '\n':
		return "Enter"
	case '\t':
		return "Tab"
	case '\b':
		return "Backspace"
	}
	return ""
}

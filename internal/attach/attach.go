// Package attach is the raw terminal client that proxies a user's terminal to
// a session's PTY over its unix socket (SPEC.md §10). It runs as its own
// process: standalone for `xanax attach`/`new`, and shelled out to by the
// dashboard when entering interact mode. Owning the whole process means the
// terminal is always restored cleanly and no input reader leaks back into the
// dashboard.
package attach

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"xanax/internal/wire"
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

// Options configures an attach session.
type Options struct {
	SocketPath string
	ExitKey    byte     // byte that detaches (default ctrl+\, 0x1c)
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
		opts.ExitKey = 0x1c
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

// findDetach returns the offset and length of the earliest detach trigger in
// data — the configured exit key or an unmodified Left-arrow key press — or
// (-1, 0) if none is present. Left returns the user to the dashboard.
//
// Both triggers are recognized in every encoding a harness may negotiate
// through the passthrough. Once a harness (codex) pushes the Kitty keyboard
// protocol (CSI > u), the terminal re-encodes the Left arrow as a parameterized
// CSI and a control chord like the exit key ctrl+\ as a CSI-u sequence
// (ESC[92;5u) rather than the raw 0x1c byte, so a raw-byte scan alone would miss
// both.
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
		if n := leftArrowDetachLen(data[i:]); n > 0 {
			return i, n
		}
	}
	return -1, 0
}

// leftArrowDetachLen reports the length of an *unmodified* Left-arrow key press
// at the start of data, or 0 if data does not begin with one.
//
// The Left arrow reaches the client in several encodings depending on the
// terminal mode the harness negotiated through the passthrough:
//
//	ESC O D                  SS3 / application-cursor-keys mode (DECCKM)
//	ESC [ D                  legacy CSI
//	ESC [ 1 D                Kitty form with the key code and no modifier
//	ESC [ 1 ; <m>[:<e>] D    Kitty keyboard protocol — harnesses like codex push
//	                         it (CSI > u); <m> is 1+modifier bitmask, <e> the
//	                         press(1)/repeat(2)/release(3) event.
//
// Only an unmodified press detaches. Left with a modifier (Ctrl/Alt/Shift) is
// passed through for word navigation and selection, and repeat/release events
// are ignored — otherwise a modifier released a beat before Left (which reports
// the Left release with no modifiers) would eject the user mid-edit. The key
// code, when present, must be the arrow's own code 1; an ANSI cursor-motion
// sequence such as ESC[5D (cursor-back-5, common in pasted output) shares the
// final 'D' but is not the Left key.
//
// A sequence still being assembled across a read boundary (no final byte yet)
// returns 0; the partial prefix is then forwarded to the harness and the detach
// is missed — an accepted limitation, as with the legacy 3-byte forms.
func leftArrowDetachLen(data []byte) int {
	if len(data) < 3 || data[0] != 0x1b {
		return 0
	}
	switch data[1] {
	case 'O': // SS3: ESC O D
		if data[2] == 'D' {
			return 3
		}
		return 0
	case '[': // CSI: ESC [ <params> D, params in [0-9;:]
		i := 2
		for i < len(data) && isCSIParam(data[i]) {
			i++
		}
		if i >= len(data) || data[i] != 'D' {
			return 0 // wrong final byte, or partial sequence
		}
		if !leftArrowUnmodifiedPress(data[2:i]) {
			return 0
		}
		return i + 1
	default:
		return 0
	}
}

func isCSIParam(b byte) bool {
	return (b >= '0' && b <= '9') || b == ';' || b == ':'
}

// leftArrowUnmodifiedPress reports whether a CSI cursor-key parameter list (the
// bytes between "ESC [" and the final "D") denotes the Left arrow with no
// modifiers held, on a key-press event.
//
// The list holds up to two ';'-separated fields — the key code and the modifier:
//   - Key code: the leading field, before any ':' alternate-key subfields. Empty
//     (legacy "ESC [ D") or "1" is the Left arrow; any other code is a different
//     key and never detaches.
//   - Modifier: the second field, before any ':' event subfield. It is
//     1+bitmask, so no modifier (ignoring lock states) means an empty field or 1.
//     The event subfield is press(1)/repeat(2)/release(3), defaulting to press;
//     only a press detaches.
func leftArrowUnmodifiedPress(params []byte) bool {
	keyField, modField, hasMod := bytes.Cut(params, []byte{';'})

	keyCode, _, _ := bytes.Cut(keyField, []byte{':'})
	if len(keyCode) != 0 && string(keyCode) != "1" {
		return false
	}
	if !hasMod {
		return true
	}

	// A trailing text field (ESC [ code ; mod ; text) never accompanies an
	// arrow; keep only the modifier field.
	modField, _, _ = bytes.Cut(modField, []byte{';'})
	mod, event, _ := bytes.Cut(modField, []byte{':'})
	if !modifiersClear(mod) {
		return false // a modifier is held -> pass through
	}
	return len(event) == 0 || string(event) == "1" // press only
}

// modifiersClear reports whether a Kitty modifier field (the value 1+bitmask, an
// empty field meaning none) carries no active modifier once lock states are
// ignored. Shift, Alt, Ctrl, Super, Hyper and Meta block a Left detach; the
// caps_lock (64) and num_lock (128) bits do not, so a plain Left still detaches
// with Num Lock on (which the terminal reports as ESC[1;129D).
func modifiersClear(mod []byte) bool {
	if len(mod) == 0 {
		return true
	}
	n, _, ok := parseCSIInt(mod, 0)
	if !ok || n < 1 {
		return false
	}
	const lockBits = 0b11000000 // caps_lock (64) | num_lock (128)
	return (n-1)&^lockBits == 0
}

// exitKeyDetachLen reports the length of a Kitty keyboard-protocol CSI-u
// encoding of the configured exit key at the start of data, or 0. Under the
// disambiguate flag a control chord such as ctrl+\ arrives as "ESC [ 92 ; 5 u"
// (codepoint ; 1+modifier bitmask) instead of the raw 0x1c byte the caller
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
	cp, i, ok := parseCSIInt(data, 2)
	if !ok || i >= len(data) || data[i] != ';' {
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
	const ctrlBit = 0b100 // Kitty modifier bit for Ctrl
	if mods < 1 || (mods-1)&ctrlBit == 0 || event != 1 {
		return 0 // the exit key needs Ctrl held, on a press event
	}
	// Ctrl+<cp> folds to a C0 control byte via cp&0x1f (matching ParseExitKey).
	if cp > 0x7f || byte(cp)&0x1f != exitKey {
		return 0
	}
	return i + 1
}

// parseCSIInt reads a base-10 CSI parameter from data starting at i, returning
// its value, the index just past the digits, and whether any digit was present.
func parseCSIInt(data []byte, i int) (val, next int, ok bool) {
	start := i
	for i < len(data) && data[i] >= '0' && data[i] <= '9' {
		val = val*10 + int(data[i]-'0')
		i++
	}
	return val, i, i > start
}

// ParseExitKey converts a config key spec like "ctrl+\" to its control byte.
// Unrecognized specs fall back to ctrl+\ (0x1c).
func ParseExitKey(spec string) byte {
	s := strings.ToLower(strings.TrimSpace(spec))
	s = strings.TrimPrefix(s, "ctrl+")
	if len(s) != 1 {
		return 0x1c
	}
	c := s[0]
	switch {
	case c >= 'a' && c <= 'z':
		return c - 'a' + 1
	case c == '\\':
		return 0x1c
	case c == ']':
		return 0x1d
	case c == '^':
		return 0x1e
	case c == '_':
		return 0x1f
	default:
		return 0x1c
	}
}

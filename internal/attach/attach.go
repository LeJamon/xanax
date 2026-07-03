// Package attach is the raw terminal client that proxies a user's terminal to
// a session's PTY over its unix socket (SPEC.md §10). It runs as its own
// process: standalone for `xanax attach`/`new`, and shelled out to by the
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
	if opts.In == nil {
		opts.In = os.Stdin
	}
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	if opts.ExitKey == 0 {
		opts.ExitKey = 0x1c
	}

	conn, err := net.Dial("unix", opts.SocketPath)
	if err != nil {
		return Disconnected, fmt.Errorf("connect to session: %w", err)
	}
	defer conn.Close()

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

// leftArrowSeqs are the byte sequences a terminal sends for the Left arrow, in
// normal (CSI) and application (SS3) cursor-key modes.
var leftArrowSeqs = [][]byte{{0x1b, '[', 'D'}, {0x1b, 'O', 'D'}}

// findDetach returns the offset and length of the earliest detach trigger in
// data — either the configured exit key byte or a Left-arrow sequence — or
// (-1, 0) if none is present. Left arrow returns the user to the dashboard.
func findDetach(data []byte, exitKey byte) (idx, length int) {
	best, bestLen := -1, 0
	if i := indexByte(data, exitKey); i >= 0 {
		best, bestLen = i, 1
	}
	for _, seq := range leftArrowSeqs {
		if i := bytesIndex(data, seq); i >= 0 && (best < 0 || i < best) {
			best, bestLen = i, len(seq)
		}
	}
	return best, bestLen
}

func indexByte(p []byte, b byte) int {
	for i, c := range p {
		if c == b {
			return i
		}
	}
	return -1
}

func bytesIndex(hay, needle []byte) int {
	if len(needle) == 0 || len(needle) > len(hay) {
		return -1
	}
	for i := 0; i+len(needle) <= len(hay); i++ {
		match := true
		for j := range needle {
			if hay[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
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

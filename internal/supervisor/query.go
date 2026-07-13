package supervisor

import (
	"bytes"
	"fmt"
	"strings"
)

// withStableTerm forces a widely-supported terminal type on the harness so it
// emits standard escape sequences regardless of where rvr was launched. A
// harness that inherits e.g. TERM=xterm-ghostty emits Ghostty-specific
// sequences that break in other terminals (and it runs inside a bare PTY, not a
// real emulator). xterm-256color + COLORTERM=truecolor keeps 24-bit color while
// staying portable.
func withStableTerm(env []string) []string {
	out := make([]string, 0, len(env)+2)
	for _, e := range env {
		if strings.HasPrefix(e, "TERM=") || strings.HasPrefix(e, "COLORTERM=") {
			continue
		}
		out = append(out, e)
	}
	return append(out, "TERM=xterm-256color", "COLORTERM=truecolor")
}

// Terminal capability queries that TUIs emit at startup and block on. When the
// harness launches there is no client attached yet, so nothing answers these —
// leaving apps like opencode to initialize with wrong assumptions and
// mis-render. The supervisor answers them itself while no client is attached;
// once a client attaches, its real terminal handles queries instead (so we
// avoid double replies).
// terminalQueryResponses returns the bytes to write back to the harness's stdin
// in reply to any capability queries found in chunk. Answering these lets a TUI
// that starts before any client attaches initialize instead of hanging or
// crashing while it waits. rows/cols answer a cursor-position report as if the
// cursor were at the far corner, which is how apps probe screen size via DSR.
func terminalQueryResponses(chunk []byte, rows, cols uint16) []byte {
	var out []byte
	if bytes.Contains(chunk, []byte("\x1b[6n")) { // DSR — cursor position (size probe)
		out = fmt.Appendf(out, "\x1b[%d;%dR", rows, cols)
	}
	if bytes.Contains(chunk, []byte("\x1b[5n")) { // DSR — device status
		out = append(out, "\x1b[0n"...) // OK
	}
	if bytes.Contains(chunk, []byte("\x1b[0c")) || bytes.Contains(chunk, []byte("\x1b[c")) {
		out = append(out, "\x1b[?1;2c"...) // primary DA: VT100 with advanced video
	}
	if bytes.Contains(chunk, []byte("\x1b[>0c")) || bytes.Contains(chunk, []byte("\x1b[>c")) {
		out = append(out, "\x1b[>0;10;1c"...) // secondary DA
	}
	if bytes.Contains(chunk, []byte("\x1b[?u")) { // Kitty keyboard protocol query
		out = append(out, "\x1b[?0u"...) // not supported
	}
	if bytes.Contains(chunk, []byte("\x1b[?2026$p")) { // DECRQM synchronized output
		out = append(out, "\x1b[?2026;2$y"...) // recognized, currently reset
	}
	if bytes.Contains(chunk, []byte("\x1b]11;?")) { // OSC 11 — background color
		out = append(out, "\x1b]11;rgb:1e1e/1e1e/1e1e\x1b\\"...) // dark
	}
	if bytes.Contains(chunk, []byte("\x1b]10;?")) { // OSC 10 — foreground color
		out = append(out, "\x1b]10;rgb:c0c0/c0c0/c0c0\x1b\\"...) // light
	}
	return out
}

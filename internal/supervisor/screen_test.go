package supervisor

import (
	"bytes"
	"strings"
	"testing"
)

func TestSnapshotReproducesText(t *testing.T) {
	sc := newScreen(20, 5)
	sc.write([]byte("\x1b[2;3HHELLO"))
	snap := string(sc.snapshot())

	if !strings.Contains(snap, "HELLO") {
		t.Errorf("snapshot missing text: %q", snap)
	}
	if !strings.HasPrefix(snap, "\x1b[?25l\x1b[0m\x1b[2J") {
		t.Errorf("snapshot must start hidden-cursor + reset + clear: %.60q", snap)
	}
	// Cursor ended after "HELLO" on row 2: col 3+5=8.
	if !strings.Contains(snap, "\x1b[2;8H") {
		t.Errorf("snapshot missing final cursor position: %q", snap)
	}
	if !strings.HasSuffix(snap, "\x1b[?25h") {
		t.Errorf("cursor visibility not restored: %.40q", snap[len(snap)-40:])
	}
}

func TestSnapshotCarriesColors(t *testing.T) {
	sc := newScreen(20, 3)
	sc.write([]byte("\x1b[1;1H\x1b[38;2;10;20;30m\x1b[48;5;196mX\x1b[0m"))
	snap := string(sc.snapshot())

	if !strings.Contains(snap, ";38;2;10;20;30") {
		t.Errorf("truecolor foreground lost: %q", snap)
	}
	if !strings.Contains(snap, ";48;5;196") {
		t.Errorf("palette background lost: %q", snap)
	}
}

func TestSnapshotCarriesAttributes(t *testing.T) {
	sc := newScreen(10, 2)
	sc.write([]byte("\x1b[1;1H\x1b[1;7mB\x1b[0m"))
	snap := string(sc.snapshot())
	// The bold+reverse cell must re-emit those attributes.
	if !strings.Contains(snap, ";1") || !strings.Contains(snap, ";7") {
		t.Errorf("bold/reverse attributes lost: %q", snap)
	}
}

func TestSnapshotAfterAltScreenAndClear(t *testing.T) {
	sc := newScreen(30, 4)
	// Simulate a full-screen TUI: enter alt screen, paint, position cursor.
	sc.write([]byte("\x1b[?1049h\x1b[2J\x1b[1;1HTOPBAR\x1b[4;1HSTATUS"))
	snap := sc.snapshot()

	if !bytes.Contains(snap, []byte("TOPBAR")) || !bytes.Contains(snap, []byte("STATUS")) {
		t.Errorf("alt-screen content missing from snapshot: %q", snap)
	}
	// The snapshot is rendered state, never raw sequences from the app.
	if bytes.Contains(snap, []byte("\x1b[?1049h")) {
		t.Errorf("snapshot leaked raw alt-screen sequence: %q", snap)
	}
}

func TestPreviewText(t *testing.T) {
	sc := newScreen(40, 10)
	sc.write([]byte("\x1b[3;1HLINE-A\x1b[4;1HLINE-B\x1b[5;1HLINE-C"))
	got := sc.previewText(2, 40) // last 2 non-empty rows
	if got != "LINE-B\nLINE-C" {
		t.Errorf("previewText = %q, want last two lines", got)
	}
	// Plain text only — no escape sequences.
	if bytes.Contains([]byte(got), []byte("\x1b")) {
		t.Errorf("preview leaked escape sequences: %q", got)
	}
}

func TestPreviewTextTruncatesWidth(t *testing.T) {
	sc := newScreen(40, 5)
	sc.write([]byte("\x1b[1;1H0123456789ABCDEFGHIJ"))
	got := sc.previewText(1, 6)
	if got != "012345" {
		t.Errorf("previewText width-limited = %q, want 012345", got)
	}
}

func TestPreviewEmptyScreen(t *testing.T) {
	sc := newScreen(20, 5)
	if got := sc.previewText(4, 20); got != "" {
		t.Errorf("blank screen preview = %q, want empty", got)
	}
}

func TestSnapshotResize(t *testing.T) {
	sc := newScreen(10, 3)
	sc.write([]byte("AB"))
	sc.resize(20, 6)
	snap := string(sc.snapshot())
	// After resize, all 6 rows must be addressed.
	if !strings.Contains(snap, "\x1b[6;1H") {
		t.Errorf("snapshot not rendered at new size: %q", snap)
	}
}

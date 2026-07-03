package supervisor

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/hinshun/vt10x"
)

// screen is a server-side terminal emulator fed with everything the harness
// writes. Full-screen TUIs render differentially against their own internal
// frame buffer, so a newly attached client cannot rely on the app repainting —
// instead we replay an exact snapshot of the current screen (the tmux
// approach), after which the app's diffs apply cleanly (SPEC.md §4).
type screen struct {
	term vt10x.Terminal
}

// vt10x keeps attribute bits on Glyph.Mode but does not export the constants;
// these mirror the pinned version (state.go: reverse, underline, bold, gfx,
// italic, blink in iota order).
const (
	vtAttrReverse   = 1 << 0
	vtAttrUnderline = 1 << 1
	vtAttrBold      = 1 << 2
	vtAttrItalic    = 1 << 4
	vtAttrBlink     = 1 << 5

	vtSGRBits = vtAttrReverse | vtAttrUnderline | vtAttrBold | vtAttrItalic | vtAttrBlink
)

func newScreen(cols, rows int) *screen {
	return &screen{term: vt10x.New(vt10x.WithSize(cols, rows))}
}

// write feeds harness output into the emulator. vt10x locks internally.
func (s *screen) write(p []byte) {
	_, _ = s.term.Write(p)
}

func (s *screen) resize(cols, rows int) {
	s.term.Resize(cols, rows)
}

// snapshot renders the current screen as an escape stream that reproduces it
// on a blank terminal: reset, clear, paint every cell (so background fills are
// included), then restore cursor position and visibility.
func (s *screen) snapshot() []byte {
	s.term.Lock()
	defer s.term.Unlock()

	cols, rows := s.term.Size()
	var b bytes.Buffer
	b.Grow(cols * rows * 4)
	b.WriteString("\x1b[?25l\x1b[0m\x1b[2J")

	var last vt10x.Glyph
	haveLast := false
	for y := range rows {
		fmt.Fprintf(&b, "\x1b[%d;1H", y+1)
		for x := range cols {
			g := s.term.Cell(x, y)
			if !haveLast || g.FG != last.FG || g.BG != last.BG ||
				g.Mode&vtSGRBits != last.Mode&vtSGRBits {
				writeSGR(&b, g)
				last, haveLast = g, true
			}
			r := g.Char
			if r == 0 {
				r = ' '
			}
			b.WriteRune(r)
		}
	}

	b.WriteString("\x1b[0m")
	cur := s.term.Cursor()
	fmt.Fprintf(&b, "\x1b[%d;%dH", cur.Y+1, cur.X+1)
	if s.term.CursorVisible() {
		b.WriteString("\x1b[?25h")
	}
	return b.Bytes()
}

// writeSGR emits the full attribute state for a cell (starting from reset, so
// no stale attributes leak between runs of cells).
func writeSGR(b *bytes.Buffer, g vt10x.Glyph) {
	b.WriteString("\x1b[0")
	if g.Mode&vtAttrBold != 0 {
		b.WriteString(";1")
	}
	if g.Mode&vtAttrItalic != 0 {
		b.WriteString(";3")
	}
	if g.Mode&vtAttrUnderline != 0 {
		b.WriteString(";4")
	}
	if g.Mode&vtAttrBlink != 0 {
		b.WriteString(";5")
	}
	if g.Mode&vtAttrReverse != 0 {
		b.WriteString(";7")
	}
	writeSGRColor(b, g.FG, false)
	writeSGRColor(b, g.BG, true)
	b.WriteByte('m')
}

// previewText renders a plain-text (no color) preview of the most recent
// activity: the block of rows ending at the last non-empty row, at most
// maxRows tall and maxCols wide. Used for the dashboard's peek pane, where a
// small colorless excerpt is enough and trivially fits any width.
func (s *screen) previewText(maxRows, maxCols int) string {
	s.term.Lock()
	defer s.term.Unlock()

	cols, rows := s.term.Size()
	if maxCols <= 0 || maxCols > cols {
		maxCols = cols
	}
	lines := make([]string, rows)
	lastNonEmpty := -1
	for y := range rows {
		var sb strings.Builder
		for x := range maxCols {
			r := s.term.Cell(x, y).Char
			if r == 0 {
				r = ' '
			}
			sb.WriteRune(r)
		}
		lines[y] = strings.TrimRight(sb.String(), " ")
		if lines[y] != "" {
			lastNonEmpty = y
		}
	}
	if lastNonEmpty < 0 {
		return ""
	}
	start := max(0, lastNonEmpty-maxRows+1)
	return strings.Join(lines[start:lastNonEmpty+1], "\n")
}

// writeSGRColor appends the color clause. vt10x packs colors as: 0-255 palette,
// values below 1<<24 truecolor (r<<16|g<<8|b), and dedicated default markers.
func writeSGRColor(b *bytes.Buffer, c vt10x.Color, bg bool) {
	base := 38
	def := ";39"
	if bg {
		base = 48
		def = ";49"
	}
	switch {
	case c == vt10x.DefaultFG, c == vt10x.DefaultBG:
		b.WriteString(def)
	case c < 256:
		fmt.Fprintf(b, ";%d;5;%d", base, c)
	case c < 1<<24:
		fmt.Fprintf(b, ";%d;2;%d;%d;%d", base, (c>>16)&0xff, (c>>8)&0xff, c&0xff)
	default:
		b.WriteString(def)
	}
}

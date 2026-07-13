package termview

import (
	"bytes"
	"strings"

	"github.com/hinshun/vt10x"
)

// PreviewBytes replays PTY output into a VT emulator and returns a plain-text
// excerpt of the final screen. It is meant for read-only previews of stored raw
// logs, where replaying escape sequences directly would be unreadable.
func PreviewBytes(p []byte, termRows, maxRows, maxCols int) string {
	if maxRows <= 0 {
		return ""
	}
	if maxCols <= 0 {
		maxCols = 80
	}
	if termRows < maxRows {
		termRows = maxRows
	}
	sc := New(maxCols, termRows)
	sc.Write(finalScreenBytes(p))
	return sc.PreviewText(maxRows, maxCols)
}

var altScreenLeaveSequences = [...][]byte{
	[]byte("\x1b[?1049l"),
	[]byte("\x1b[?1047l"),
	[]byte("\x1b[?47l"),
}

var altScreenEnterSequences = [...][]byte{
	[]byte("\x1b[?1049h"),
	[]byte("\x1b[?1047h"),
	[]byte("\x1b[?47h"),
}

// finalScreenBytes preserves the last frame drawn by a full-screen program.
// Replaying an alternate-screen leave switches the emulator back to the old
// primary buffer, hiding the conversation the preview is meant to show. Any
// output after that leave belongs to the restored parent terminal rather than
// the completed full-screen session.
func finalScreenBytes(p []byte) []byte {
	lastLeave := lastSequenceIndex(p, altScreenLeaveSequences[:])
	lastEnter := lastSequenceIndex(p, altScreenEnterSequences[:])
	if lastLeave >= 0 && lastLeave > lastEnter {
		return p[:lastLeave]
	}
	return p
}

func lastSequenceIndex(p []byte, sequences [][]byte) int {
	last := -1
	for _, seq := range sequences {
		if i := bytes.LastIndex(p, seq); i > last {
			last = i
		}
	}
	return last
}

type Screen struct {
	term vt10x.Terminal
}

func New(cols, rows int) *Screen {
	return &Screen{term: vt10x.New(vt10x.WithSize(max(1, cols), max(1, rows)))}
}

func (s *Screen) Write(p []byte) {
	_, _ = s.term.Write(p)
}

func (s *Screen) PreviewText(maxRows, maxCols int) string {
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

package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/LeJamon/rvr/internal/session"
)

// lineWidths returns the rendered (ANSI-aware) width of each line in s.
func maxLineWidth(s string) int {
	max := 0
	for ln := range strings.SplitSeq(s, "\n") {
		if w := lipgloss.Width(ln); w > max {
			max = w
		}
	}
	return max
}

// countsLineIndex returns the index of the header line carrying the tallies.
func countsLineIndex(header string) int {
	for i, ln := range strings.Split(header, "\n") {
		if strings.Contains(ln, "awaiting input") {
			return i
		}
	}
	return -1
}

// TestViewNoLineExceedsWidth is the regression guard for the bottom-pin: no
// rendered line may be wider than the terminal, or it would soft-wrap into
// extra physical rows that lipgloss.Height can't see, making the gap math
// push the pinned footer off the bottom of the screen. A long error, path,
// and filter — each appended to the single header counts line — are the
// classic triggers.
func TestViewNoLineExceedsWidth(t *testing.T) {
	m := newTestModel(sampleSessions())
	m.width, m.height = 80, 24
	m.path = "~/very/deeply/nested/working/directory/that/is/quite/long/indeed/rvr"
	m.filter = "some-long-filter-string-that-eats-columns"
	m.err = errors.New(`launch failed: exec: "claude": executable file not found in $PATH`)

	view := m.View()
	if w := maxLineWidth(view); w > m.width {
		t.Fatalf("a line is %d cols wide, exceeds width %d — it will soft-wrap and break the pin", w, m.width)
	}
	// Short list + fixed height: the gap fills so the frame is exactly height
	// rows, with the footer pinned to the last one.
	if h := lipgloss.Height(view); h != m.height {
		t.Fatalf("view height = %d, want %d (footer not pinned to bottom row)", h, m.height)
	}
	if last := lastLine(view); !strings.Contains(last, "quit") {
		t.Errorf("last line is not the footer: %q", last)
	}
}

func lastLine(s string) string {
	lines := strings.Split(s, "\n")
	return lines[len(lines)-1]
}

// TestHeaderCountsAccountForEverySession guards that the masthead tally
// reconciles with the list: failed/cancelled/other sessions must be counted,
// not silently dropped.
func TestHeaderCountsAccountForEverySession(t *testing.T) {
	now := time.Now()
	sessions := []*session.Session{
		{ID: "wait0001", Title: "a", Status: session.StatusWaiting, CreatedAt: now},
		{ID: "idle0001", Title: "idle", Status: session.StatusIdle, CreatedAt: now},
		{ID: "fail0001", Title: "b", Status: session.StatusFailed, CreatedAt: now},
		{ID: "fail0002", Title: "c", Status: session.StatusFailed, CreatedAt: now},
		{ID: "canc0001", Title: "d", Status: session.StatusCancelled, CreatedAt: now},
	}
	m := newTestModel(sessions)
	c := m.counts()
	for _, want := range []string{"1 awaiting input", "1 idle", "0 working", "0 completed", "2 failed", "1 cancelled"} {
		if !strings.Contains(stripANSI(c), want) {
			t.Errorf("counts %q missing %q", stripANSI(c), want)
		}
	}
}

// TestHeaderHidesEmptyBuckets keeps the common case uncluttered: cancelled /
// failed / other segments appear only when non-empty.
func TestHeaderHidesEmptyBuckets(t *testing.T) {
	c := stripANSI(newTestModel(sampleSessions()).counts())
	for _, absent := range []string{"failed", "cancelled", "other"} {
		if strings.Contains(c, absent) {
			t.Errorf("counts %q should not mention %q when that bucket is empty", c, absent)
		}
	}
}

// TestHeaderOmitsEmptyPathRow guards finding #3: when the path is unknown
// (unscoped and os.Getwd failed) the masthead must not render a blank middle
// line — the counts move up to sit directly under the title.
func TestHeaderOmitsEmptyPathRow(t *testing.T) {
	withPath := newTestModel(sampleSessions())
	withPath.path = "~/proj"
	if got := countsLineIndex(withPath.header()); got != 2 {
		t.Errorf("with a path, counts should be on line 2, got line %d", got)
	}

	noPath := newTestModel(sampleSessions())
	noPath.path = ""
	if got := countsLineIndex(noPath.header()); got != 1 {
		t.Errorf("with no path, counts should move up to line 1 (no blank row), got line %d", got)
	}
}

// stripANSI removes SGR escape sequences so assertions can match visible text.
func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			inEsc = true
		case inEsc && r == 'm':
			inEsc = false
		case !inEsc:
			b.WriteRune(r)
		}
	}
	return b.String()
}

package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func newComposerTestModel(t *testing.T, width, height int) model {
	t.Helper()
	m := newTestModel(nil)
	m.composer = newComposer()
	next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return next.(model)
}

// TestComposerHeightGrowsAndClamps exercises syncComposerHeight directly: the
// box is one line by default, grows a line per wrapped/hard-broken row, and is
// capped so it can never crowd out the session list.
func TestComposerHeightGrowsAndClamps(t *testing.T) {
	m := newComposerTestModel(t, 120, 40) // ceiling is min(10, 40/3) = 10

	// Empty: a single line — the default.
	m.syncComposerHeight()
	if got := m.composer.Height(); got != 1 {
		t.Errorf("empty composer height = %d, want 1", got)
	}

	// Three hard-broken short lines → three rows.
	m.composer.SetValue("one\ntwo\nthree")
	m.syncComposerHeight()
	if got := m.composer.Height(); got != 3 {
		t.Errorf("three-line composer height = %d, want 3", got)
	}

	// A single long line that overflows the width wraps to more than one row.
	m.composer.SetValue(strings.Repeat("x", 400))
	m.syncComposerHeight()
	if got := m.composer.Height(); got <= 1 {
		t.Errorf("wrapped long line height = %d, want > 1", got)
	}

	// Far more content than the ceiling is clamped, not unbounded.
	m.composer.SetValue(strings.Repeat("line\n", 40))
	m.syncComposerHeight()
	if got := m.composer.Height(); got != 10 {
		t.Errorf("overflowing composer height = %d, want clamped to 10", got)
	}
}

// TestComposerHeightWiredThroughUpdate verifies the box grows as the user types
// and snaps back to one line once a launch clears it — i.e. syncComposerHeight
// is actually invoked on the keypress and reset paths.
func TestComposerHeightWiredThroughUpdate(t *testing.T) {
	m := newComposerTestModel(t, 120, 40)
	if got := m.composer.Height(); got != 1 {
		t.Fatalf("composer height after resize with no text = %d, want 1", got)
	}

	// Type a prompt long enough to wrap; the box must grow.
	for range 250 {
		m = send(m, "x")
	}
	if got := m.composer.Height(); got <= 1 {
		t.Fatalf("composer did not grow while typing a long prompt: height = %d", got)
	}

	// Enter launches and clears — the box returns to a single line.
	next, cmd := m.Update(key("enter"))
	m = next.(model)
	if cmd == nil {
		t.Fatal("enter with a prompt returned no launch command")
	}
	if m.composer.Value() != "" {
		t.Errorf("composer not cleared after launch: %q", m.composer.Value())
	}
	if got := m.composer.Height(); got != 1 {
		t.Errorf("composer did not shrink back after launch: height = %d, want 1", got)
	}
}

func TestComposerHeightMatchesTextareaWrapping(t *testing.T) {
	m := newComposerTestModel(t, 30, 30)
	m.composer.SetWidth(10)

	for _, tc := range []struct {
		name  string
		value string
		want  int
	}{
		{name: "exact width moves cursor to next row", value: strings.Repeat("x", 10), want: 2},
		{name: "natural word wrapping", value: "aaaaaa bbbbbb cccccc", want: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m.composer.SetValue(tc.value)
			m.syncComposerHeight()
			if got := m.composer.Height(); got != tc.want {
				t.Errorf("composer height = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestComposerGrowthKeepsTypedPromptVisible(t *testing.T) {
	m := newComposerTestModel(t, 32, 30)
	prompt := "PREFIX alpha beta gamma delta epsilon zeta eta theta iota kappa lambda SUFFIX"

	// Bubble Tea renders after every update. Rendering here is essential because
	// textarea uses the previous View to maintain its viewport scroll position.
	for _, r := range prompt {
		m = send(m, string(r))
		_ = m.View()
	}

	if got := m.composer.Height(); got != 3 {
		t.Fatalf("composer height = %d, want 3", got)
	}
	view := stripANSI(m.composer.View())
	for _, marker := range []string{"PREFIX", "SUFFIX"} {
		if !strings.Contains(view, marker) {
			t.Errorf("expanded composer hid %q:\n%s", marker, view)
		}
	}
}

func TestComposerHeightChangePreservesCursor(t *testing.T) {
	m := newComposerTestModel(t, 22, 30)
	m.composer.SetValue("PREFIX alpha beta gamma delta SUFFIX")
	m.composer.SetCursor(len("PREFIX"))

	m.syncComposerHeight()
	info := m.composer.LineInfo()
	if got := info.StartColumn + info.ColumnOffset; got != len("PREFIX") {
		t.Fatalf("cursor column after growth = %d, want %d", got, len("PREFIX"))
	}
	m = send(m, "!")
	if got := m.composer.Value(); got != "PREFIX! alpha beta gamma delta SUFFIX" {
		t.Errorf("typing after growth used the wrong cursor position: %q", got)
	}
}

func TestComposerResizePreservesCursorInEarlierWideLine(t *testing.T) {
	m := newComposerTestModel(t, 32, 30)
	prefix := "界e\u0301"
	value := prefix + " alpha beta gamma\nSECOND delta epsilon zeta"
	m.composer.SetValue(value)
	m.composer.CursorStart()
	m.composer.CursorUp()
	column := len([]rune(prefix))
	m.composer.SetCursor(column)

	next, _ := m.Update(tea.WindowSizeMsg{Width: 22, Height: 30})
	m = next.(model)
	info := m.composer.LineInfo()
	if m.composer.Line() != 0 {
		t.Fatalf("cursor logical line after resize = %d, want 0", m.composer.Line())
	}
	if got := info.StartColumn + info.ColumnOffset; got != column {
		t.Fatalf("cursor column after resize = %d, want %d", got, column)
	}
	m = send(m, "!")
	if got := m.composer.Value(); got != prefix+"! alpha beta gamma\nSECOND delta epsilon zeta" {
		t.Errorf("typing after resize used the wrong cursor position: %q", got)
	}
}

func TestComposerPasteAtHeightCapShowsCursor(t *testing.T) {
	m := newComposerTestModel(t, 22, 15) // height cap is five rows
	m = send(m, "line01\nline02\nline03\nline04\nline05")
	_ = m.composer.View()

	m = send(m, "\nline06\nline07\nline08\nline09\nline10\nline11\nline12\nline13\nline14\nline15\nTAILMARKER")
	view := stripANSI(m.composer.View())
	if got := m.composer.Height(); got != 5 {
		t.Fatalf("composer height = %d, want capped height 5", got)
	}
	if !strings.Contains(view, "TAILMARKER") {
		t.Errorf("composer did not scroll a large paste to its cursor:\n%s", view)
	}
}

func TestComposerReflowsOnResize(t *testing.T) {
	m := newComposerTestModel(t, 32, 30)
	_ = m.View()
	m = send(m, "aaaaaaaaaaa bbbbbbbbbbb ccccccccccc")

	for _, tc := range []struct {
		width int
		want  int
	}{
		{width: 32, want: 2},
		{width: 22, want: 3},
		{width: 32, want: 2},
	} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: tc.width, Height: 30})
		m = next.(model)
		view := stripANSI(m.composer.View())
		if got := m.composer.Height(); got != tc.want {
			t.Errorf("terminal width %d: composer height = %d, want %d", tc.width, got, tc.want)
		}
		for _, marker := range []string{"aaaaaaaaaaa", "ccccccccccc"} {
			if !strings.Contains(view, marker) {
				t.Errorf("terminal width %d: reflowed composer hid %q:\n%s", tc.width, marker, view)
			}
		}
	}
}

func TestComposerRestoredPromptResyncsHeight(t *testing.T) {
	m := newComposerTestModel(t, 22, 30)
	_ = m.View()

	next, _ := m.Update(actionDoneMsg{
		status:        "launch failed: boom",
		restorePrompt: "aaaaaaaaaaa bbbbbbbbbbb ccccccccccc",
	})
	m = next.(model)
	view := stripANSI(m.composer.View())

	if got := m.composer.Height(); got != 3 {
		t.Errorf("restored composer height = %d, want 3", got)
	}
	for _, marker := range []string{"aaaaaaaaaaa", "ccccccccccc"} {
		if !strings.Contains(view, marker) {
			t.Errorf("restored composer hid %q:\n%s", marker, view)
		}
	}
}

func TestComposerWidthTracksNarrowTerminal(t *testing.T) {
	for _, terminalWidth := range []int{8, 12, 21} {
		m := newComposerTestModel(t, terminalWidth, 9)
		want := max(1, terminalWidth-2)
		if got := m.composer.Width(); got != want {
			t.Errorf("terminal width %d: composer width = %d, want %d", terminalWidth, got, want)
		}
		m.composer.SetValue("narrow terminal prompt")
		m.syncComposerHeight()
		if got := maxLineWidth(m.View()); got > terminalWidth {
			t.Errorf("terminal width %d: rendered line width = %d", terminalWidth, got)
		}
	}
}

func TestExpandedComposerKeepsDashboardWithinTerminal(t *testing.T) {
	m := newTestModel(manyRunningSessions(30))
	m.composer = newComposer()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(model)
	m.composer.SetValue(strings.Repeat("wrapped prompt words ", 20))
	m.syncComposerHeight()

	out := m.View()
	if got := lipgloss.Height(out); got != m.height {
		t.Errorf("dashboard height with expanded composer = %d, want %d", got, m.height)
	}
	if got := maxLineWidth(out); got > m.width {
		t.Errorf("dashboard width with expanded composer = %d, want <= %d", got, m.width)
	}
}

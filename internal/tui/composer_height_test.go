package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestComposerHeightGrowsAndClamps exercises syncComposerHeight directly: the
// box is one line by default, grows a line per wrapped/hard-broken row, and is
// capped so it can never crowd out the session list.
func TestComposerHeightGrowsAndClamps(t *testing.T) {
	m := newTestModel(nil) // height 40 → ceiling is min(10, 40/3) = 10
	m.composer.Prompt = "" // mirror the real composer (no prompt gutter)
	m.composer.SetWidth(118)

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
	m := newTestModel(nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(model)
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

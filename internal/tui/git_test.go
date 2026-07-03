package tui

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestPadRight(t *testing.T) {
	// Plain (no ANSI) so widths are the raw lengths.
	got := padRight("left", "R", 10)
	if got != "left     R" { // 4 + 5 spaces + 1
		t.Errorf("padRight = %q", got)
	}
	// No room → suffix dropped.
	if got := padRight("aaaaaaaa", "RIGHT", 6); got != "aaaaaaaa" {
		t.Errorf("padRight should drop suffix when no room, got %q", got)
	}
	// Empty right → unchanged.
	if got := padRight("x", "", 10); got != "x" {
		t.Errorf("padRight = %q", got)
	}
}

func TestPadRightMeasuresStyledWidth(t *testing.T) {
	left := lipgloss.NewStyle().Bold(true).Render("hi") // width 2 despite ANSI
	right := branchStyle.Render("main")                 // width 4
	out := padRight(left, right, 12)
	if lipgloss.Width(out) != 12 {
		t.Errorf("styled padRight width = %d, want 12", lipgloss.Width(out))
	}
}

func TestGitBranchOfRealRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@e"},
		{"config", "user.name", "t"},
		{"checkout", "-q", "-b", "feature/x"},
		{"commit", "-q", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	if got := gitBranchOf(dir); got != "feature/x" {
		t.Errorf("gitBranchOf = %q, want feature/x", got)
	}
	// A non-repo yields "".
	if got := gitBranchOf(t.TempDir()); got != "" {
		t.Errorf("gitBranchOf(non-repo) = %q, want empty", got)
	}
}

func TestGitSuffix(t *testing.T) {
	m := newTestModel(nil)
	m.gitCache = map[string]gitInfo{
		"/r/a": {branch: "main", pr: "42"},
		"/r/b": {branch: "dev"},
		"/r/c": {},
	}
	if s := m.gitSuffix("/r/a"); !strings.Contains(s, "main") || !strings.Contains(s, "#42") {
		t.Errorf("suffix a = %q", s)
	}
	if s := m.gitSuffix("/r/b"); !strings.Contains(s, "dev") || strings.Contains(s, "#") {
		t.Errorf("suffix b = %q", s)
	}
	if s := m.gitSuffix("/r/c"); s != "" {
		t.Errorf("suffix c should be empty, got %q", s)
	}
}

func TestGitInfoMsgPreservesPRBetweenPolls(t *testing.T) {
	m := newTestModel(nil)
	// First: PR poll sets branch + pr.
	next, _ := m.Update(gitInfoMsg{infos: map[string]gitInfo{"/r": {branch: "main", pr: "7"}}, polledPR: true})
	m = next.(model)
	// Then: branch-only poll must not wipe the cached PR.
	next, _ = m.Update(gitInfoMsg{infos: map[string]gitInfo{"/r": {branch: "main"}}, polledPR: false})
	m = next.(model)
	if m.gitCache["/r"].pr != "7" {
		t.Errorf("PR not preserved between polls: %+v", m.gitCache["/r"])
	}
}

// TestGitInfoMsgDropsPROnBranchChange verifies a checkout invalidates the
// cached PR: a branch-only poll reporting a different branch must not keep the
// previous branch's PR number (which would render "newbranch · #oldPR").
func TestGitInfoMsgDropsPROnBranchChange(t *testing.T) {
	m := newTestModel(nil)
	// PR poll: feature/x has open PR #7.
	next, _ := m.Update(gitInfoMsg{infos: map[string]gitInfo{"/r": {branch: "feature/x", pr: "7"}}, polledPR: true})
	m = next.(model)
	// Branch-only poll after switching to main — the stale PR must be dropped.
	next, _ = m.Update(gitInfoMsg{infos: map[string]gitInfo{"/r": {branch: "main"}}, polledPR: false})
	m = next.(model)
	if got := m.gitCache["/r"]; got.branch != "main" || got.pr != "" {
		t.Errorf("stale PR carried across branch change: %+v", got)
	}
}

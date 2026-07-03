package tui

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"xanax/internal/config"
	"xanax/internal/session"
	"xanax/internal/store"
)

func newTestModel(sessions []*session.Session) model {
	cfg := config.Default()
	grp := grouped(sessions)
	m := model{
		deps:        Deps{Cfg: cfg, SocketDir: "/tmp/xanax-nonexistent-test"},
		composer:    textarea.New(),
		renameInput: textinput.New(),
		filterInput: textinput.New(),
		formInputs:  newFormInputs(),
		onComposer:  true,
		harnesses:   harnessNames(cfg),
		width:       120,
		height:      40,
		allSessions: grp,
		sessions:    grp,
	}
	m.composer.Focus()
	return m
}

func key(s string) tea.KeyMsg {
	switch s {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "ctrl+o":
		return tea.KeyMsg{Type: tea.KeyCtrlO}
	case "ctrl+k":
		return tea.KeyMsg{Type: tea.KeyCtrlK}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func send(m model, k string) model {
	next, _ := m.Update(key(k))
	return next.(model)
}

// selectSession moves selection off the composer onto session index i.
func selectSession(m model, i int) model {
	m.onComposer = false
	m.cursor = i
	m.composer.Blur()
	return m
}

func sampleSessions() []*session.Session {
	now := time.Now()
	return []*session.Session{
		{ID: "run00001", Title: "refactor auth", Harness: "opencode", RepoPath: "/x/a", Status: session.StatusRunning, CreatedAt: now},
		{ID: "wait0001", Title: "which API?", Harness: "pi", RepoPath: "/x/b", Status: session.StatusWaiting, StatusDetail: "pick one", CreatedAt: now},
		{ID: "done0001", Title: "release notes", Harness: "opencode", RepoPath: "/x/c", Status: session.StatusCompleted, CreatedAt: now},
	}
}

func TestGroupedOrdersWaitingFirst(t *testing.T) {
	g := grouped(sampleSessions())
	if g[0].Status != session.StatusWaiting {
		t.Errorf("first group should be waiting, got %q", g[0].Status)
	}
	if g[len(g)-1].Status != session.StatusCompleted {
		t.Errorf("last should be completed, got %q", g[len(g)-1].Status)
	}
}

func TestArrowNavigationBetweenSessionsAndComposer(t *testing.T) {
	m := newTestModel(sampleSessions()) // starts on the composer, 3 sessions
	if !m.onComposer {
		t.Fatal("should start on the composer")
	}
	m = send(m, "up") // composer -> last session
	if m.onComposer || m.cursor != 2 {
		t.Fatalf("up from composer: onComposer=%v cursor=%d, want a session at 2", m.onComposer, m.cursor)
	}
	m = send(m, "up")
	m = send(m, "up") // to index 0
	m = send(m, "up") // clamp at 0
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want clamped to 0", m.cursor)
	}
	m = send(m, "down")
	m = send(m, "down") // to last session
	m = send(m, "down") // last session -> composer
	if !m.onComposer {
		t.Fatal("down from last session should select the composer")
	}
}

func TestComposerAcceptsTextOnlyWhenSelected(t *testing.T) {
	// Selected: typing lands in the composer.
	m := newTestModel(sampleSessions())
	m = send(m, "h")
	m = send(m, "i")
	if m.composer.Value() != "hi" {
		t.Errorf("composer did not capture text while selected: %q", m.composer.Value())
	}

	// Not selected (a session is): the same keys must not type into the box.
	m2 := selectSession(newTestModel(sampleSessions()), 0)
	m2 = send(m2, "z")
	if m2.composer.Value() != "" {
		t.Errorf("composer captured text while a session was selected: %q", m2.composer.Value())
	}
}

func TestComposerEnterLaunchesAndClears(t *testing.T) {
	m := newTestModel(sampleSessions())
	m.composer.SetValue("fix the flaky test")
	next, cmd := m.Update(key("enter"))
	m = next.(model)
	if cmd == nil {
		t.Fatal("enter with a prompt returned no launch command")
	}
	if m.composer.Value() != "" {
		t.Errorf("composer not cleared after launch: %q", m.composer.Value())
	}
}

func TestSessionLetterAndCtrlKeysAct(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sess := &session.Session{ID: "todelete01", Title: "dead", RepoPath: "/x", Harness: "opencode", Status: session.StatusFailed}
	if err := st.CreateSession(sess); err != nil {
		t.Fatal(err)
	}

	m := selectSession(newTestModel([]*session.Session{sess}), 0)
	m.deps.Store = st
	m.deps.SocketDir = t.TempDir()

	// Plain 'k' removes (no Ctrl needed).
	_, cmd := m.Update(key("k"))
	if cmd == nil {
		t.Fatal("k on a selected session returned no command")
	}
	cmd()
	if _, err := st.GetSession("todelete01"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("k did not remove the session: %v", err)
	}
}

func TestRightOpensSelectedSession(t *testing.T) {
	m := selectSession(newTestModel(sampleSessions()), 0)
	_, cmd := m.Update(key("right"))
	if cmd == nil {
		t.Fatal("right arrow on a selected session returned no command")
	}
}

func TestRenameUpdatesTitleInStore(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sess := &session.Session{ID: "rename001", Title: "old name", RepoPath: "/x", Harness: "pi", Status: session.StatusRunning}
	if err := st.CreateSession(sess); err != nil {
		t.Fatal(err)
	}

	m := selectSession(newTestModel([]*session.Session{sess}), 0)
	m.deps.Store = st

	next, _ := m.Update(key("e")) // start rename
	m = next.(model)
	if !m.renaming {
		t.Fatal("e did not enter rename mode")
	}
	if m.renameInput.Value() != "old name" {
		t.Errorf("rename input not pre-filled: %q", m.renameInput.Value())
	}
	m.renameInput.SetValue("brand new name")
	next, cmd := m.Update(key("enter"))
	m = next.(model)
	if m.renaming {
		t.Error("enter did not exit rename mode")
	}
	if cmd == nil {
		t.Fatal("rename returned no command")
	}
	cmd() // commit

	got, _ := st.GetSession("rename001")
	if got.Title != "brand new name" {
		t.Errorf("title = %q, want renamed", got.Title)
	}
}

func TestRenameEscCancels(t *testing.T) {
	m := selectSession(newTestModel(sampleSessions()), 0)
	next, _ := m.Update(key("e"))
	m = next.(model)
	next, _ = m.Update(key("esc"))
	m = next.(model)
	if m.renaming {
		t.Error("esc did not cancel rename")
	}
}

func TestHarnessNamesDefaultFirst(t *testing.T) {
	cfg := config.Default() // default_harness = opencode, plus pi
	names := harnessNames(cfg)
	if len(names) != 2 || names[0] != "opencode" || names[1] != "pi" {
		t.Errorf("harnessNames = %v, want [opencode pi]", names)
	}
}

func TestTabOpensPickerAndSelectsHarness(t *testing.T) {
	m := newTestModel(nil) // composer selected
	m = send(m, "tab")
	if !m.picking {
		t.Fatal("tab did not open the harness picker")
	}
	m = send(m, "down") // opencode -> pi
	m = send(m, "enter")
	if m.picking {
		t.Fatal("enter did not close the picker")
	}
	if m.harness() != "pi" {
		t.Errorf("harness = %q, want pi after picking", m.harness())
	}
	// The choice drives the launch args.
	args := newSessionArgs(m.harness(), "", "do things", false)
	want := []string{"new", "--harness", "pi", "--no-attach", "--", "do things"}
	if !slices.Equal(args, want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
}

// isQuit reports whether cmd produces tea.Quit.
func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestCtrlCConfirmsThenQuitsFromEveryMode(t *testing.T) {
	// In every mode the first Ctrl+C arms the confirmation (no quit); the
	// second Ctrl+C quits.
	cases := []struct {
		name  string
		build func() model
	}{
		{"composer", func() model { return newTestModel(sampleSessions()) }},
		{"session selected", func() model { return selectSession(newTestModel(sampleSessions()), 0) }},
		{"harness picker", func() model {
			m := send(newTestModel(nil), "tab")
			if !m.picking {
				t.Fatal("picker did not open")
			}
			return m
		}},
		{"renaming", func() model {
			m := send(selectSession(newTestModel(sampleSessions()), 0), "e")
			if !m.renaming {
				t.Fatal("rename did not open")
			}
			return m
		}},
		{"filtering", func() model {
			m := send(selectSession(newTestModel(sampleSessions()), 0), "/")
			if !m.filtering {
				t.Fatal("filter did not open")
			}
			return m
		}},
		{"add-harness form", func() model {
			m := send(send(newTestModel(nil), "tab"), "+")
			if !m.addingHarness {
				t.Fatal("add-harness form did not open")
			}
			return m
		}},
	}
	for _, tc := range cases {
		next, cmd := tc.build().Update(key("ctrl+c"))
		if isQuit(cmd) {
			t.Errorf("%s: first ctrl+c quit; want a confirmation prompt", tc.name)
		}
		armed := next.(model)
		if !armed.confirmQuit {
			t.Errorf("%s: first ctrl+c did not arm confirmQuit", tc.name)
		}
		if _, cmd := armed.Update(key("ctrl+c")); !isQuit(cmd) {
			t.Errorf("%s: second ctrl+c did not quit", tc.name)
		}
	}
}

// TestCtrlCConfirmDisarmsOnOtherKey checks that any non-Ctrl+C keystroke cancels
// a pending quit confirmation, and that the footer shows the prompt while armed.
func TestCtrlCConfirmDisarmsOnOtherKey(t *testing.T) {
	next, _ := newTestModel(sampleSessions()).Update(key("ctrl+c"))
	armed := next.(model)
	if !armed.confirmQuit {
		t.Fatal("ctrl+c did not arm confirmQuit")
	}
	if !strings.Contains(armed.footer(), "again to exit") {
		t.Error("footer did not show the confirm-exit prompt while armed")
	}
	// Typing a character both disarms the confirmation and still reaches the
	// composer — the disarming keystroke must not be swallowed.
	after, cmd := armed.Update(key("x"))
	if isQuit(cmd) {
		t.Error("a non-ctrl+c key quit while the confirmation was armed")
	}
	m := after.(model)
	if m.confirmQuit {
		t.Error("a non-ctrl+c key did not disarm confirmQuit")
	}
	if m.composer.Value() != "x" {
		t.Errorf("disarming keystroke was swallowed: composer = %q, want %q", m.composer.Value(), "x")
	}
}

func TestPickerEscCancelsWithoutChanging(t *testing.T) {
	m := newTestModel(nil)
	m = send(m, "tab")
	m = send(m, "down")
	m = send(m, "esc")
	if m.picking {
		t.Fatal("esc did not close the picker")
	}
	if m.harness() != "opencode" {
		t.Errorf("esc changed the harness to %q", m.harness())
	}
}

func TestPickerArrowsDoNotMoveSessionSelection(t *testing.T) {
	m := newTestModel(sampleSessions())
	m = send(m, "tab") // open picker while composer selected
	m = send(m, "up")  // must move picker highlight, not leave composer
	if !m.picking {
		t.Fatal("picker closed unexpectedly")
	}
	if !m.onComposer {
		t.Error("picker arrows leaked into session navigation")
	}
}

func TestCtrlOLaunchesAndAttaches(t *testing.T) {
	m := newTestModel(nil)
	m.composer.SetValue("hello agent")
	next, cmd := m.Update(key("ctrl+o"))
	m = next.(model)
	if cmd == nil {
		t.Fatal("ctrl+o returned no command")
	}
	if m.composer.Value() != "" {
		t.Errorf("composer not cleared: %q", m.composer.Value())
	}
}

func TestNewSessionArgsAttach(t *testing.T) {
	args := newSessionArgs("opencode", "", "-starts with dash", true)
	want := []string{"new", "--harness", "opencode", "--", "-starts with dash"}
	if !slices.Equal(args, want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
}

func TestTabWithSingleHarnessOpensPicker(t *testing.T) {
	m := newTestModel(nil)
	m.harnesses = []string{"opencode"} // even with one harness, Tab must reach
	m = send(m, "tab")                 // the picker so '+' (add harness) is usable
	if !m.picking {
		t.Fatal("tab did not open the picker with a single harness")
	}
}

func TestLaunchFailureRestoresPrompt(t *testing.T) {
	m := newTestModel(nil)
	// A launch that failed before the session captured the prompt hands it back
	// so the user can retry instead of losing what they typed.
	next, _ := m.Update(actionDoneMsg{status: "launch failed: boom", restorePrompt: "my prompt"})
	m = next.(model)
	if m.composer.Value() != "my prompt" {
		t.Errorf("prompt not restored: %q", m.composer.Value())
	}
	if m.status != "launch failed: boom" {
		t.Errorf("status = %q, want the failure message", m.status)
	}
}

func TestLaunchFailureDoesNotClobberNewText(t *testing.T) {
	m := newTestModel(nil)
	// The user started typing something new while a background launch was in
	// flight; a late failure must not overwrite it with the old prompt.
	m.composer.SetValue("something new")
	next, _ := m.Update(actionDoneMsg{status: "launch failed: boom", restorePrompt: "old prompt"})
	m = next.(model)
	if m.composer.Value() != "something new" {
		t.Errorf("composer clobbered: %q, want %q", m.composer.Value(), "something new")
	}
}

func TestScopeFilter(t *testing.T) {
	sessions := []*session.Session{
		{ID: "1", RepoPath: "/home/u/proj"},
		{ID: "2", RepoPath: "/home/u/proj/sub"},
		{ID: "3", RepoPath: "/home/u/proj2"}, // sibling with shared prefix
		{ID: "4", RepoPath: "/home/u/other"},
	}
	got := scopeFilter(sessions, "/home/u/proj")
	ids := map[string]bool{}
	for _, s := range got {
		ids[s.ID] = true
	}
	if !ids["1"] || !ids["2"] {
		t.Errorf("scope should include the repo and nested repos: %v", ids)
	}
	if ids["3"] {
		t.Error("scope must not match a sibling sharing the path prefix (/home/u/proj2)")
	}
	if ids["4"] {
		t.Error("scope must not match an unrelated repo")
	}
	// Empty scope keeps everything.
	if len(scopeFilter(sessions, "")) != 4 {
		t.Error("empty scope should keep all sessions")
	}
}

func TestScopedLaunchTargetsRepo(t *testing.T) {
	m := newTestModel(nil)
	m.deps.Scope = "/work/repo"
	args := newSessionArgs(m.harness(), m.deps.Scope, "do it", false)
	if !argsContain(args, "--repo", "/work/repo") {
		t.Errorf("scoped launch missing --repo /work/repo: %v", args)
	}
}

func argsContain(args []string, flag, val string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == val {
			return true
		}
	}
	return false
}

func TestFilterSessions(t *testing.T) {
	all := sampleSessions() // "refactor auth"(opencode), "which API?"(pi), "release notes"(opencode)
	if got := filterSessions(all, "pi"); len(got) != 1 || got[0].Harness != "pi" {
		t.Errorf("harness filter = %v", got)
	}
	if got := filterSessions(all, "RELEASE"); len(got) != 1 || got[0].Title != "release notes" {
		t.Errorf("case-insensitive title filter = %v", got)
	}
	if got := filterSessions(all, "nomatch"); len(got) != 0 {
		t.Errorf("no-match filter = %v", got)
	}
	if len(filterSessions(all, "")) != 3 {
		t.Error("empty filter should keep all")
	}
}

func TestFilterModeLiveNarrowsList(t *testing.T) {
	m := selectSession(newTestModel(sampleSessions()), 0)
	m = send(m, "/") // open filter
	if !m.filtering {
		t.Fatal("/ did not open filter")
	}
	// Type "release" — only that session remains.
	for _, r := range "release" {
		m = send(m, string(r))
	}
	if len(m.sessions) != 1 || m.sessions[0].Title != "release notes" {
		t.Errorf("live filter did not narrow: %d sessions", len(m.sessions))
	}
	// Esc clears back to all.
	m = send(m, "esc")
	if m.filtering || m.filter != "" || len(m.sessions) != 3 {
		t.Errorf("esc did not clear filter: filtering=%v filter=%q n=%d", m.filtering, m.filter, len(m.sessions))
	}
}

func TestApplyThemeChangesColors(t *testing.T) {
	t.Cleanup(func() { applyTheme(config.Default().Theme) }) // restore for other tests
	applyTheme(config.Theme{
		Accent: "#123456", Muted: "240", Text: "15", Waiting: "3", Running: "4",
		Completed: "2", Failed: "1", Cancelled: "8", Branch: "5", PR: "6",
	})
	if colAccent != lipgloss.Color("#123456") {
		t.Errorf("accent not applied: %v", colAccent)
	}
	// Derived styles pick up the new accent.
	if titleStyle.GetForeground() != lipgloss.Color("#123456") {
		t.Errorf("titleStyle foreground not rebuilt: %v", titleStyle.GetForeground())
	}
}

// TestReloadKeepsSelectionByID guards against the cursor (a list index) sliding
// onto a different session when the periodic reload re-sorts the list — which
// would make k/enter act on the wrong session.
func TestReloadKeepsSelectionByID(t *testing.T) {
	m := selectSession(newTestModel(sampleSessions()), 2) // the completed row, last
	selID := m.current().ID

	// A new waiting session appears; grouped() sorts it to the top, pushing the
	// completed row's index from 2 to 3.
	extra := &session.Session{
		ID: "wait0002", Title: "new", Harness: "pi", RepoPath: "/x/d",
		Status: session.StatusWaiting, CreatedAt: time.Now(),
	}
	next, _ := m.Update(sessionsMsg{sessions: grouped(append(sampleSessions(), extra))})
	m = next.(model)

	if got := m.current(); got == nil || got.ID != selID {
		t.Fatalf("selection not preserved by ID across reload: got %v, want %s", got, selID)
	}
}

// TestReloadWhileFilteringZeroKeepsSelection guards the case where a background
// reload arrives while the filter bar is open and currently matches nothing:
// it must not yank focus to the composer (which would silently drop the
// selection once matches return).
func TestReloadWhileFilteringZeroKeepsSelection(t *testing.T) {
	m := selectSession(newTestModel(sampleSessions()), 0)
	m = send(m, "/")
	for _, r := range "zzz" { // matches nothing
		m = send(m, string(r))
	}
	if len(m.sessions) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(m.sessions))
	}
	next, _ := m.Update(sessionsMsg{sessions: grouped(sampleSessions())})
	m = next.(model)
	if m.onComposer {
		t.Error("reload during an empty filter stole focus to the composer")
	}
	if !m.filtering {
		t.Error("filter bar should stay open across the reload")
	}
}

// TestReloadEmptyWithFilterFocusesComposer guards the flip side of the case
// above: when the underlying list becomes genuinely empty while a filter string
// is still set (e.g. the last matching session was removed with k), the
// composer MUST be focused — otherwise current() is nil and every keystroke is
// silently dropped with no way back but the arrow keys.
func TestReloadEmptyWithFilterFocusesComposer(t *testing.T) {
	m := selectSession(newTestModel(sampleSessions()), 0)
	m.filter = "refactor" // applied filter matching one session
	m.sessions = filterSessions(m.allSessions, m.filter)
	// That session is removed → the reload delivers zero sessions.
	next, _ := m.Update(sessionsMsg{sessions: nil})
	m = next.(model)
	if !m.onComposer {
		t.Fatal("genuinely-empty list with a filter set must focus the composer")
	}
}

// TestEscClearsFilterWhenAllHidden verifies Esc still clears an applied filter
// when it currently hides every row (current() == nil), so the user is never
// trapped in an empty filtered view.
func TestEscClearsFilterWhenAllHidden(t *testing.T) {
	m := selectSession(newTestModel(sampleSessions()), 0)
	m.filter = "zzz" // applied filter that matches nothing
	m.sessions = filterSessions(m.allSessions, m.filter)
	if len(m.sessions) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(m.sessions))
	}
	m = send(m, "esc")
	if m.filter != "" || len(m.sessions) != 3 {
		t.Errorf("esc did not clear filter on empty view: filter=%q n=%d", m.filter, len(m.sessions))
	}
}

func TestViewRendersWithoutPanic(t *testing.T) {
	m := newTestModel(sampleSessions())
	out := m.View()
	if !strings.Contains(out, "xanax") {
		t.Error("view missing header")
	}
	if !strings.Contains(out, "Needs input") {
		t.Error("view missing group header")
	}
	// Rename view renders too.
	rm := selectSession(newTestModel(sampleSessions()), 0)
	rn, _ := rm.Update(key("e"))
	if !strings.Contains(rn.(model).View(), "Rename session") {
		t.Error("rename view missing")
	}
}

func TestEmptyStateRenders(t *testing.T) {
	m := newTestModel(nil)
	if out := m.View(); !strings.Contains(out, "No sessions") {
		t.Errorf("empty state not shown: %q", out)
	}
}

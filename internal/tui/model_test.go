package tui

import (
	"errors"
	"fmt"
	"os"
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
		deps:          Deps{Cfg: cfg, SocketDir: "/tmp/xanax-nonexistent-test"},
		composer:      textarea.New(),
		renameInput:   textinput.New(),
		filterInput:   textinput.New(),
		formInputs:    newFormInputs(),
		onComposer:    true,
		harnesses:     harnessNames(cfg),
		searchInput:   textinput.New(),
		settingsInput: textinput.New(),
		width:         120,
		height:        40,
		allSessions:   grp,
		sessions:      grp,
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
	case "ctrl+x":
		return tea.KeyMsg{Type: tea.KeyCtrlX}
	case "ctrl+k":
		return tea.KeyMsg{Type: tea.KeyCtrlK}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "space": // bubbletea encodes the spacebar as KeySpace carrying the rune
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
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

func manyRunningSessions(n int) []*session.Session {
	now := time.Now()
	sessions := make([]*session.Session, 0, n)
	for i := range n {
		sessions = append(sessions, &session.Session{
			ID:        fmt.Sprintf("sess%04d", i),
			Title:     fmt.Sprintf("session %02d", i),
			Harness:   "opencode",
			RepoPath:  "/x/many",
			Status:    session.StatusRunning,
			CreatedAt: now.Add(-time.Duration(i) * time.Minute),
		})
	}
	return sessions
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
	m = send(m, "up") // to the top chat (index 0)
	if m.onComposer || m.cursor != 0 {
		t.Fatalf("up to top: onComposer=%v cursor=%d, want the top chat at 0", m.onComposer, m.cursor)
	}
	m = send(m, "up") // top chat wraps up to the composer (full circle)
	if !m.onComposer {
		t.Fatal("up from the top chat should wrap around to the composer")
	}
	m = send(m, "down") // composer wraps down to the top chat (full circle)
	if m.onComposer || m.cursor != 0 {
		t.Fatalf("down from the composer: onComposer=%v cursor=%d, want a wrap to the top chat at 0", m.onComposer, m.cursor)
	}
	m = send(m, "down")
	m = send(m, "down") // to the last session (index 2)
	if m.onComposer || m.cursor != 2 {
		t.Fatalf("down to last: onComposer=%v cursor=%d, want the last chat at 2", m.onComposer, m.cursor)
	}
	m = send(m, "down") // last session -> composer
	if !m.onComposer {
		t.Fatal("down from last session should select the composer")
	}
}

func TestVimNavigationKeysMoveSelection(t *testing.T) {
	m := selectSession(newTestModel(sampleSessions()), 1)
	m = send(m, "k")
	if m.onComposer || m.cursor != 0 {
		t.Fatalf("k should move selection up: onComposer=%v cursor=%d", m.onComposer, m.cursor)
	}
	m = send(m, "j")
	if m.onComposer || m.cursor != 1 {
		t.Fatalf("j should move selection down: onComposer=%v cursor=%d", m.onComposer, m.cursor)
	}
}

func TestComposerAcceptsVimNavigationLetters(t *testing.T) {
	m := newTestModel(sampleSessions())
	m = send(m, "j")
	m = send(m, "k")
	if !m.onComposer {
		t.Fatal("j/k typed in the composer should not move selection to a session")
	}
	if m.composer.Value() != "jk" {
		t.Errorf("composer = %q, want jk", m.composer.Value())
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

func TestCtrlXRemovesTerminalSession(t *testing.T) {
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

	_, cmd := m.Update(key("ctrl+x"))
	if cmd == nil {
		t.Fatal("ctrl+x on a terminal selected session returned no command")
	}
	cmd()
	if _, err := st.GetSession("todelete01"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("ctrl+x did not remove the session: %v", err)
	}
}

func TestLiveRemoveRequiresConfirmation(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sess := &session.Session{ID: "live0001", Title: "running", RepoPath: "/x", Harness: "opencode", Status: session.StatusRunning}
	if err := st.CreateSession(sess); err != nil {
		t.Fatal(err)
	}

	m := selectSession(newTestModel([]*session.Session{sess}), 0)
	m.deps.Store = st
	m.deps.SocketDir = t.TempDir()

	next, cmd := m.Update(key("ctrl+x"))
	m = next.(model)
	if cmd != nil {
		t.Fatal("first remove press on a live session should only arm confirmation")
	}
	if m.confirmRemoveID != "live0001" {
		t.Fatalf("confirmRemoveID = %q, want live0001", m.confirmRemoveID)
	}
	if _, err := st.GetSession("live0001"); err != nil {
		t.Fatalf("first remove press deleted the live session: %v", err)
	}
	if foot := m.footer(); !strings.Contains(foot, "again to kill and remove live0001") {
		t.Errorf("footer did not show remove confirmation: %q", foot)
	}

	next, cmd = m.Update(key("ctrl+x"))
	m = next.(model)
	if cmd == nil {
		t.Fatal("second remove press on the same live session returned no command")
	}
	cmd()
	if _, err := st.GetSession("live0001"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("confirmed remove did not delete the session: %v", err)
	}
	if m.confirmRemoveID != "" {
		t.Errorf("confirmation remained armed after dispatch: %q", m.confirmRemoveID)
	}
}

func TestRemoveConfirmationDisarmsOnOtherKey(t *testing.T) {
	sess := &session.Session{ID: "live0002", Title: "running", RepoPath: "/x", Harness: "opencode", Status: session.StatusRunning}
	m := selectSession(newTestModel([]*session.Session{sess}), 0)

	next, cmd := m.Update(key("ctrl+x"))
	m = next.(model)
	if cmd != nil || m.confirmRemoveID != "live0002" {
		t.Fatalf("first remove did not arm confirmation: cmd=%v id=%q", cmd, m.confirmRemoveID)
	}

	next, _ = m.Update(key("j"))
	m = next.(model)
	if m.confirmRemoveID != "" {
		t.Errorf("non-remove key did not disarm confirmation: %q", m.confirmRemoveID)
	}
}

func TestKillFailureDoesNotDeleteSession(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sess := &session.Session{ID: "killfail1", Title: "running", RepoPath: "/x", Harness: "opencode", Status: session.StatusRunning}
	if err := st.CreateSession(sess); err != nil {
		t.Fatal(err)
	}

	m := selectSession(newTestModel([]*session.Session{sess}), 0)
	m.deps.Store = st
	m.deps.SocketDir = t.TempDir()
	m.attachAliveFn = func(string) bool { return true }
	m.attachKillFn = func(string) error { return errors.New("socket closed") }

	next, _ := m.Update(key("ctrl+x"))
	m = next.(model)
	next, cmd := m.Update(key("ctrl+x"))
	m = next.(model)
	if cmd == nil {
		t.Fatal("confirmed live remove returned no command")
	}
	raw := cmd()
	msg, ok := raw.(actionDoneMsg)
	if !ok {
		t.Fatalf("remove command returned %T, want actionDoneMsg", raw)
	}
	if !strings.Contains(msg.status, "remove failed: kill killfail") {
		t.Fatalf("status = %q, want kill failure", msg.status)
	}
	if _, err := st.GetSession("killfail1"); err != nil {
		t.Fatalf("kill failure deleted the session: %v", err)
	}
	if m.confirmRemoveID != "" {
		t.Errorf("confirmation remained armed after confirmed remove: %q", m.confirmRemoveID)
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
	cfg := config.Default() // default_harness = opencode, rest alphabetical
	names := harnessNames(cfg)
	if !slices.Equal(names, []string{"opencode", "codex", "pi"}) {
		t.Errorf("harnessNames = %v, want [opencode codex pi]", names)
	}
}

func TestTabOpensPickerAndSelectsHarness(t *testing.T) {
	m := newTestModel(nil) // composer selected
	m = send(m, "tab")
	if !m.picking {
		t.Fatal("tab did not open the harness picker")
	}
	m = send(m, "down") // opencode -> codex
	m = send(m, "enter")
	if m.picking {
		t.Fatal("enter did not close the picker")
	}
	if m.harness() != "codex" {
		t.Errorf("harness = %q, want codex after picking", m.harness())
	}
	// The choice drives the launch args.
	args := newSessionArgs(m.harness(), "", "do things", false)
	want := []string{"new", "--harness", "codex", "--no-attach", "--", "do things"}
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

// TestPickerSearchFiltering confirms that typing letters filters the harness
// list, and the current pickIdx is clamped to the filtered results.
func TestPickerSearchFiltering(t *testing.T) {
	m := newTestModel(nil) // built-ins: opencode, codex, pi (opencode is default)
	m = send(m, "tab")
	if !m.picking {
		t.Fatal("picker not open")
	}
	// Typing "open" should focus the search bar and filter to opencode.
	for _, r := range "open" {
		m = send(m, string(r))
	}
	if !m.searchFocused {
		t.Fatal("search should be focused after typing")
	}
	filtered := m.filteredHarnesses()
	if len(filtered) != 1 || filtered[0] != "opencode" {
		t.Errorf("filtered = %v, want [opencode]", filtered)
	}
	// Enter should select it.
	m = send(m, "enter")
	if m.picking {
		t.Fatal("enter did not close picker")
	}
	if m.harness() != "opencode" {
		t.Errorf("selected harness = %q, want opencode", m.harness())
	}
}

// TestPickerSearchEsc verifies Esc from the search box closes the picker (a
// single Esc closes everything from the picker).
func TestPickerSearchEsc(t *testing.T) {
	m := newTestModel(nil)
	m = send(m, "tab")
	m = send(m, "p") // type into the search box
	if !m.searchFocused {
		t.Fatal("search not focused")
	}
	// Esc from the search box closes the picker.
	m = send(m, "esc")
	if m.picking {
		t.Fatal("esc did not close picker")
	}
}

// TestPickerOpensSearchable confirms the picker opens with the search box
// focused so a letter filters rather than triggering a command — in particular
// 'd' must not silently change the default harness.
func TestPickerOpensSearchable(t *testing.T) {
	m := newTestModel(nil)
	m = send(m, "tab")
	if !m.searchFocused {
		t.Fatal("picker should open with the search box focused")
	}
	m = send(m, "d") // 'd' filters here; it only sets the default on the action row
	if !m.picking {
		t.Fatal("'d' closed the picker instead of filtering")
	}
	if m.search != "d" {
		t.Errorf("search = %q, want d", m.search)
	}
}

// TestPickerArrowsNavigateWhileSearching confirms ↑/↓ move the highlight even
// while the search box is focused, so a non-top match can be selected.
func TestPickerArrowsNavigateWhileSearching(t *testing.T) {
	m := newTestModel(nil) // opencode (default), codex, pi
	m = send(m, "tab")
	if !m.searchFocused {
		t.Fatal("picker should open with the search box focused")
	}
	m = send(m, "down") // navigate to codex while still in the search box
	if m.pickIdx != 1 {
		t.Fatalf("pickIdx = %d, want 1 (arrows must navigate while searching)", m.pickIdx)
	}
	m = send(m, "enter")
	if m.harness() != "codex" {
		t.Errorf("selected harness = %q, want codex", m.harness())
	}
}

func TestPickerSearchAcceptsVimNavigationLetters(t *testing.T) {
	m := newTestModel(nil)
	m = send(m, "tab")
	m = send(m, "j")
	m = send(m, "k")
	if !m.searchFocused {
		t.Fatal("picker search should stay focused")
	}
	if m.search != "jk" {
		t.Errorf("search = %q, want jk", m.search)
	}
}

// TestPickerDefaultKey ensures pressing 'd' on the action row while a harness is
// highlighted sets it as the default in the config.
func TestPickerDefaultKey(t *testing.T) {
	m := newTestModel(nil)
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.toml")
	data := `default_harness = "opencode"` + "\n"
	os.WriteFile(cfgPath, []byte(data), 0o600)
	m.deps.ConfigPath = cfgPath

	m = send(m, "tab")  // open picker (search focused), pickIdx = 0 (opencode)
	m = send(m, "tab")  // switch to the action row so 'd' is a command
	m = send(m, "down") // move to codex (pickIdx = 1)
	if m.pickIdx != 1 {
		t.Errorf("pickIdx = %d, want 1", m.pickIdx)
	}
	// Press 'd' to set codex as default.
	m = send(m, "d")
	if m.picking {
		t.Fatal("'d' did not close picker")
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if cfg.DefaultHarness != "codex" {
		t.Errorf("default_harness = %q, want codex", cfg.DefaultHarness)
	}
}

// TestPickerScroll verifies that arrow keys scroll the list when there are
// more harnesses than visible rows.
func TestPickerScroll(t *testing.T) {
	cfg := config.Default()
	cfg.DefaultHarness = "a"
	cfg.Harnesses = map[string]config.Harness{
		"a": {Adapter: "generic", Command: "a"},
		"b": {Adapter: "generic", Command: "b"},
		"c": {Adapter: "generic", Command: "c"},
		"d": {Adapter: "generic", Command: "d"},
		"e": {Adapter: "generic", Command: "e"},
		"f": {Adapter: "generic", Command: "f"},
		"g": {Adapter: "generic", Command: "g"},
	}
	names := harnessNames(cfg)
	m := model{
		deps:          Deps{Cfg: cfg},
		harnesses:     names,
		picking:       true,
		pickIdx:       0,
		searchFocused: false,
		search:        "",
		pickScroll:    0,
		width:         80,
		height:        20,
	}
	m.searchInput = textinput.New()
	m.searchInput.Prompt = ""
	m.searchInput.Placeholder = "search..."
	m.searchInput.CharLimit = 80
	m.searchInput.TextStyle = lipgloss.NewStyle().Foreground(colWhite)

	vis := m.visibleRows()
	if vis > 6 {
		t.Errorf("visibleRows = %d, expected <= 6 for height 20", vis)
	}
	for i := 0; i < vis+2; i++ {
		m = send(m, "down")
	}
	if m.pickScroll == 0 {
		t.Error("pickScroll should have increased")
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
// is still set (e.g. the last matching session was removed), the
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

func TestEmptyFilteredListArrowsKeepEscAvailable(t *testing.T) {
	for _, key := range []string{"up", "down"} {
		t.Run(key, func(t *testing.T) {
			m := selectSession(newTestModel(sampleSessions()), 0)
			m.filter = "zzz" // applied filter that matches nothing
			m.sessions = filterSessions(m.allSessions, m.filter)
			if len(m.sessions) != 0 {
				t.Fatalf("expected 0 matches, got %d", len(m.sessions))
			}

			m = send(m, key)
			if m.onComposer {
				t.Fatalf("%s moved focus to the composer on an empty filtered list", key)
			}

			m = send(m, "esc")
			if m.filter != "" || len(m.sessions) != 3 {
				t.Errorf("esc after %s did not clear filter: filter=%q n=%d", key, m.filter, len(m.sessions))
			}
		})
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

func TestRenderListWindowsAroundSelectedSession(t *testing.T) {
	m := selectSession(newTestModel(manyRunningSessions(24)), 12)
	out := stripANSI(m.renderList(9))
	for _, want := range []string{"session 12", "more sessions above", "more sessions below"} {
		if !strings.Contains(out, want) {
			t.Fatalf("windowed list missing %q:\n%s", want, out)
		}
	}
	for _, hidden := range []string{"session 00", "session 23"} {
		if strings.Contains(out, hidden) {
			t.Fatalf("windowed list rendered hidden row %q:\n%s", hidden, out)
		}
	}
}

func TestViewWindowsLongSessionListToTerminalHeight(t *testing.T) {
	m := selectSession(newTestModel(manyRunningSessions(30)), 20)
	m.width, m.height = 80, 24

	out := m.View()
	plain := stripANSI(out)
	if h := lipgloss.Height(out); h != m.height {
		t.Fatalf("view height = %d, want %d", h, m.height)
	}
	for _, want := range []string{"xanax", "session 20", "more sessions above", "more sessions below"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("long-list view missing %q:\n%s", want, plain)
		}
	}
	if last := lastLine(out); !strings.Contains(last, "select") {
		t.Fatalf("footer is not pinned to the last line: %q", last)
	}
}

func TestViewWindowsLongSessionListWithPreview(t *testing.T) {
	m := selectSession(newTestModel(manyRunningSessions(30)), 20)
	m.width, m.height = 80, 24
	m.composer.SetHeight(1)
	m.previewOn = true
	m.previewText = strings.Join([]string{
		"preview line 1",
		"preview line 2",
		"preview line 3",
		"preview line 4",
		"preview line 5",
	}, "\n")

	out := m.View()
	plain := stripANSI(out)
	if h := lipgloss.Height(out); h != m.height {
		t.Fatalf("view height with preview = %d, want %d", h, m.height)
	}
	for _, want := range []string{"session 20", "Preview", "preview line 5", "more sessions above"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("preview long-list view missing %q:\n%s", want, plain)
		}
	}
}

func TestPreviewOpensOnlyWithSpace(t *testing.T) {
	m := selectSession(newTestModel(sampleSessions()), 0)
	if m.renderPreview() != "" {
		t.Fatal("preview rendered before space was pressed")
	}
	if m.previewCmd() != nil {
		t.Fatal("preview fetch scheduled while the preview is closed")
	}

	next, cmd := m.Update(key("space"))
	m = next.(model)
	if !m.previewOn || cmd == nil {
		t.Fatalf("space should open the preview and fetch: on=%v cmd-nil=%v", m.previewOn, cmd == nil)
	}
	if m.previewCmd() == nil {
		t.Fatal("open preview must keep scheduling fetches (tick refresh)")
	}
	next, _ = m.Update(previewMsg{id: m.selectedID(), text: "agent screen"})
	m = next.(model)
	if !strings.Contains(m.renderPreview(), "agent screen") {
		t.Errorf("preview not rendered after space + fetch: %q", m.renderPreview())
	}

	m = send(m, "space")
	if m.previewOn || m.renderPreview() != "" {
		t.Error("space again should close the preview")
	}
}

func TestPreviewClosesWhenSelectionMoves(t *testing.T) {
	m := selectSession(newTestModel(sampleSessions()), 0)
	m = send(m, "space")
	next, _ := m.Update(previewMsg{id: m.selectedID(), text: "peek"})
	m = next.(model)
	m = send(m, "down")
	if m.previewOn || m.renderPreview() != "" {
		t.Error("moving the selection should close the preview")
	}
}

// TestPreviewClosesWhenPeekedSessionVanishes guards the non-arrow selection
// change: a reload drops the peeked session and reselect() clamps onto a
// neighbor — the preview must close rather than silently show a session the
// user never peeked.
func TestPreviewClosesWhenPeekedSessionVanishes(t *testing.T) {
	m := selectSession(newTestModel(sampleSessions()), 0)
	m = send(m, "space")
	next, _ := m.Update(previewMsg{id: m.selectedID(), text: "peek"})
	m = next.(model)

	all := sampleSessions()
	remaining := grouped([]*session.Session{all[0], all[2]}) // peeked wait0001 removed
	next, _ = m.Update(sessionsMsg{sessions: remaining})
	m = next.(model)
	if m.previewOn || m.renderPreview() != "" {
		t.Error("preview should close when the peeked session disappears on reload")
	}
}

func TestPreviewStaleResultIgnoredWhenClosed(t *testing.T) {
	m := selectSession(newTestModel(sampleSessions()), 0)
	m = send(m, "space")
	id := m.selectedID()
	m = send(m, "space") // closed again before the in-flight fetch lands
	next, _ := m.Update(previewMsg{id: id, text: "late"})
	m = next.(model)
	if m.previewText != "" || m.renderPreview() != "" {
		t.Error("preview result applied while closed")
	}
}

// TestPreviewTickKeepsFetchingWhileOpen executes the tick's command batch and
// asserts it contains the preview fetch — guarding the tickMsg → previewCmd
// wiring that keeps an open preview fresh, not just previewCmd in isolation
// (dropping previewCmd from the tick batch would freeze the preview at its
// first snapshot forever and no other test would notice).
func TestPreviewTickKeepsFetchingWhileOpen(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m := selectSession(newTestModel(sampleSessions()), 0)
	m.deps.Store = st // the tick batch also runs reload(), which needs a store
	m = send(m, "space")

	next, cmd := m.Update(tickMsg{})
	m = next.(model)
	if cmd == nil {
		t.Fatal("tick returned no command")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("tick did not return a batch, got %T", cmd())
	}
	found := false
	for _, c := range batch { // Peek fails fast on the nonexistent socket -> empty text
		if pm, ok := c().(previewMsg); ok && pm.id == m.selectedID() {
			found = true
		}
	}
	if !found {
		t.Error("tick batch did not fetch the open preview")
	}
}

// TestPreviewClosesOnMoveUpAndFilterNarrow covers the remaining selection-change
// paths: an up-arrow move, and live filter typing sliding a different session
// under the cursor.
func TestPreviewClosesOnMoveUpAndFilterNarrow(t *testing.T) {
	m := selectSession(newTestModel(sampleSessions()), 1)
	m = send(m, "space")
	next, _ := m.Update(previewMsg{id: m.selectedID(), text: "peek"})
	m = next.(model)
	m = send(m, "up")
	if m.previewOn || m.renderPreview() != "" {
		t.Error("up-arrow move should close the preview")
	}

	m2 := selectSession(newTestModel(sampleSessions()), 0) // wait0001 peeked
	m2 = send(m2, "space")
	next2, _ := m2.Update(previewMsg{id: m2.selectedID(), text: "peek"})
	m2 = next2.(model)
	m2 = send(m2, "/")
	for _, r := range "release" { // narrows to done0001; wait0001 drops out
		m2 = send(m2, string(r))
	}
	if m2.previewOn || m2.renderPreview() != "" {
		t.Error("filter narrowing away the peeked session should close the preview")
	}
}

// TestPreviewIgnoresResultForOtherSession guards the id half of the previewMsg
// acceptance condition: a late result for a session that is no longer selected
// must not be stored while the preview is open on another session.
func TestPreviewIgnoresResultForOtherSession(t *testing.T) {
	m := selectSession(newTestModel(sampleSessions()), 0)
	m = send(m, "space")
	next, _ := m.Update(previewMsg{id: "someother1", text: "wrong session"})
	m = next.(model)
	if m.previewText != "" {
		t.Errorf("stored a preview for a non-selected session: %q", m.previewText)
	}
}

func TestSpaceTypesIntoComposer(t *testing.T) {
	m := newTestModel(sampleSessions()) // starts on the composer
	m = send(m, "a")
	m = send(m, "space")
	m = send(m, "b")
	if m.composer.Value() != "a b" {
		t.Errorf("composer = %q, want %q", m.composer.Value(), "a b")
	}
}

// TestRebindingSessionAction proves a config remap changes which key acts: after
// rebinding remove from Ctrl+X to 'x', 'x' removes the session and Ctrl+X is inert.
func TestRebindingSessionAction(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sess := &session.Session{ID: "rebind0001", Title: "dead", RepoPath: "/x", Harness: "opencode", Status: session.StatusFailed}
	if err := st.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	m := selectSession(newTestModel([]*session.Session{sess}), 0)
	m.deps.Store = st
	m.deps.SocketDir = t.TempDir()
	m.deps.Cfg.Keys.Remove = config.Binding{"x"} // rebind ctrl+x -> x

	// The old key no longer acts.
	if _, cmd := m.Update(key("ctrl+x")); cmd != nil {
		t.Fatal("ctrl+x still acted after remove was rebound to 'x'")
	}
	if _, err := st.GetSession("rebind0001"); err != nil {
		t.Fatalf("old remove key deleted the session: %v", err)
	}
	// The new key does.
	_, cmd := m.Update(key("x"))
	if cmd == nil {
		t.Fatal("'x' did not act after rebinding remove to it")
	}
	cmd()
	if _, err := st.GetSession("rebind0001"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("rebound remove did not delete the session: %v", err)
	}
}

// TestFooterReflectsReboundKeys confirms the hint line is built from the live
// bindings, so a remapped key shows in the footer instead of a stale default.
func TestFooterReflectsReboundKeys(t *testing.T) {
	m := selectSession(newTestModel(sampleSessions()), 0)
	m.deps.Cfg.Keys.Remove = config.Binding{"x"}
	if foot := m.footer(); !strings.Contains(foot, "x remove") {
		t.Errorf("footer did not reflect the rebound remove key: %q", foot)
	}
}

// TestSettingsKeyOpensEditor confirms 's' (from a selected session) opens the
// keybindings editor with its search box focused.
func TestSettingsKeyOpensEditor(t *testing.T) {
	m := selectSession(newTestModel(sampleSessions()), 0)
	m = send(m, "s")
	if !m.settingsOn {
		t.Fatal("'s' did not open the settings editor")
	}
	if !strings.Contains(m.View(), "Keybindings") {
		t.Error("settings modal did not render its Keybindings panel")
	}
}

func TestSettingsSearchAcceptsVimNavigationLetters(t *testing.T) {
	m := selectSession(newTestModel(sampleSessions()), 0)
	m = send(m, "s")
	m = send(m, "j")
	m = send(m, "k")
	if !m.settingsOn {
		t.Fatal("settings editor closed unexpectedly")
	}
	if m.settingsSearch != "jk" {
		t.Errorf("settingsSearch = %q, want jk", m.settingsSearch)
	}
}

// TestSettingsRebindPersistsAndApplies drives the full editor flow: filter to an
// action, capture two keys, commit with Enter, and confirm the multi-key binding
// is both written to the config file and live in the model (so keyMatches uses it
// immediately).
func TestSettingsRebindPersistsAndApplies(t *testing.T) {
	m := selectSession(newTestModel(sampleSessions()), 0)
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	m.deps.ConfigPath = cfgPath

	m = send(m, "s") // open the editor
	if !m.settingsOn {
		t.Fatal("settings editor did not open")
	}
	for _, r := range "remove" { // search narrows to the remove action
		m = send(m, string(r))
	}
	if got := m.filteredActions(); len(got) == 0 || got[m.settingsIdx].Name != "remove" {
		t.Fatalf("search did not highlight the remove action: %v", got)
	}
	m = send(m, "enter") // begin capturing keys
	if !m.settingsCapture {
		t.Fatal("enter did not start key capture")
	}
	if !strings.Contains(m.View(), "Press keys for remove") {
		t.Error("capture mode did not prompt for the new keys")
	}
	m = send(m, "x")      // accumulate two keys before committing
	m = send(m, "ctrl+k") //
	m = send(m, "x")      // a repeat is ignored (deduped)
	if !slices.Equal(m.settingsPending, []string{"x", "ctrl+k"}) {
		t.Fatalf("pending keys = %v, want [x ctrl+k] (deduped)", m.settingsPending)
	}
	m = send(m, "enter") // commit
	if m.settingsCapture {
		t.Fatal("enter did not complete the capture")
	}
	if !slices.Equal(m.deps.Cfg.Keys.Remove, config.Binding{"x", "ctrl+k"}) {
		t.Errorf("in-memory remove = %v, want [x ctrl+k] right after rebinding", m.deps.Cfg.Keys.Remove)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("reload persisted config: %v", err)
	}
	if !slices.Equal(cfg.Keys.Remove, config.Binding{"x", "ctrl+k"}) {
		t.Errorf("persisted remove = %v, want [x ctrl+k]", cfg.Keys.Remove)
	}
}

// TestSettingsEmptyCommitIsNoOp confirms committing with no keys captured is a
// cancel, not an accidental unbind.
func TestSettingsEmptyCommitIsNoOp(t *testing.T) {
	m := selectSession(newTestModel(sampleSessions()), 0)
	m.deps.ConfigPath = filepath.Join(t.TempDir(), "config.toml")
	before := slices.Clone(m.deps.Cfg.Keys.Remove)

	m = send(m, "s")
	for _, r := range "remove" {
		m = send(m, string(r))
	}
	m = send(m, "enter") // start capture
	m = send(m, "enter") // commit with nothing captured
	if m.settingsCapture {
		t.Error("empty commit left capture active")
	}
	if !slices.Equal(m.deps.Cfg.Keys.Remove, before) {
		t.Errorf("empty commit changed the binding: %v", m.deps.Cfg.Keys.Remove)
	}
}

// TestSettingsCaptureEscAborts confirms Esc during capture leaves the binding
// untouched, and Esc in list mode closes the editor.
func TestSettingsCaptureEscAborts(t *testing.T) {
	m := selectSession(newTestModel(sampleSessions()), 0)
	m.deps.ConfigPath = filepath.Join(t.TempDir(), "config.toml")
	before := slices.Clone(m.deps.Cfg.Keys.Remove)

	m = send(m, "s")
	for _, r := range "remove" {
		m = send(m, string(r))
	}
	m = send(m, "enter") // capture
	m = send(m, "esc")   // abort capture, stay in the editor
	if m.settingsCapture {
		t.Error("esc did not abort the capture")
	}
	if !m.settingsOn {
		t.Error("esc during capture should return to the list, not close the editor")
	}
	if !slices.Equal(m.deps.Cfg.Keys.Remove, before) {
		t.Errorf("aborted capture changed the binding: %v", m.deps.Cfg.Keys.Remove)
	}
	m = send(m, "esc") // now close the editor
	if m.settingsOn {
		t.Error("esc in list mode did not close the editor")
	}
}

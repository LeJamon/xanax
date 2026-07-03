package tui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"xanax/internal/config"
	"xanax/internal/session"
	"xanax/internal/store"
)

func newTestModel(sessions []*session.Session) model {
	cfg := config.Default()
	m := model{
		deps:        Deps{Cfg: cfg, SocketDir: "/tmp/xanax-nonexistent-test"},
		composer:    textarea.New(),
		renameInput: textinput.New(),
		onComposer:  true,
		harnesses:   harnessNames(cfg),
		width:       120,
		height:      40,
		sessions:    grouped(sessions),
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
	args := newSessionArgs(m.harness(), "do things", false)
	want := []string{"new", "--harness", "pi", "--no-attach", "--", "do things"}
	if len(args) != len(want) {
		t.Fatalf("args = %v", args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args = %v, want %v", args, want)
		}
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
	args := newSessionArgs("opencode", "-starts with dash", true)
	want := []string{"new", "--harness", "opencode", "--", "-starts with dash"}
	if len(args) != len(want) {
		t.Fatalf("args = %v", args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args = %v, want %v", args, want)
		}
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

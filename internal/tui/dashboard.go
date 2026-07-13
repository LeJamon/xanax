// Package tui is the rvr dashboard: an arrow-key-navigated list of sessions
// plus an always-present prompt box, both treated as selectable rows. Opening a
// session shells out to `rvr attach` so the live terminal owns the process
// cleanly (SPEC.md §10).
package tui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/LeJamon/rvr/internal/attach"
	"github.com/LeJamon/rvr/internal/config"
	"github.com/LeJamon/rvr/internal/session"
	"github.com/LeJamon/rvr/internal/sessionctl"
	"github.com/LeJamon/rvr/internal/store"
	"github.com/LeJamon/rvr/internal/termview"
)

// Deps are the services the dashboard needs from the CLI layer.
type Deps struct {
	Store      *store.Store
	Cfg        *config.Config
	SelfPath   string // path to the rvr binary, for shelling out
	LogsDir    string
	SocketDir  string
	ConfigPath string // config.toml, for adding harnesses from the dashboard
	Version    string // rvr release, shown in the header
	// Reconcile repairs interrupted session lifecycles. The dashboard invokes it
	// periodically so a handoff skipped during its startup grace is retried.
	Reconcile func() error
	// Scope, when set, restricts the dashboard to sessions under this absolute
	// path and launches new sessions there. Empty = all sessions, cwd launches.
	Scope string
}

// model treats the session list and the prompt box as one navigable column.
// The arrow keys move the selection while the prompt box is empty; with a draft
// they move its text cursor instead. The prompt box (last row) only accepts text
// while selected. When a session is selected, action bindings act on it instead
// of typing into the composer.
type model struct {
	deps     Deps
	composer textarea.Model

	// harnesses lists the configured harness names; harnessIdx selects which
	// one new sessions launch with. Tab opens a picker (scales to many
	// harnesses, unlike cycling).
	harnesses     []string
	harnessIdx    int
	picking       bool            // harness picker is open
	pickIdx       int             // highlighted row in the picker
	searchInput   textinput.Model // search input in the harness picker
	searchFocused bool            // search input currently has keyboard focus
	search        string          // current search filter text
	pickScroll    int             // scroll offset within filtered list

	// settings/keybindings editor (opened with the settings key while a session
	// is selected). It mirrors the harness picker: a searchable, scrollable list
	// of actions, each showing its current keys. Confirm on a row captures the
	// next keypress as that action's new binding (settingsCapture) and persists it.
	settingsOn      bool
	settingsCapture bool
	settingsPending []string // keys accumulated during a capture, committed on Enter
	settingsIdx     int
	settingsScroll  int
	settingsSearch  string
	settingsInput   textinput.Model

	allSessions []*session.Session // full scoped list
	sessions    []*session.Session // filtered display list (grouped)
	cursor      int                // selected session index (when !onComposer)
	onComposer  bool               // the prompt box is the selected row

	renaming    bool
	renameInput textinput.Model
	renameID    string

	filtering   bool
	filter      string
	filterInput textinput.Model

	addingHarness bool   // the harness form (add or modify) is open
	editHarness   string // "" = adding a new harness; else the name being modified
	formInputs    []textinput.Model
	formField     int
	formErr       string

	gitCache     map[string]gitInfo // live branch/PR per repo path
	gitPollCount int                // gates the slower PR refresh

	previewOn         bool   // preview panel open (space toggles it on the selected session)
	previewText       string // last fetched preview of the selected session
	previewLabel      string
	previewMode       previewMode
	previewGeneration uint64
	previewPending    bool // one request is in flight; prevents overlapping raw-log replays
	previewStatic     bool // terminal stored-log result is immutable and can be reused
	reconcilePending  bool // serializes periodic lifecycle reconciliation

	attachAliveFn func(string) bool
	attachKillFn  func(string) error

	width, height int
	path          string // scope (or cwd), home-relative, for the header
	status        string
	err           error

	// confirmQuit is armed by the first Ctrl+C; the next Ctrl+C (before any
	// other key) quits. Any other key disarms it. See updateKey / footer.
	confirmQuit bool

	// confirmRemoveID is armed by the first remove key on a live session; pressing
	// remove again on the same session performs the destructive action.
	confirmRemoveID string
}

const previewRows = 8

type previewMode uint8

const (
	previewAuto previewMode = iota
	previewStored
)

// harness returns the currently selected harness for new sessions.
func (m model) harness() string {
	if len(m.harnesses) == 0 {
		return m.deps.Cfg.DefaultHarness
	}
	return m.harnesses[m.harnessIdx]
}

type sessionsMsg struct {
	sessions []*session.Session
	err      error
}
type tickMsg struct{}
type reconcileTickMsg struct{}
type reconcileDoneMsg struct{ err error }

// actionDoneMsg reports the result of a shelled-out or background action.
// restorePrompt, when set, puts a would-be-lost prompt back in the composer
// (a launch failed before the session captured it, so the user can retry).
type actionDoneMsg struct {
	status          string
	restorePrompt   string
	confirmRemoveID string
}

// Run starts the dashboard event loop.
func Run(deps Deps) error {
	applyTheme(deps.Cfg.Theme) // rebuild styles from the configured theme

	ta := newComposer()

	ri := textinput.New()
	ri.Prompt = ""
	ri.CharLimit = 120
	ri.TextStyle = lipgloss.NewStyle().Foreground(colWhite)

	fi := textinput.New()
	fi.Prompt = ""
	fi.CharLimit = 80
	fi.TextStyle = lipgloss.NewStyle().Foreground(colWhite)

	si := textinput.New()
	si.Prompt = ""
	si.Placeholder = "search..."
	si.CharLimit = 80
	si.TextStyle = lipgloss.NewStyle().Foreground(colWhite)

	gi := textinput.New()
	gi.Prompt = ""
	gi.Placeholder = "search settings..."
	gi.CharLimit = 40
	gi.TextStyle = lipgloss.NewStyle().Foreground(colWhite)

	m := model{
		deps:          deps,
		composer:      ta,
		renameInput:   ri,
		filterInput:   fi,
		formInputs:    newFormInputs(),
		onComposer:    true,
		harnesses:     harnessNames(deps.Cfg),
		path:          headerPath(deps.Scope),
		searchInput:   si,
		searchFocused: false,
		settingsInput: gi,
	}
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

// newComposer builds the prompt textarea: white input text with no line
// numbers, starting one line tall and auto-growing (syncComposerHeight), and a
// placeholder rendered in the theme's muted shade. Reads the package-level
// colors, so callers must have applied the theme (init seeds the defaults).
func newComposer() textarea.Model {
	ta := textarea.New()
	ta.Placeholder = "Paste a prompt and press Enter to launch a new session…"
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.SetHeight(1) // starts one line, auto-grows with content (syncComposerHeight)
	ta.CharLimit = 0
	// White text, and no current-line highlight. The cursor line is styled by
	// CursorLine (not Text), so it must carry the white foreground too —
	// otherwise a blank CursorLine drops the color on the line being typed.
	white := lipgloss.NewStyle().Foreground(colWhite)
	ta.FocusedStyle.Text = white
	ta.FocusedStyle.CursorLine = white
	ta.BlurredStyle.Text = white
	ta.BlurredStyle.CursorLine = white
	// Placeholder in the theme's muted shade. Bubbles defaults it to a dark
	// grey (240) that's hard to read; tracking colMuted keeps it the same light
	// grey as the other hint text so it stays legible.
	placeholder := lipgloss.NewStyle().Foreground(colMuted)
	ta.FocusedStyle.Placeholder = placeholder
	ta.BlurredStyle.Placeholder = placeholder
	ta.Focus()
	return ta
}

// headerPath resolves the location shown in the header: the dashboard's scope,
// or the working directory when unscoped, rendered home-relative (~/…).
func headerPath(scope string) string {
	p := scope
	if p == "" {
		if wd, err := os.Getwd(); err == nil {
			p = wd
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if p == home {
			return "~"
		}
		if rel, ok := strings.CutPrefix(p, home+string(filepath.Separator)); ok {
			return "~/" + rel
		}
	}
	return p
}

// harnessNames returns the configured harness names, default first, rest
// alphabetical — so the picker (opened with Tab) lists the default at the top.
func harnessNames(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Harnesses))
	for name := range cfg.Harnesses {
		if name != cfg.DefaultHarness {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return append([]string{cfg.DefaultHarness}, names...)
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.reload(), tickCmd(), gitTickCmd(), m.reconcileTickCmd(), textarea.Blink)
}

func (m model) reload() tea.Cmd {
	scope := m.deps.Scope
	return func() tea.Msg {
		s, err := m.deps.Store.ListSessions()
		return sessionsMsg{sessions: grouped(scopeFilter(s, scope)), err: err}
	}
}

// filterSessions keeps sessions whose title, repo, or harness contains the
// (case-insensitive) query. An empty query keeps everything.
func filterSessions(sessions []*session.Session, query string) []*session.Session {
	if query == "" {
		return sessions
	}
	q := strings.ToLower(query)
	out := sessions[:0:0]
	for _, s := range sessions {
		if strings.Contains(strings.ToLower(s.Title), q) ||
			strings.Contains(strings.ToLower(s.RepoPath), q) ||
			strings.Contains(strings.ToLower(s.Harness), q) {
			out = append(out, s)
		}
	}
	return out
}

// scopeFilter keeps sessions whose repo equals scope or lives under it. An
// empty scope keeps everything.
func scopeFilter(sessions []*session.Session, scope string) []*session.Session {
	if scope == "" {
		return sessions
	}
	prefix := scope + string(filepath.Separator)
	out := sessions[:0:0]
	for _, s := range sessions {
		if s.RepoPath == scope || strings.HasPrefix(s.RepoPath, prefix) {
			out = append(out, s)
		}
	}
	return out
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

type gitTickMsg struct{}

func gitTickCmd() tea.Cmd {
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return gitTickMsg{} })
}

func (m model) reconcileTickCmd() tea.Cmd {
	if m.deps.Reconcile == nil {
		return nil
	}
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return reconcileTickMsg{} })
}

func (m model) reconcileCmd() tea.Cmd {
	if m.deps.Reconcile == nil {
		return nil
	}
	return func() tea.Msg { return reconcileDoneMsg{err: m.deps.Reconcile()} }
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		widthChanged := m.width != msg.Width
		m.width, m.height = msg.Width, msg.Height
		m.composer.SetWidth(max(1, msg.Width-2))
		m.reflowComposer() // wrap width changed, so rebuild its height and viewport
		m.renameInput.Width = max(20, msg.Width-4)
		if m.addingHarness {
			m.syncFormWidths() // keep the form inputs sized to the (resized) modal
		}
		if widthChanged && m.previewOn {
			m = m.resetPreview(m.previewMode)
			return m.requestPreview()
		}
		return m, nil

	case sessionsMsg:
		firstLoad := m.gitCache == nil && len(msg.sessions) > 0
		prevID := m.selectedID()
		prevTerminal, hadPrev := false, false
		var prevUpdated time.Time
		if prev := m.current(); prev != nil {
			prevTerminal, hadPrev = prev.Status.Terminal(), true
			prevUpdated = prev.UpdatedAt
		}
		m.allSessions, m.err = msg.sessions, msg.err
		m.sessions = filterSessions(m.allSessions, m.filter)
		var selCmd tea.Cmd
		m, selCmd = m.reselect(prevID)
		m = m.closeMovedPreview(prevID)
		m = m.clearStaleRemoveConfirm()
		var preview tea.Cmd
		if m.previewOn && hadPrev && m.selectedID() == prevID {
			if current := m.current(); current != nil {
				lifecycleChanged := current.Status.Terminal() != prevTerminal ||
					(prevTerminal && current.Status.Terminal() && !current.UpdatedAt.Equal(prevUpdated))
				if lifecycleChanged {
					m = m.resetPreview(m.previewMode)
					m, preview = m.requestPreview()
				}
			}
		}
		if firstLoad {
			// Show branches promptly rather than waiting for the 5 s git tick
			// (branches only — the slower gh PR lookup stays on the tick).
			m.gitCache = make(map[string]gitInfo)
			return m, tea.Batch(selCmd, gitPollCmd(uniqueRepos(m), false), preview)
		}
		if preview != nil {
			return m, tea.Batch(selCmd, preview)
		}
		return m, selCmd

	case tickMsg:
		var preview tea.Cmd
		m, preview = m.requestPreview()
		return m, tea.Batch(m.reload(), tickCmd(), preview)

	case reconcileTickMsg:
		if m.reconcilePending {
			return m, m.reconcileTickCmd()
		}
		m.reconcilePending = true
		return m, tea.Batch(m.reconcileCmd(), m.reconcileTickCmd())

	case reconcileDoneMsg:
		m.reconcilePending = false
		if msg.err != nil {
			m.status = "reconcile failed: " + msg.err.Error()
		} else if strings.HasPrefix(m.status, "reconcile failed: ") {
			m.status = ""
		}
		return m, nil

	case previewMsg:
		// Only one preview command is ever in flight. A stale response releases
		// that slot and immediately schedules the current generation, rather than
		// overlapping a mode/width replacement with the old replay.
		m.previewPending = false
		if m.previewOn && msg.id == m.selectedID() &&
			msg.generation == m.previewGeneration && msg.mode == m.previewMode {
			m.previewText = msg.text
			m.previewStatic = msg.static
			if msg.label != "" {
				m.previewLabel = msg.label
			}
			return m, nil
		}
		return m.requestPreview()

	case gitTickMsg:
		// Refresh PR numbers only every 12th tick (~60 s); branches every tick.
		pollPR := m.gitPollCount%12 == 0
		m.gitPollCount++
		return m, tea.Batch(gitPollCmd(uniqueRepos(m), pollPR), gitTickCmd())

	case gitInfoMsg:
		if m.gitCache == nil {
			m.gitCache = make(map[string]gitInfo)
		}
		for repo, gi := range msg.infos {
			// Branches are polled every tick (~5 s), PR numbers only every ~60 s.
			// On a branch-only poll keep the cached PR — but only while the branch
			// is unchanged, since a checkout invalidates the old branch's PR (else
			// the row would pair the new branch with a stale, unrelated #number).
			if !msg.polledPR {
				if prev, ok := m.gitCache[repo]; ok && prev.branch == gi.branch {
					gi.pr = prev.pr
				}
			}
			m.gitCache[repo] = gi
		}
		return m, nil

	case actionDoneMsg:
		m.status = msg.status
		m.confirmRemoveID = msg.confirmRemoveID
		// Put a would-be-lost prompt back, but only if the box is empty — a
		// background launch stays interactive, so the user may have started
		// typing something new we must not clobber.
		if msg.restorePrompt != "" && m.composer.Value() == "" {
			m.composer.SetValue(msg.restorePrompt)
			m.reflowComposer()
		}
		return m, m.reload()

	case tea.KeyMsg:
		return m.updateKey(msg)
	}
	return m, nil
}

func (m model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// The quit key (Ctrl+C by default) is a two-step exit, regardless of mode
	// (Cancel/Esc is the per-mode dismiss key): the first press arms a
	// confirmation, the second quits.
	if keyMatches(m.keys().Quit, msg) {
		if m.confirmQuit {
			return m, tea.Quit
		}
		m.confirmQuit = true
		m.confirmRemoveID = ""
		return m, nil
	}
	// Any other key disarms a pending quit confirmation.
	m.confirmQuit = false
	if m.confirmRemoveID != "" && !keyMatches(m.keys().Remove, msg) {
		m.confirmRemoveID = ""
	}
	// The peek is per-session and closes whenever a key moves the selection to a
	// different session (reopened with space). updateKey is the single choke point
	// for all key handling, so capturing the selected id here and re-checking it
	// after dispatch closes the peek for every key-driven selection change in one
	// place — no individual key handler has to remember to route through it.
	prev := m.selectedID()
	next, cmd := m.dispatchKey(msg)
	if nm, ok := next.(model); ok {
		next = nm.closeMovedPreview(prev)
	}
	return next, cmd
}

// dispatchKey routes a key to the handler for the current mode.
func (m model) dispatchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.renaming {
		return m.updateRenameKey(msg)
	}
	if m.addingHarness {
		return m.updateFormKey(msg)
	}
	if m.picking {
		return m.updatePickKey(msg)
	}
	if m.settingsOn {
		return m.updateSettingsKey(msg)
	}
	if m.filtering {
		return m.updateFilterKey(msg)
	}
	if m.onComposer {
		switch {
		case keyMatches(m.keys().Up, msg) && !textInputKey(msg) && m.composer.Value() == "":
			return m.moveUp()
		case keyMatches(m.keys().Down, msg) && !textInputKey(msg) && m.composer.Value() == "":
			return m.moveDown()
		}
		return m.updateComposerKey(msg)
	}
	switch {
	case keyMatches(m.keys().Up, msg):
		return m.moveUp()
	case keyMatches(m.keys().Down, msg):
		return m.moveDown()
	}
	return m.updateSessionKey(msg)
}

func textInputKey(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace
}

// moveUp/moveDown treat the prompt box as the row just below the last session
// and wrap around, so the sessions plus the composer form a single circular
// ring: top chat → … → last chat → composer → top chat.
func (m model) moveUp() (tea.Model, tea.Cmd) {
	if m.onComposer {
		if len(m.sessions) > 0 {
			m.onComposer = false
			m.cursor = len(m.sessions) - 1
			m.composer.Blur()
		}
	} else if m.cursor > 0 {
		m.cursor--
	} else if len(m.sessions) > 0 {
		// At the top chat: wrap up to the composer to close the circle.
		m.onComposer = true
		return m, m.composer.Focus()
	}
	return m, nil
}

func (m model) moveDown() (tea.Model, tea.Cmd) {
	if m.onComposer {
		// At the composer: wrap down to the top chat to close the circle.
		if len(m.sessions) > 0 {
			m.onComposer = false
			m.cursor = 0
			m.composer.Blur()
		}
		return m, nil
	}
	var extra tea.Cmd
	if m.cursor < len(m.sessions)-1 {
		m.cursor++
	} else if len(m.sessions) > 0 {
		m.onComposer = true
		extra = m.composer.Focus()
	}
	return m, extra
}

// closeMovedPreview closes the preview when the selection left prevID — the peek
// is per-session, reopened with space. Callers capture the selected id before a
// change and pass it here afterwards: the updateKey choke point covers every
// key-driven selection change, and the async sessionsMsg reload (which can drop
// the peeked session) calls it directly. This keeps the preview from silently
// jumping to a session the user did not peek.
func (m model) closeMovedPreview(prevID string) model {
	if m.previewOn && m.selectedID() != prevID {
		m = m.closePreview()
	}
	return m
}

// updateComposerKey runs while the prompt box is selected. Enter launches a
// new session in the background; Ctrl+O (or Alt+Enter) launches and attaches
// immediately — landing in the harness's own input, where its native syntax
// (/commands, @files, completions) is fully available. Tab opens the harness
// picker. Cancel clears the prompt. Everything else edits the prompt, including
// Up/Down while it contains text so multiline cursor navigation stays local.
func (m model) updateComposerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := m.keys()
	switch {
	case keyMatches(k.Cancel, msg):
		m.composer.Reset()
		m.syncComposerHeight()
		return m, nil
	case keyMatches(k.HarnessPicker, msg):
		// Always open the picker — even with one (or zero) harnesses it is the
		// only way to reach the '+' add-harness form. It opens with the search box
		// focused (type to filter); Tab inside toggles to the single-key action row.
		m.picking = true
		m.pickIdx = m.harnessIdx
		m.search = ""
		m.searchInput.SetValue("")
		m.pickScroll = 0
		m.searchFocused = true
		m.ensurePickVisible() // reveal the current harness even when it sits below the fold
		m.composer.Blur()
		return m, m.searchInput.Focus()
	case keyMatches(k.LaunchAttach, msg):
		// Launch + attach; an empty prompt opens a fresh harness to type in.
		prompt := strings.TrimSpace(m.composer.Value())
		m.composer.Reset()
		m.syncComposerHeight() // shrink back to one line
		return m, m.execNewAttach(prompt)
	case keyMatches(k.Confirm, msg):
		if prompt := strings.TrimSpace(m.composer.Value()); prompt != "" {
			m.composer.Reset()
			m.syncComposerHeight() // shrink back to one line
			return m, m.execNewBackground(prompt)
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.composer, cmd = m.composer.Update(msg)
	m.syncComposerHeight() // grow/shrink to fit what the user just typed or pasted
	return m, cmd
}

// syncComposerHeight sizes the prompt box to its content and realigns the
// textarea's viewport whenever the visible row count changes.
func (m *model) syncComposerHeight() {
	m.resizeComposer(false)
}

// reflowComposer also rebuilds the textarea's viewport when its width or value
// changed outside textarea.Update (terminal resize and failed-launch restore).
func (m *model) reflowComposer() {
	m.resizeComposer(true)
}

func (m *model) resizeComposer(forceReflow bool) {
	const ceiling = 10
	maxRows := ceiling
	if m.height > 0 {
		maxRows = min(ceiling, max(1, m.height/3))
	}
	height := composerVisualRows(m.composer, maxRows)
	if forceReflow || height != m.composer.Height() {
		m.setComposerHeight(height)
		return
	}
	// At the cap, a paste can add multiple rows without changing the widget's
	// height. Refresh before repositioning so the cursor is visible immediately.
	if height == maxRows {
		m.repositionComposerViewport()
	}
}

// composerVisualRows walks a copy of the composer so LineInfo applies the exact
// wrapping rules without moving the live cursor. The copy reuses the textarea's
// wrap cache and backing value instead of allocating a new 10,000-line buffer
// on the keystroke hot path.
func composerVisualRows(composer textarea.Model, limit int) int {
	limit = max(1, limit)
	for composer.Line() < composer.LineCount()-1 {
		composer.CursorEnd()
		composer.CursorDown()
	}
	composer.CursorEnd()

	rows := 0
	for {
		rows += composer.LineInfo().Height
		if rows >= limit {
			return limit
		}
		if composer.Line() == 0 {
			return max(1, rows)
		}
		// CursorUp from column zero moves directly to the preceding logical line.
		composer.CursorStart()
		composer.CursorUp()
	}
}

// setComposerHeight preserves the edit position while resetting Bubbles'
// private viewport. SetHeight alone leaves its old scroll offset in place,
// which is why earlier wrapped rows used to disappear as the box grew.
func (m *model) setComposerHeight(height int) {
	value := m.composer.Value()
	line := m.composer.Line()
	info := m.composer.LineInfo()
	column := info.StartColumn + info.ColumnOffset

	m.composer.SetHeight(height)
	m.composer.SetValue(value) // Reset inside SetValue returns the viewport to top.
	for m.composer.Line() > line {
		m.composer.CursorStart()
		m.composer.CursorUp()
	}
	m.composer.SetCursor(column)
	m.repositionComposerViewport()
}

// repositionComposerViewport renders content into the textarea's viewport,
// then lets its update loop scroll the restored cursor into view. The temporary
// focus is necessary because a blurred textarea ignores updates.
func (m *model) repositionComposerViewport() {
	focused := m.composer.Focused()
	if !focused {
		m.composer.Focus()
	}
	_ = m.composer.View()
	m.composer, _ = m.composer.Update(nil)
	if !focused {
		m.composer.Blur()
	}
}

// updatePickKey runs while the harness picker is open. It opens with the search
// box focused: printable keys filter, ↑/↓ move the highlight, Enter selects,
// Esc cancels, '+' adds. Tab toggles to the action row, where 'd' sets the
// highlighted harness as default and 'm' modifies it — single keys that would
// otherwise be swallowed as search text.
func (m model) updatePickKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := m.keys()
	// '+' always opens the add-harness form, in either focus.
	if keyMatches(k.AddHarness, msg) {
		return m.startAddHarness()
	}
	// Navigation, selection and cancel behave the same whether or not the search
	// box holds focus, but printable navigation aliases stay available as search
	// text while the search box is active.
	switch {
	case keyMatches(k.Up, msg) && (!m.searchFocused || !textInputKey(msg)):
		m.movePick(-1)
		return m, nil
	case keyMatches(k.Down, msg) && (!m.searchFocused || !textInputKey(msg)):
		m.movePick(1)
		return m, nil
	case keyMatches(k.Confirm, msg):
		return m.pickHarness()
	case keyMatches(k.Cancel, msg):
		return m.cancelPick()
	case keyMatches(k.ToggleSearch, msg):
		m.searchFocused = !m.searchFocused
		if m.searchFocused {
			return m, m.searchInput.Focus()
		}
		m.searchInput.Blur()
		return m, nil
	}
	if m.searchFocused {
		// Typing filters the list; the highlight resets to the top match.
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		m.search = m.searchInput.Value()
		m.pickIdx = 0
		m.pickScroll = 0
		return m, cmd
	}
	// Action row: single-key harness commands.
	switch {
	case keyMatches(k.SetDefault, msg):
		return m.setDefaultHarness()
	case keyMatches(k.ModifyHarness, msg):
		return m.startModifyHarness()
	}
	return m, nil
}

// movePick moves the highlight by delta within the filtered list, scrolling the
// window so the selected row stays visible.
func (m *model) movePick(delta int) {
	n := len(m.filteredHarnesses())
	if n == 0 {
		return
	}
	m.pickIdx = min(max(m.pickIdx+delta, 0), n-1)
	m.ensurePickVisible()
}

// ensurePickVisible scrolls the picker window so pickIdx lands within the
// visible rows — used on open (the selection may sit below the fold) and after
// each move.
func (m *model) ensurePickVisible() {
	vis := m.visibleRows()
	if m.pickIdx < m.pickScroll {
		m.pickScroll = m.pickIdx
	} else if m.pickIdx >= m.pickScroll+vis {
		m.pickScroll = m.pickIdx - vis + 1
	}
	if m.pickScroll < 0 {
		m.pickScroll = 0
	}
}

// filteredHarnesses returns harness names matching the search text,
// case-insensitive substring match on the name.
func (m model) filteredHarnesses() []string {
	if m.search == "" {
		return m.harnesses
	}
	q := strings.ToLower(m.search)
	var out []string
	for _, name := range m.harnesses {
		if strings.Contains(strings.ToLower(name), q) {
			out = append(out, name)
		}
	}
	return out
}

// visibleRows returns the number of harness rows that fit in the list area,
// clamped between 5 and 12 based on terminal height.
func (m model) visibleRows() int {
	h := min(16, max(8, m.height/3))
	return max(5, h-4)
}

// pickHarness selects the highlighted harness and closes the picker.
func (m model) pickHarness() (tea.Model, tea.Cmd) {
	filtered := m.filteredHarnesses()
	if len(filtered) == 0 {
		m.picking = false
		m.searchFocused = false
		return m, m.composer.Focus()
	}
	name := filtered[m.pickIdx]
	m.harnessIdx = slices.Index(m.harnesses, name)
	m.picking = false
	m.searchFocused = false
	return m, m.composer.Focus()
}

// cancelPick closes the picker without selecting.
func (m model) cancelPick() (tea.Model, tea.Cmd) {
	m.picking = false
	m.searchFocused = false
	return m, m.composer.Focus()
}

// setDefaultHarness sets the highlighted harness as the default, writes to
// the config file, and closes the picker.
func (m model) setDefaultHarness() (tea.Model, tea.Cmd) {
	filtered := m.filteredHarnesses()
	if len(filtered) == 0 {
		return m.cancelPick()
	}
	name := filtered[m.pickIdx]
	m.picking = false
	m.searchFocused = false
	if m.deps.ConfigPath == "" {
		m.status = "set default failed: no config path"
		return m, m.composer.Focus()
	}
	unlock, err := acquireConfigLock(m.deps.ConfigPath)
	if err != nil {
		m.status = "set default failed: " + err.Error()
		return m, m.composer.Focus()
	}
	defer unlock()
	cfg, err := config.Load(m.deps.ConfigPath)
	if err != nil {
		m.status = "set default failed: " + err.Error()
		return m, m.composer.Focus()
	}
	if _, ok := cfg.Harnesses[name]; !ok {
		m.status = "set default failed: harness " + name + " no longer exists"
		return m, m.composer.Focus()
	}
	if err := setDefaultInConfig(m.deps.ConfigPath, name); err != nil {
		m.status = "set default failed: " + err.Error()
		return m, m.composer.Focus()
	}
	cfg, err = config.Load(m.deps.ConfigPath)
	if err != nil {
		m.status = "set default failed: " + err.Error()
		return m, m.composer.Focus()
	}
	m.deps.Cfg = cfg
	m.harnesses = harnessNames(cfg)
	m.harnessIdx = slices.Index(m.harnesses, name)
	m.status = "set " + name + " as default"
	return m, m.composer.Focus()
}

// updateSessionKey runs while a session is selected. Action bindings act on it
// instead of typing into the composer.
func (m model) updateSessionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := m.keys()
	// Cancel clears an applied filter — checked before the empty-list guard so it
	// still works when the filter currently hides every row (current() == nil).
	if keyMatches(k.Cancel, msg) && m.filter != "" {
		m.filter = ""
		m.sessions = filterSessions(m.allSessions, "")
		return m, nil
	}
	s := m.current()
	if s == nil {
		return m, nil
	}
	switch {
	case keyMatches(k.Open, msg):
		return m.openSession(s)
	case keyMatches(k.Remove, msg):
		return m.confirmOrRemove(s)
	case keyMatches(k.Resume, msg):
		return m, m.execInteractive("resume", s.ID)
	case keyMatches(k.Logs, msg):
		return m.showSessionLogs(s)
	case keyMatches(k.Preview, msg):
		return m.togglePreview()
	case keyMatches(k.Rename, msg):
		return m.startRename(s)
	case keyMatches(k.Filter, msg):
		m.filtering = true
		m.filterInput.SetValue(m.filter)
		m.filterInput.CursorEnd()
		return m, m.filterInput.Focus()
	case keyMatches(k.Settings, msg):
		return m.openSettings()
	case keyMatches(k.QuitList, msg):
		return m, tea.Quit
	}
	return m, nil
}

func (m model) confirmOrRemove(s *session.Session) (tea.Model, tea.Cmd) {
	force := m.confirmRemoveID == s.ID
	if m.removeNeedsConfirm(s) && !force {
		m.confirmRemoveID = s.ID
		m.status = "press " + keyHint(m.keys().Remove) + " again to kill and remove " + shortID(s.ID)
		return m, nil
	}
	m.confirmRemoveID = ""
	return m, m.execKillRemove(s, force)
}

func (m model) removeNeedsConfirm(s *session.Session) bool {
	return s.Status.Live() || m.alive(s.ID)
}

// updateFilterKey runs while the filter bar is open: typing refines the filter
// live, Enter applies and closes, Esc clears and closes, arrows navigate the
// filtered list.
func (m model) updateFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := m.keys()
	switch {
	case keyMatches(k.Confirm, msg):
		m.filtering = false
		m.filterInput.Blur()
		return m, nil
	case keyMatches(k.Cancel, msg):
		m.filtering = false
		m.filter = ""
		m.filterInput.Blur()
		m.filterInput.SetValue("")
		m.sessions = filterSessions(m.allSessions, "")
		return m, nil
	case keyMatches(k.Up, msg):
		return m.moveUp()
	case keyMatches(k.Down, msg):
		return m.moveDown()
	}
	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	m.filter = m.filterInput.Value()
	m.sessions = filterSessions(m.allSessions, m.filter)
	if m.cursor >= len(m.sessions) {
		m.cursor = max(0, len(m.sessions)-1)
	}
	return m, cmd
}

func (m model) startRename(s *session.Session) (tea.Model, tea.Cmd) {
	m.renaming = true
	m.renameID = s.ID
	m.renameInput.SetValue(s.Title)
	m.renameInput.CursorEnd()
	return m, m.renameInput.Focus()
}

// updateRenameKey runs while renaming a session (rvr UI label only).
func (m model) updateRenameKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := m.keys()
	switch {
	case keyMatches(k.Confirm, msg):
		title := strings.TrimSpace(m.renameInput.Value())
		id := m.renameID
		m.renaming = false
		m.renameInput.Blur()
		if title == "" {
			return m, nil
		}
		return m, m.execRename(id, title)
	case keyMatches(k.Cancel, msg):
		m.renaming = false
		m.renameInput.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.renameInput, cmd = m.renameInput.Update(msg)
	return m, cmd
}

// execInteractive shells out to a subcommand that owns the terminal (attach,
// resume). Bubble Tea releases and restores the terminal around it.
func (m model) execInteractive(sub string, arg string) tea.Cmd {
	c := exec.Command(m.deps.SelfPath, sub, arg)
	return execAttachProcess(c, func(err error, result attach.Result, reported bool) tea.Msg {
		if err != nil {
			return actionDoneMsg{status: fmt.Sprintf("%s failed: %v", sub, err)}
		}
		if reported {
			return actionDoneMsg{status: m.reportedInteractiveStatus(sub, arg, result)}
		}
		return actionDoneMsg{status: interactiveReturnStatus(sub)}
	})
}

func execAttachProcess(
	c *exec.Cmd,
	done func(error, attach.Result, bool) tea.Msg,
) tea.Cmd {
	reader, writer, err := os.Pipe()
	if err != nil {
		return tea.ExecProcess(c, func(runErr error) tea.Msg {
			return done(runErr, attach.Disconnected, false)
		})
	}
	fd := 3 + len(c.ExtraFiles)
	c.ExtraFiles = append(c.ExtraFiles, writer)
	c.Env = setProcessEnv(c.Env, attach.ResultFDEnv, strconv.Itoa(fd))
	return tea.ExecProcess(c, func(runErr error) tea.Msg {
		_ = writer.Close()
		if runErr != nil {
			_ = reader.Close()
			return done(runErr, attach.Disconnected, false)
		}
		_ = reader.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		data := make([]byte, 32)
		n, readErr := reader.Read(data)
		_ = reader.Close()
		result, reported := attach.ParseResult(data[:n])
		reported = reported && (readErr == nil || n > 0)
		return done(runErr, result, reported)
	})
}

func setProcessEnv(env []string, key, value string) []string {
	if env == nil {
		env = os.Environ()
	}
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			out = append(out, entry)
		}
	}
	return append(out, prefix+value)
}

func (m model) reportedInteractiveStatus(sub, id string, result attach.Result) string {
	var sess *session.Session
	if result == attach.SessionExited && id != "" && m.deps.Store != nil {
		sess, _ = m.deps.Store.GetSession(id)
	}
	return interactiveAttachResultStatus(sub, id, result, sess)
}

func interactiveAttachResultStatus(sub, id string, result attach.Result, sess *session.Session) string {
	switch result {
	case attach.Detached:
		if sub == "new" {
			if id == "" {
				return "detached from new session (still running)"
			}
			return "detached from new session " + shortID(id) + " (still running)"
		}
		return "detached from " + shortID(id) + " (still running)"
	case attach.SessionExited:
		if sess != nil && sess.Status.Terminal() {
			if sub == "new" {
				return "new session " + shortID(id) + " ended (" + string(sess.Status) + ")"
			}
			return "session " + shortID(id) + " ended (" + string(sess.Status) + ")"
		}
		if sub == "new" {
			if id == "" {
				return "new session ended"
			}
			return "new session " + shortID(id) + " ended"
		}
		return "session " + shortID(id) + " ended"
	case attach.Disconnected:
		if sub == "new" {
			if id == "" {
				return "disconnected from new session"
			}
			return "disconnected from new session " + shortID(id)
		}
		return "disconnected from " + shortID(id)
	default:
		return "returned from " + sub
	}
}

func interactiveReturnStatus(sub string) string {
	if sub == "new" {
		return "returned from new session"
	}
	return "returned from " + sub
}

// execKillRemove terminates a live session and deletes it from the store, so it
// disappears from the dashboard.
func (m model) execKillRemove(s *session.Session, force bool) tea.Cmd {
	id := s.ID
	return func() tea.Msg {
		results, err := sessionctl.Remove(m.deps.Store, []string{id}, sessionctl.Options{
			SocketDir: m.deps.SocketDir,
			Force:     force,
			Alive:     m.attachAlive,
			Kill:      m.attachKill,
		})
		if err != nil {
			var active *sessionctl.ActiveError
			if errors.As(err, &active) {
				return actionDoneMsg{
					status:          "press " + keyHint(m.keys().Remove) + " again to kill and remove " + shortID(id),
					confirmRemoveID: id,
				}
			}
			return actionDoneMsg{status: "remove failed: " + err.Error()}
		}
		if len(results) == 1 && results[0].Killed {
			return actionDoneMsg{status: "killed and removed " + shortID(id)}
		}
		return actionDoneMsg{status: "removed " + shortID(id)}
	}
}

// newSessionArgs builds the `rvr new` argv for the selected harness. repo,
// when set, targets a specific directory (the dashboard's scope); empty lets
// `rvr new` default to cwd. "--" stops flag parsing so prompts beginning
// with "-" are safe.
func newSessionArgs(harness, repo, prompt string, attach bool) []string {
	args := []string{"new", "--harness", harness}
	if repo != "" {
		args = append(args, "--repo", repo)
	}
	if !attach {
		args = append(args, "--no-attach")
	}
	return append(args, "--", prompt)
}

// execNewBackground launches a new session in the selected harness without
// attaching, so the user stays on the dashboard.
func (m model) execNewBackground(prompt string) tea.Cmd {
	h, repo := m.harness(), m.deps.Scope
	return func() tea.Msg {
		if err := exec.Command(m.deps.SelfPath, newSessionArgs(h, repo, prompt, false)...).Run(); err != nil {
			return actionDoneMsg{status: "launch failed: " + err.Error(), restorePrompt: prompt}
		}
		return actionDoneMsg{status: "launched " + h + ": " + truncate(prompt, 40)}
	}
}

// execNewAttach launches a new session and drops straight into the harness's
// live window (its own input, native /command and @file syntax included).
func (m model) execNewAttach(prompt string) tea.Cmd {
	known, tracking := m.snapshotSessionIDs()
	c := exec.Command(m.deps.SelfPath, newSessionArgs(m.harness(), m.deps.Scope, prompt, true)...)
	return execAttachProcess(c, func(err error, result attach.Result, reported bool) tea.Msg {
		if err != nil {
			return actionDoneMsg{status: "launch failed: " + err.Error(), restorePrompt: prompt}
		}
		id := ""
		if tracking {
			id = m.sessionCreatedSince(known)
		}
		if reported {
			return actionDoneMsg{status: m.reportedInteractiveStatus("new", id, result)}
		}
		return actionDoneMsg{status: interactiveReturnStatus("new")}
	})
}

func (m model) snapshotSessionIDs() (map[string]struct{}, bool) {
	if m.deps.Store == nil {
		return nil, false
	}
	sessions, err := m.deps.Store.ListSessions()
	if err != nil {
		return nil, false
	}
	ids := make(map[string]struct{}, len(sessions))
	for _, sess := range sessions {
		ids[sess.ID] = struct{}{}
	}
	return ids, true
}

// sessionCreatedSince returns the one session created by an interactive `new`
// invocation. Concurrent creators make the result ambiguous, in which case the
// dashboard uses a neutral return message instead of attributing the wrong row.
func (m model) sessionCreatedSince(known map[string]struct{}) string {
	sessions, err := m.deps.Store.ListSessions()
	if err != nil {
		return ""
	}
	created := ""
	for _, sess := range sessions {
		if _, ok := known[sess.ID]; ok {
			continue
		}
		if created != "" {
			return ""
		}
		created = sess.ID
	}
	return created
}

func (m model) execRename(id, title string) tea.Cmd {
	return func() tea.Msg {
		if err := m.deps.Store.SetTitle(id, title); err != nil {
			return actionDoneMsg{status: "rename failed: " + err.Error()}
		}
		return actionDoneMsg{status: "renamed to " + truncate(title, 40)}
	}
}

// openSession enters the live agent window. Terminal sessions are inspected
// read-only from their stored log; explicit resume stays on the resume binding.
func (m model) openSession(s *session.Session) (tea.Model, tea.Cmd) {
	if m.alive(s.ID) {
		return m, m.execInteractive("attach", s.ID)
	}
	if s.Status.Terminal() {
		return m.showSessionLogs(s)
	}
	if m.deps.Cfg.CanResume(s) {
		return m, m.execInteractive("resume", s.ID)
	}
	msg := "session " + shortID(s.ID) + " has ended — press " + keyHint(m.keys().Remove) + " to remove"
	return m, func() tea.Msg { return actionDoneMsg{status: msg} }
}

func (m model) current() *session.Session {
	if m.onComposer || m.cursor < 0 || m.cursor >= len(m.sessions) {
		return nil
	}
	return m.sessions[m.cursor]
}

// selectedID is the id of the selected session, or "" when the prompt box (or
// nothing) is selected.
func (m model) selectedID() string {
	if s := m.current(); s != nil {
		return s.ID
	}
	return ""
}

func (m model) clearStaleRemoveConfirm() model {
	if m.confirmRemoveID != "" && m.selectedID() != m.confirmRemoveID {
		m.confirmRemoveID = ""
	}
	return m
}

// reselect re-anchors the selection after the periodic reload rebuilt the list.
// The cursor is an index into a list that grouped() re-sorts by status then
// recency, so without re-anchoring a status change (or a newly launched
// session) can slide a different session under the cursor — making k/enter act
// on the wrong one. It keeps the previously selected session selected by ID
// when it survived, otherwise clamps into range. It only falls back to the
// prompt box when there are genuinely no sessions — not while a filter is
// narrowing the view, where an empty result is transient and Esc still clears
// the filter.
func (m model) reselect(prevID string) (model, tea.Cmd) {
	if len(m.sessions) > 0 {
		if i := indexOfID(m.sessions, prevID); i >= 0 {
			m.cursor = i
		} else if m.cursor > len(m.sessions)-1 {
			m.cursor = len(m.sessions) - 1
		}
		return m, nil
	}
	m.cursor = 0
	// Fall back to the prompt box only when there are genuinely no sessions at
	// all. While a filter is merely hiding every row (allSessions non-empty),
	// stay in the list context so an empty filtered view never traps keystrokes
	// and Esc still clears the filter.
	if !m.onComposer && len(m.allSessions) == 0 {
		m.onComposer = true
		return m, m.composer.Focus()
	}
	return m, nil
}

// indexOfID returns the index of the session with the given id, or -1.
func indexOfID(sessions []*session.Session, id string) int {
	if id == "" {
		return -1
	}
	for i, s := range sessions {
		if s.ID == id {
			return i
		}
	}
	return -1
}

// togglePreview opens or closes the peek of the selected session's screen.
// Opening fetches immediately; while open, the 1 s tick keeps it fresh.
func (m model) togglePreview() (tea.Model, tea.Cmd) {
	if m.previewOn {
		return m.closePreview(), nil
	}
	m.previewOn = true
	m = m.resetPreview(previewAuto)
	return m.requestPreview()
}

func (m model) showSessionLogs(s *session.Session) (tea.Model, tea.Cmd) {
	m.previewOn = true
	m = m.resetPreview(previewStored)
	m.status = "showing stored log for " + shortID(s.ID)
	return m.requestPreview()
}

func (m model) closePreview() model {
	m.previewOn = false
	m.previewGeneration++
	m.previewText = ""
	m.previewLabel = ""
	m.previewStatic = false
	return m
}

func (m model) resetPreview(mode previewMode) model {
	m.previewMode = mode
	m.previewGeneration++
	m.previewText = ""
	m.previewStatic = false
	if mode == previewStored {
		m.previewLabel = "Logs"
	} else {
		m.previewLabel = "Preview"
	}
	return m
}

// requestPreview schedules at most one fetch for the current preview state.
// Terminal stored logs are immutable once Finish is recorded, so their accepted
// result remains cached until the preview, selection, mode, or width changes.
func (m model) requestPreview() (model, tea.Cmd) {
	if !m.previewOn || m.previewPending || m.previewStatic {
		return m, nil
	}
	s := m.current()
	if s == nil {
		return m, nil
	}
	id := s.ID
	sock := filepath.Join(m.deps.SocketDir, s.ID+".sock")
	cols := max(1, m.width-4)
	mode := m.previewMode
	generation := m.previewGeneration
	m.previewPending = true
	return m, func() tea.Msg {
		if mode == previewStored || s.Status.Terminal() || !m.attachAlive(sock) {
			return previewMsg{
				id:         id,
				generation: generation,
				mode:       mode,
				label:      "Logs",
				text:       m.sessionLogPreview(s, cols),
				static:     s.Status.Terminal(),
			}
		}
		return previewMsg{
			id:         id,
			generation: generation,
			mode:       mode,
			label:      "Preview",
			text:       attach.Peek(sock, previewRows, cols),
		}
	}
}

// previewCmd is a read-only convenience for tests. State-changing paths use
// requestPreview so the pending flag serializes requests.
func (m model) previewCmd() tea.Cmd {
	_, cmd := m.requestPreview()
	return cmd
}

type previewMsg struct {
	id         string
	generation uint64
	mode       previewMode
	label      string
	text       string
	static     bool
}

func (m model) sessionLogPreview(s *session.Session, cols int) string {
	if m.deps.LogsDir == "" {
		return m.noSessionLogPreview(s, "log directory is unavailable")
	}
	data, err := os.ReadFile(filepath.Join(m.deps.LogsDir, s.ID+".raw"))
	if err != nil {
		if os.IsNotExist(err) {
			return m.noSessionLogPreview(s, "no stored log found")
		}
		return "read stored log failed: " + err.Error()
	}
	if len(data) == 0 {
		return m.noSessionLogPreview(s, "stored log is empty")
	}
	text := termview.PreviewBytes(data, 40, previewRows, cols)
	if strings.TrimSpace(text) == "" {
		return m.noSessionLogPreview(s, "stored log has no visible text")
	}
	return text
}

func (m model) noSessionLogPreview(s *session.Session, reason string) string {
	lines := []string{reason + " for " + shortID(s.ID)}
	if s.StatusDetail != "" {
		lines = append(lines, s.StatusDetail)
	}
	if m.deps.Cfg.CanResume(s) {
		lines = append(lines, "Press "+keyHint(m.keys().Resume)+" to resume.")
	}
	return strings.Join(lines, "\n")
}

func (m model) alive(id string) bool {
	return m.attachAlive(filepath.Join(m.deps.SocketDir, id+".sock"))
}

func (m model) attachAlive(sock string) bool {
	if m.attachAliveFn != nil {
		return m.attachAliveFn(sock)
	}
	return attach.Alive(sock)
}

func (m model) attachKill(sock string) error {
	if m.attachKillFn != nil {
		return m.attachKillFn(sock)
	}
	return attach.Kill(sock)
}

// grouped sorts sessions by state group then recency (SPEC.md §10).
func grouped(sessions []*session.Session) []*session.Session {
	out := append([]*session.Session(nil), sessions...)
	sort.SliceStable(out, func(i, j int) bool {
		gi, gj := groupRank(out[i].Status), groupRank(out[j].Status)
		if gi != gj {
			return gi < gj
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

func groupRank(s session.Status) int {
	switch s {
	case session.StatusWaiting:
		return 0
	case session.StatusIdle:
		return 1
	case session.StatusRunning, session.StatusStarting:
		return 2
	case session.StatusCompleted:
		return 3
	case session.StatusCancelled:
		return 4
	case session.StatusFailed:
		return 5
	default:
		return 6
	}
}

func groupLabel(rank int) string {
	switch rank {
	case 0:
		return "Needs input"
	case 1:
		return "Idle"
	case 2:
		return "Running"
	case 3:
		return "Completed"
	case 4:
		return "Cancelled"
	case 5:
		return "Failed"
	default:
		return "Other"
	}
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

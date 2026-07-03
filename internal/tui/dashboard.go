// Package tui is the xanax dashboard: an arrow-key-navigated list of sessions
// plus an always-present prompt box, both treated as selectable rows. Opening a
// session shells out to `xanax attach` so the live terminal owns the process
// cleanly (SPEC.md §10).
package tui

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"xanax/internal/attach"
	"xanax/internal/config"
	"xanax/internal/session"
	"xanax/internal/store"
)

// Deps are the services the dashboard needs from the CLI layer.
type Deps struct {
	Store     *store.Store
	Cfg       *config.Config
	SelfPath  string // path to the xanax binary, for shelling out
	SocketDir string
	// Scope, when set, restricts the dashboard to sessions under this absolute
	// path and launches new sessions there. Empty = all sessions, cwd launches.
	Scope string
}

// model treats the session list and the prompt box as one navigable column.
// The arrow keys always move the selection; the prompt box (last row) only
// accepts text while it is the selected row. When a session is selected, plain
// letter keys act on it — so no Ctrl-chords are required.
type model struct {
	deps     Deps
	composer textarea.Model

	// harnesses lists the configured harness names; harnessIdx selects which
	// one new sessions launch with. Tab opens a picker (scales to many
	// harnesses, unlike cycling).
	harnesses  []string
	harnessIdx int
	picking    bool // harness picker is open
	pickIdx    int  // highlighted row in the picker

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

	gitCache     map[string]gitInfo // live branch/PR per repo path
	gitPollCount int                // gates the slower PR refresh

	previewID   string // session id the current preview belongs to
	previewText string // last fetched preview of the selected session

	width, height int
	status        string
	err           error
}

const previewRows = 8

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

// actionDoneMsg reports the result of a shelled-out or background action.
// restorePrompt, when set, puts a would-be-lost prompt back in the composer
// (a launch failed before the session captured it, so the user can retry).
type actionDoneMsg struct {
	status        string
	restorePrompt string
}

// Run starts the dashboard event loop.
func Run(deps Deps) error {
	applyTheme(deps.Cfg.Theme) // rebuild styles from the configured theme

	ta := textarea.New()
	ta.Placeholder = "Paste a prompt and press Enter to launch a new session…"
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.SetHeight(3)
	ta.CharLimit = 0
	// White text, and no current-line highlight. The cursor line is styled by
	// CursorLine (not Text), so it must carry the white foreground too —
	// otherwise a blank CursorLine drops the color on the line being typed.
	white := lipgloss.NewStyle().Foreground(colWhite)
	ta.FocusedStyle.Text = white
	ta.FocusedStyle.CursorLine = white
	ta.BlurredStyle.Text = white
	ta.BlurredStyle.CursorLine = white
	ta.Focus()

	ri := textinput.New()
	ri.Prompt = ""
	ri.CharLimit = 120
	ri.TextStyle = lipgloss.NewStyle().Foreground(colWhite)

	fi := textinput.New()
	fi.Prompt = ""
	fi.CharLimit = 80
	fi.TextStyle = lipgloss.NewStyle().Foreground(colWhite)

	m := model{
		deps:        deps,
		composer:    ta,
		renameInput: ri,
		filterInput: fi,
		onComposer:  true,
		harnesses:   harnessNames(deps.Cfg),
	}
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
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
	return tea.Batch(m.reload(), tickCmd(), gitTickCmd(), textarea.Blink)
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

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.composer.SetWidth(max(20, msg.Width-2))
		m.renameInput.Width = max(20, msg.Width-4)
		return m, nil

	case sessionsMsg:
		firstLoad := m.gitCache == nil && len(msg.sessions) > 0
		prevID := m.selectedID()
		m.allSessions, m.err = msg.sessions, msg.err
		m.sessions = filterSessions(m.allSessions, m.filter)
		var selCmd tea.Cmd
		m, selCmd = m.reselect(prevID)
		if firstLoad {
			// Show branches promptly rather than waiting for the 5 s git tick
			// (branches only — the slower gh PR lookup stays on the tick).
			m.gitCache = make(map[string]gitInfo)
			return m, tea.Batch(selCmd, gitPollCmd(uniqueRepos(m), false))
		}
		return m, selCmd

	case tickMsg:
		return m, tea.Batch(m.reload(), tickCmd(), m.previewCmd())

	case previewMsg:
		if msg.id == m.selectedID() { // ignore stale results after moving
			m.previewID, m.previewText = msg.id, msg.text
		}
		return m, nil

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
		// Put a would-be-lost prompt back, but only if the box is empty — a
		// background launch stays interactive, so the user may have started
		// typing something new we must not clobber.
		if msg.restorePrompt != "" && m.composer.Value() == "" {
			m.composer.SetValue(msg.restorePrompt)
		}
		return m, m.reload()

	case tea.KeyMsg:
		return m.updateKey(msg)
	}
	return m, nil
}

func (m model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Ctrl+C always quits, regardless of mode (Esc is the cancel key).
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if m.renaming {
		return m.updateRenameKey(msg)
	}
	if m.picking {
		return m.updatePickKey(msg)
	}
	if m.filtering {
		return m.updateFilterKey(msg)
	}
	switch msg.Type {
	case tea.KeyUp:
		return m.moveUp()
	case tea.KeyDown:
		return m.moveDown()
	}
	if m.onComposer {
		return m.updateComposerKey(msg)
	}
	return m.updateSessionKey(msg)
}

// moveUp/moveDown treat the prompt box as the row just below the last session.
func (m model) moveUp() (tea.Model, tea.Cmd) {
	prev := m.selectedID()
	if m.onComposer {
		if len(m.sessions) > 0 {
			m.onComposer = false
			m.cursor = len(m.sessions) - 1
			m.composer.Blur()
		}
	} else if m.cursor > 0 {
		m.cursor--
	}
	return m.afterMove(prev, nil)
}

func (m model) moveDown() (tea.Model, tea.Cmd) {
	prev := m.selectedID()
	if m.onComposer {
		return m, nil // already the bottom row
	}
	var extra tea.Cmd
	if m.cursor < len(m.sessions)-1 {
		m.cursor++
	} else {
		m.onComposer = true
		extra = m.composer.Focus()
	}
	return m.afterMove(prev, extra)
}

// afterMove clears the stale preview and requests a fresh one when the selected
// session changed, batching any extra command.
func (m model) afterMove(prevID string, extra tea.Cmd) (tea.Model, tea.Cmd) {
	if m.selectedID() == prevID {
		return m, extra
	}
	m.previewText, m.previewID = "", ""
	return m, tea.Batch(extra, m.previewCmd())
}

// updateComposerKey runs while the prompt box is selected. Enter launches a
// new session in the background; Ctrl+O (or Alt+Enter) launches and attaches
// immediately — landing in the harness's own input, where its native syntax
// (/commands, @files, completions) is fully available. Tab opens the harness
// picker. Everything else edits the prompt.
func (m model) updateComposerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyTab && len(m.harnesses) > 1:
		// Only intercept Tab when there's a choice to make; with a single
		// harness it falls through to the composer rather than dead-keying.
		m.picking = true
		m.pickIdx = m.harnessIdx
		m.composer.Blur()
		return m, nil
	case msg.Type == tea.KeyCtrlO, msg.Type == tea.KeyEnter && msg.Alt:
		// Launch + attach; an empty prompt opens a fresh harness to type in.
		prompt := strings.TrimSpace(m.composer.Value())
		m.composer.Reset()
		return m, m.execNewAttach(prompt)
	case msg.Type == tea.KeyEnter:
		if prompt := strings.TrimSpace(m.composer.Value()); prompt != "" {
			m.composer.Reset()
			return m, m.execNewBackground(prompt)
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.composer, cmd = m.composer.Update(msg)
	return m, cmd
}

// updatePickKey runs while the harness picker is open: ↑/↓ move, Enter picks,
// Esc/Tab cancels.
func (m model) updatePickKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyUp:
		if m.pickIdx > 0 {
			m.pickIdx--
		}
	case tea.KeyDown:
		if m.pickIdx < len(m.harnesses)-1 {
			m.pickIdx++
		}
	case tea.KeyEnter:
		m.harnessIdx = m.pickIdx
		m.picking = false
		return m, m.composer.Focus()
	case tea.KeyEsc, tea.KeyTab:
		m.picking = false
		return m, m.composer.Focus()
	}
	return m, nil
}

// updateSessionKey runs while a session is selected. Plain letters act on it
// (nothing is typed), so no Ctrl-chords are needed; the Ctrl variants are kept
// as aliases.
func (m model) updateSessionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Esc clears an applied filter — checked before the empty-list guard so it
	// still works when the filter currently hides every row (current() == nil).
	if msg.Type == tea.KeyEsc && m.filter != "" {
		m.filter = ""
		m.sessions = filterSessions(m.allSessions, "")
		return m, nil
	}
	s := m.current()
	if s == nil {
		return m, nil
	}
	switch msg.Type {
	case tea.KeyEnter, tea.KeyRight:
		return m, m.openSession(s)
	case tea.KeyCtrlK:
		return m, m.execKillRemove(s)
	case tea.KeyCtrlR:
		return m, m.execInteractive("resume", s.ID)
	}
	switch msg.String() {
	case "o":
		return m, m.openSession(s)
	case "k":
		return m, m.execKillRemove(s)
	case "r":
		return m, m.execInteractive("resume", s.ID)
	case "e":
		return m.startRename(s)
	case "/":
		m.filtering = true
		m.filterInput.SetValue(m.filter)
		m.filterInput.CursorEnd()
		return m, m.filterInput.Focus()
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

// updateFilterKey runs while the filter bar is open: typing refines the filter
// live, Enter applies and closes, Esc clears and closes, arrows navigate the
// filtered list.
func (m model) updateFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		m.filtering = false
		m.filterInput.Blur()
		return m, nil
	case tea.KeyEsc:
		m.filtering = false
		m.filter = ""
		m.filterInput.Blur()
		m.filterInput.SetValue("")
		m.sessions = filterSessions(m.allSessions, "")
		return m, nil
	case tea.KeyUp:
		return m.moveUp()
	case tea.KeyDown:
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

// updateRenameKey runs while renaming a session (xanax UI label only).
func (m model) updateRenameKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		title := strings.TrimSpace(m.renameInput.Value())
		id := m.renameID
		m.renaming = false
		m.renameInput.Blur()
		if title == "" {
			return m, nil
		}
		return m, m.execRename(id, title)
	case tea.KeyEsc:
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
	return tea.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			return actionDoneMsg{status: fmt.Sprintf("%s failed: %v", sub, err)}
		}
		return actionDoneMsg{}
	})
}

// execKillRemove terminates a live session and deletes it from the store, so it
// disappears from the dashboard.
func (m model) execKillRemove(s *session.Session) tea.Cmd {
	id := s.ID
	sock := filepath.Join(m.deps.SocketDir, id+".sock")
	return func() tea.Msg {
		if attach.Alive(sock) {
			_ = attach.Kill(sock)
		}
		if err := m.deps.Store.DeleteSession(id); err != nil {
			return actionDoneMsg{status: "remove failed: " + err.Error()}
		}
		return actionDoneMsg{status: "removed " + shortID(id)}
	}
}

// newSessionArgs builds the `xanax new` argv for the selected harness. repo,
// when set, targets a specific directory (the dashboard's scope); empty lets
// `xanax new` default to cwd. "--" stops flag parsing so prompts beginning
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
	c := exec.Command(m.deps.SelfPath, newSessionArgs(m.harness(), m.deps.Scope, prompt, true)...)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			return actionDoneMsg{status: "launch failed: " + err.Error(), restorePrompt: prompt}
		}
		return actionDoneMsg{}
	})
}

func (m model) execRename(id, title string) tea.Cmd {
	return func() tea.Msg {
		if err := m.deps.Store.SetTitle(id, title); err != nil {
			return actionDoneMsg{status: "rename failed: " + err.Error()}
		}
		return actionDoneMsg{status: "renamed to " + truncate(title, 40)}
	}
}

// openSession enters the live agent window, or resumes it natively if the
// supervisor is gone but a harness session ref was captured.
func (m model) openSession(s *session.Session) tea.Cmd {
	if m.alive(s.ID) {
		return m.execInteractive("attach", s.ID)
	}
	if s.HarnessSessionRef != "" {
		return m.execInteractive("resume", s.ID)
	}
	msg := "session " + shortID(s.ID) + " has ended — press k to remove"
	return func() tea.Msg { return actionDoneMsg{status: msg} }
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

// previewCmd fetches a peek of the selected session's screen (nothing while
// the prompt box is selected). Runs off the main loop.
func (m model) previewCmd() tea.Cmd {
	s := m.current()
	if s == nil {
		return nil
	}
	id, sock, cols := s.ID, filepath.Join(m.deps.SocketDir, s.ID+".sock"), m.width-4
	return func() tea.Msg {
		return previewMsg{id: id, text: attach.Peek(sock, previewRows, cols)}
	}
}

type previewMsg struct {
	id   string
	text string
}

func (m model) alive(id string) bool {
	return attach.Alive(filepath.Join(m.deps.SocketDir, id+".sock"))
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
	case session.StatusRunning, session.StatusStarting:
		return 1
	case session.StatusCompleted:
		return 2
	case session.StatusCancelled:
		return 3
	case session.StatusFailed:
		return 4
	default:
		return 5
	}
}

func groupLabel(rank int) string {
	switch rank {
	case 0:
		return "Needs input"
	case 1:
		return "Running"
	case 2:
		return "Completed"
	case 3:
		return "Cancelled"
	case 4:
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

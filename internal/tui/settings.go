package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/LeJamon/rvr/internal/config"
)

// openSettings opens the keybindings editor over the current view. It is reached
// from the session context, so closing it simply returns there (the composer
// stays blurred) rather than refocusing the prompt box like the harness picker.
func (m model) openSettings() (tea.Model, tea.Cmd) {
	m.settingsOn = true
	m.settingsCapture = false
	m.settingsSearch = ""
	m.settingsInput.SetValue("")
	m.settingsIdx = 0
	m.settingsScroll = 0
	return m, m.settingsInput.Focus()
}

// closeSettings dismisses the editor without refocusing the composer.
func (m model) closeSettings() (tea.Model, tea.Cmd) {
	m.settingsOn = false
	m.settingsCapture = false
	m.settingsInput.Blur()
	return m, nil
}

// filteredActions returns the bindable actions whose name or description matches
// the (case-insensitive) search text. An empty search keeps them all.
func (m model) filteredActions() []config.KeyAction {
	all := m.deps.Cfg.Keys.Actions()
	if m.settingsSearch == "" {
		return all
	}
	q := strings.ToLower(m.settingsSearch)
	out := make([]config.KeyAction, 0, len(all))
	for _, a := range all {
		if strings.Contains(a.Name, q) || strings.Contains(strings.ToLower(a.Desc), q) {
			out = append(out, a)
		}
	}
	return out
}

// updateSettingsKey handles keys while the keybindings editor is open. In list
// mode the search box is focused (type to filter); ↑/↓ move the highlight,
// Confirm starts capturing a new key for the highlighted action, Cancel closes.
// In capture mode the next keypress becomes the binding (Esc aborts).
func (m model) updateSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.settingsCapture {
		return m.captureKeyBinding(msg)
	}
	k := m.keys()
	switch {
	case keyMatches(k.Up, msg) && !textInputKey(msg):
		m.moveSettings(-1)
		return m, nil
	case keyMatches(k.Down, msg) && !textInputKey(msg):
		m.moveSettings(1)
		return m, nil
	case keyMatches(k.Confirm, msg):
		if len(m.filteredActions()) == 0 {
			return m, nil
		}
		m.settingsCapture = true // capture keys for the highlighted action
		m.settingsPending = nil
		return m, nil
	case keyMatches(k.Cancel, msg):
		return m.closeSettings()
	}
	// Anything else filters the list; the highlight resets to the top match.
	var cmd tea.Cmd
	m.settingsInput, cmd = m.settingsInput.Update(msg)
	m.settingsSearch = m.settingsInput.Value()
	m.settingsIdx = 0
	m.settingsScroll = 0
	return m, cmd
}

// captureKeyBinding accumulates the pressed keys into settingsPending. Each key
// is added once (a repeat is ignored); Enter commits them as the action's new
// binding, Esc aborts with no change. Enter, Esc and the quit key are the
// capture's own controls (the quit key is intercepted a level up in updateKey),
// so binding an action to one of them must be done in the config file.
func (m model) captureKeyBinding(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc: // reliable abort regardless of the cancel binding
		m.settingsCapture = false
		m.settingsPending = nil
		return m, nil
	case tea.KeyEnter: // commit whatever has been captured so far
		return m.commitCapture()
	}
	key := canonKey(msg.String())
	if key == "" {
		return m, nil // ignore key events bubbletea reports with no name
	}
	if !slices.Contains(m.settingsPending, key) {
		m.settingsPending = append(m.settingsPending, key)
	}
	return m, nil
}

// commitCapture writes the accumulated keys as the highlighted action's binding
// and persists them. An empty capture is treated as a cancel so a stray Enter
// never silently unbinds an action (unbinding stays a deliberate `= []` edit).
func (m model) commitCapture() (tea.Model, tea.Cmd) {
	pending := m.settingsPending
	m.settingsCapture = false
	m.settingsPending = nil
	if len(pending) == 0 {
		return m, nil
	}
	filtered := m.filteredActions()
	if m.settingsIdx >= len(filtered) {
		return m, nil
	}
	name := filtered[m.settingsIdx].Name

	if m.deps.ConfigPath == "" {
		m.status = "rebind failed: no config path"
		return m, nil
	}
	unlock, err := acquireConfigLock(m.deps.ConfigPath)
	if err != nil {
		m.status = "rebind failed: " + err.Error()
		return m, nil
	}
	defer unlock()
	orig, origErr := os.ReadFile(m.deps.ConfigPath)
	if err := setKeyBindingInConfig(m.deps.ConfigPath, name, pending); err != nil {
		m.status = "rebind failed: " + err.Error()
		return m, nil
	}
	cfg, err := config.Load(m.deps.ConfigPath)
	if err != nil {
		restoreConfig(m.deps.ConfigPath, orig, origErr)
		m.status = "rebind reload failed: " + err.Error()
		return m, nil
	}
	m.deps.Cfg = cfg // new bindings take effect immediately (keyMatches reads Cfg.Keys)
	m.status = fmt.Sprintf("bound %s to %s", name, strings.Join(pending, ", "))
	return m, nil
}

// moveSettings moves the highlight by delta within the filtered list, scrolling
// the window so the selected row stays visible.
func (m *model) moveSettings(delta int) {
	n := len(m.filteredActions())
	if n == 0 {
		return
	}
	m.settingsIdx = min(max(m.settingsIdx+delta, 0), n-1)
	vis := m.visibleRows()
	if m.settingsIdx < m.settingsScroll {
		m.settingsScroll = m.settingsIdx
	} else if m.settingsIdx >= m.settingsScroll+vis {
		m.settingsScroll = m.settingsIdx - vis + 1
	}
	if m.settingsScroll < 0 {
		m.settingsScroll = 0
	}
}

// setKeyBindingInConfig writes `<action> = [<keys>]` under the [keys] table in
// the config file, replacing an existing assignment for that action or inserting
// one, and creating the [keys] table when absent. Everything else in the file —
// other bindings, tables, comments — is preserved. It tolerates the hand-written
// TOML the user is invited to edit: an inline comment or interior spacing on the
// [keys] header, tab- or multi-space-aligned assignments, and multi-line array
// values. Reuses tomlStringArray for quoting.
func setKeyBindingInConfig(path, action string, keys []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	line := fmt.Sprintf("%s = %s", action, tomlStringArray(keys))
	lines := strings.Split(string(data), "\n")

	// Find the [keys] table header (ignoring an inline comment or interior spaces).
	start := -1
	for i, ln := range lines {
		if tomlTableName(ln) == "keys" {
			start = i
			break
		}
	}
	if start < 0 {
		// No [keys] table yet — append one after the existing content.
		body := strings.TrimRight(string(data), "\n")
		var b strings.Builder
		if body != "" {
			b.WriteString(body)
			b.WriteString("\n\n")
		}
		b.WriteString("[keys]\n")
		b.WriteString(line)
		b.WriteString("\n")
		return writeFileAtomic(path, []byte(b.String()), 0o600)
	}

	// The table runs to the next table header (or EOF). Replace the action's
	// assignment if it is already there. Matching the key left of '=' keeps a
	// prefix like "quit" from hitting "quit_list" and handles any alignment.
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if tomlTableName(lines[i]) != "" {
			end = i
			break
		}
	}
	for i := start + 1; i < end; i++ {
		if tomlAssignmentKey(lines[i]) != action {
			continue
		}
		// Extend over a multi-line array value so its continuation lines are not
		// orphaned (the writer emits single-line arrays, but the file may be
		// hand-written).
		last := i
		for depth := tomlBracketDelta(lines[i]); depth > 0 && last+1 < end; {
			last++
			depth += tomlBracketDelta(lines[last])
		}
		out := make([]string, 0, len(lines)-(last-i))
		out = append(out, lines[:i]...)
		out = append(out, line)
		out = append(out, lines[last+1:]...)
		return writeFileAtomic(path, []byte(strings.Join(out, "\n")), 0o600)
	}
	// Not present — insert right after the [keys] header.
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:start+1]...)
	out = append(out, line)
	out = append(out, lines[start+1:]...)
	return writeFileAtomic(path, []byte(strings.Join(out, "\n")), 0o600)
}

// tomlTableName returns the bare name of a TOML table header line ("[keys]" ->
// "keys", "[ keys ]  # note" -> "keys"), or "" when the line is not a table
// header. It lets the writer locate the [keys] table even when the header
// carries an inline comment or interior spacing.
func tomlTableName(line string) string {
	s := strings.TrimSpace(line)
	if i := strings.IndexByte(s, '#'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if len(s) < 2 || s[0] != '[' || s[len(s)-1] != ']' {
		return ""
	}
	return strings.TrimSpace(s[1 : len(s)-1])
}

// tomlAssignmentKey returns the key a TOML assignment line binds — the text left
// of its first top-level '=' — or "" when the line is blank, a comment, or an
// array continuation. A '=' inside a string is ignored so a value never looks
// like a key. This matches "remove =", "remove\t=" and "remove=" alike.
func tomlAssignmentKey(line string) string {
	inBasic, inLiteral := false, false
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case inBasic:
			if c == '\\' {
				i++ // skip the escaped char
			} else if c == '"' {
				inBasic = false
			}
		case inLiteral:
			if c == '\'' {
				inLiteral = false
			}
		case c == '"':
			inBasic = true
		case c == '\'':
			inLiteral = true
		case c == '#':
			return "" // a comment before any '='
		case c == '=':
			return strings.TrimSpace(line[:i])
		}
	}
	return ""
}

// tomlBracketDelta returns the net array-bracket depth ('[' minus ']') a line
// contributes, ignoring brackets inside strings and after a '#' comment. A
// positive running total across lines means an array value is still open, so its
// assignment continues onto the next line.
func tomlBracketDelta(line string) int {
	depth := 0
	inBasic, inLiteral := false, false
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case inBasic:
			if c == '\\' {
				i++
			} else if c == '"' {
				inBasic = false
			}
		case inLiteral:
			if c == '\'' {
				inLiteral = false
			}
		case c == '"':
			inBasic = true
		case c == '\'':
			inLiteral = true
		case c == '#':
			return depth
		case c == '[':
			depth++
		case c == ']':
			depth--
		}
	}
	return depth
}

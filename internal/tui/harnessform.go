package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/LeJamon/rvr/internal/config"
)

// The add-harness form fields, in order.
const (
	fieldName = iota
	fieldCommand
	fieldPromptArg
	fieldIdleTimeout
	fieldWaitingPattern
	numFields
)

var formLabels = [numFields]string{"name", "command", "prompt arg", "idle timeout (s)", "waiting pattern"}

var harnessNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// harnessSpec is the validated content of the add-harness form.
type harnessSpec struct {
	name           string
	command        string
	args           []string
	promptArg      string
	idleTimeout    int
	waitingPattern string
}

// newFormInputs builds the blank add-harness inputs.
func newFormInputs() []textinput.Model {
	ins := make([]textinput.Model, numFields)
	placeholders := [numFields]string{"goose", "goose run", "e.g. --message (blank = type it in)", "e.g. 120", `e.g. \(y/n\)`}
	for i := range ins {
		ti := textinput.New()
		ti.Prompt = ""
		ti.CharLimit = 200
		ti.Placeholder = placeholders[i]
		ti.TextStyle = whiteText()
		ins[i] = ti
	}
	return ins
}

// formLabelCol is the column width the field labels are padded to, so the
// inputs line up in a neat second column inside the form modal.
const formLabelCol = 16

// startAddHarness opens a blank harness form (from the harness picker).
func (m model) startAddHarness() (tea.Model, tea.Cmd) {
	m.picking = false
	m.searchFocused = false
	m.addingHarness = true
	m.editHarness = "" // add mode
	m.formField = 0
	m.formErr = ""
	for i := range m.formInputs {
		m.formInputs[i].SetValue("")
		m.formInputs[i].Blur()
	}
	m.syncFormWidths()
	return m, m.formInputs[0].Focus()
}

// startModifyHarness opens the form pre-filled with the highlighted harness so
// its config can be edited in place (m from the harness picker). editHarness
// records the original name, both as the modify-mode flag and as the block to
// replace on save (renames rewrite it under the new name).
func (m model) startModifyHarness() (tea.Model, tea.Cmd) {
	filtered := m.filteredHarnesses()
	if len(filtered) == 0 {
		return m.cancelPick()
	}
	idx := min(max(m.pickIdx, 0), len(filtered)-1)
	name := filtered[idx]
	h := m.deps.Cfg.Harnesses[name]

	m.picking = false
	m.searchFocused = false
	m.addingHarness = true
	m.editHarness = name
	m.formField = 0
	m.formErr = ""

	command := harnessCommandField(h)
	idle := ""
	if h.IdleTimeout > 0 {
		idle = strconv.Itoa(h.IdleTimeout)
	}
	vals := [numFields]string{name, command, h.PromptArg, idle, h.WaitingPattern}
	for i := range m.formInputs {
		m.formInputs[i].SetValue(vals[i])
		m.formInputs[i].CursorEnd()
		m.formInputs[i].Blur()
	}
	m.syncFormWidths()
	return m, m.formInputs[0].Focus()
}

// syncFormWidths sizes the form inputs to the modal so long values scroll
// within the box instead of overflowing its border.
func (m *model) syncFormWidths() {
	iw := max(10, pickerModalWidth(m.width)-formLabelCol-4) // marker + label + a space
	for i := range m.formInputs {
		m.formInputs[i].Width = iw
	}
}

// updateFormKey handles keys while the add-harness form is open.
func (m model) updateFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := m.keys()
	switch {
	case keyMatches(k.Cancel, msg):
		m.addingHarness = false
		return m, m.composer.Focus()
	case keyMatches(k.FormNext, msg):
		return m.focusFormField(m.formField + 1)
	case keyMatches(k.FormPrev, msg):
		return m.focusFormField(m.formField - 1)
	case keyMatches(k.Confirm, msg):
		return m.submitHarness()
	}
	var cmd tea.Cmd
	m.formInputs[m.formField], cmd = m.formInputs[m.formField].Update(msg)
	return m, cmd
}

func (m model) focusFormField(i int) (tea.Model, tea.Cmd) {
	n := len(m.formInputs)
	i = ((i % n) + n) % n // wrap both directions
	for j := range m.formInputs {
		m.formInputs[j].Blur()
	}
	m.formField = i
	return m, m.formInputs[i].Focus()
}

// submitHarness validates the form and writes it to config.toml: a new
// [harness.<name>] block in add mode, or a rewrite of the edited block in modify
// mode. It reloads config, selects the harness, and closes the form. On any
// failure the form stays open with the error and the file is left intact.
func (m model) submitHarness() (tea.Model, tea.Cmd) {
	spec, err := m.parseHarnessForm()
	if err != nil {
		m.formErr = err.Error()
		return m, nil
	}
	if m.deps.ConfigPath == "" {
		m.formErr = "no config path"
		return m, nil
	}
	unlock, err := acquireConfigLock(m.deps.ConfigPath)
	if err != nil {
		m.formErr = "lock failed: " + err.Error()
		return m, nil
	}
	defer unlock()

	// Check names against the config on disk, not the (possibly stale) in-memory
	// list, and refuse to touch a config we cannot parse — either would risk a
	// duplicate [harness.<name>] table or compounding existing corruption.
	existing, err := config.Load(m.deps.ConfigPath)
	if err != nil {
		m.formErr = "existing config invalid: " + err.Error()
		return m, nil
	}
	if err := m.checkHarnessName(existing, spec.name); err != "" {
		m.formErr = err
		return m, nil
	}

	// Snapshot the (valid) file so a write that does not round-trip through TOML
	// — e.g. a value strconv.Quote escapes to an invalid form — is rolled back
	// instead of leaving config.toml unparseable.
	orig, origErr := os.ReadFile(m.deps.ConfigPath)
	if err := m.writeHarness(existing, spec); err != nil {
		m.formErr = "write failed: " + err.Error()
		return m, nil
	}
	cfg, err := config.Load(m.deps.ConfigPath)
	if err != nil {
		restoreConfig(m.deps.ConfigPath, orig, origErr)
		m.formErr = "reload failed: " + err.Error()
		return m, nil
	}
	verb := "added"
	if m.editHarness != "" {
		verb = "updated"
	}
	m.deps.Cfg = cfg
	m.harnesses = harnessNames(cfg)
	if i := slices.Index(m.harnesses, spec.name); i >= 0 {
		m.harnessIdx = i
	}
	m.addingHarness = false
	m.editHarness = ""
	m.status = verb + " harness " + spec.name
	return m, m.composer.Focus()
}

// checkHarnessName rejects a name that would collide: any existing harness in
// add mode, or a *different* existing harness in modify mode (renaming onto a
// name already in use). It also guards the modify target still being present.
// Returns "" when the name is acceptable.
func (m model) checkHarnessName(existing *config.Config, name string) string {
	if m.editHarness == "" {
		if _, ok := existing.Harnesses[name]; ok {
			return "a harness named " + name + " already exists"
		}
		return ""
	}
	if _, ok := existing.Harnesses[m.editHarness]; !ok {
		return m.editHarness + " no longer exists in config"
	}
	if name != m.editHarness {
		if _, ok := existing.Harnesses[name]; ok {
			return "a harness named " + name + " already exists"
		}
	}
	return ""
}

// writeHarness appends a new generic block (add) or rewrites the edited block
// (modify). Modify preserves the fields the form does not expose — adapter,
// env, resume_args, prompt_positional — and follows a rename of the default
// harness through to default_harness so the reloaded config stays valid.
func (m model) writeHarness(existing *config.Config, spec harnessSpec) error {
	if m.editHarness == "" {
		return writeHarnessBlock(m.deps.ConfigPath, spec)
	}
	h := existing.Harnesses[m.editHarness]
	// Keep the original command/args when the command field round-trips to the
	// same text: splitting it on spaces would otherwise corrupt an arg that
	// legitimately contains one (e.g. args = ["--message", "hello world"]) on a
	// save that never touched the field.
	if harnessCommandField(config.Harness{Command: spec.command, Args: spec.args}) != harnessCommandField(h) {
		h.Command = spec.command
		h.Args = spec.args
	}
	h.PromptArg = spec.promptArg
	h.IdleTimeout = spec.idleTimeout
	h.WaitingPattern = spec.waitingPattern
	if err := replaceHarnessBlock(m.deps.ConfigPath, m.editHarness, spec.name, h); err != nil {
		return err
	}
	if spec.name != m.editHarness && existing.DefaultHarness == m.editHarness {
		return setDefaultInConfig(m.deps.ConfigPath, spec.name)
	}
	return nil
}

// harnessCommandField renders a harness's command and args as the single
// space-joined string the form's command field shows and re-parses. Modify
// pre-fills from it and compares against it, so a save that leaves the field
// untouched keeps the original args verbatim instead of re-splitting one that
// happens to contain a space.
func harnessCommandField(h config.Harness) string {
	if len(h.Args) == 0 {
		return h.Command
	}
	return strings.Join(append([]string{h.Command}, h.Args...), " ")
}

// parseHarnessForm validates the form inputs and returns the harness to write.
// The command field is split on whitespace so a multi-word command (e.g.
// "goose run") becomes command + args rather than an unlaunchable exec path.
func (m model) parseHarnessForm() (harnessSpec, error) {
	name := strings.TrimSpace(m.formInputs[fieldName].Value())
	command := strings.TrimSpace(m.formInputs[fieldCommand].Value())
	promptArg := strings.TrimSpace(m.formInputs[fieldPromptArg].Value())
	idleStr := strings.TrimSpace(m.formInputs[fieldIdleTimeout].Value())
	pattern := strings.TrimSpace(m.formInputs[fieldWaitingPattern].Value())

	switch {
	case name == "":
		return harnessSpec{}, fmt.Errorf("name is required")
	case !harnessNameRe.MatchString(name):
		return harnessSpec{}, fmt.Errorf("name may contain only letters, digits, '-' and '_'")
	case command == "":
		return harnessSpec{}, fmt.Errorf("command is required")
	}

	idle := 0
	if idleStr != "" {
		n, err := strconv.Atoi(idleStr)
		if err != nil || n < 0 {
			return harnessSpec{}, fmt.Errorf("idle timeout must be a whole number of seconds >= 0")
		}
		idle = n
	}
	if pattern != "" {
		if _, err := regexp.Compile(pattern); err != nil {
			return harnessSpec{}, fmt.Errorf("waiting pattern is not a valid regexp: %v", err)
		}
	}

	fields := strings.Fields(command)
	return harnessSpec{
		name:           name,
		command:        fields[0],
		args:           fields[1:],
		promptArg:      promptArg,
		idleTimeout:    idle,
		waitingPattern: pattern,
	}, nil
}

// writeHarnessBlock appends a generic [harness.<name>] table to the config file
// (creating it if absent). Appending preserves the user's existing file and
// comments; submitHarness pre-validates uniqueness against the file so no
// duplicate table results.
func writeHarnessBlock(path string, h harnessSpec) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return appendHarnessBlock(path, h.name, config.Harness{
		Adapter:        config.AdapterGeneric,
		Command:        h.command,
		Args:           h.args,
		PromptArg:      h.promptArg,
		IdleTimeout:    h.idleTimeout,
		WaitingPattern: h.waitingPattern,
	})
}

// appendHarnessBlock writes a fresh [harness.<name>] table at the end of the
// file, preserving everything above it.
func appendHarnessBlock(path, name string, h config.Harness) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	data = append(data, []byte("\n"+harnessBlock(name, h))...)
	return writeFileAtomic(path, data, 0o600)
}

// replaceHarnessBlock rewrites the [harness.oldName] table in place with a fresh
// block for newName (a rename when they differ), preserving the rest of the
// file — other harnesses, default_harness, and comments outside the block. When
// no such table exists (a built-in harness overridden only by defaults, or one
// never written to this file) it appends the block instead, which merges over
// the built-in on reload.
func replaceHarnessBlock(path, oldName, newName string, h config.Harness) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	header := fmt.Sprintf("[harness.%s]", oldName)
	start := -1
	for i, ln := range lines {
		if strings.TrimSpace(ln) == header {
			start = i
			break
		}
	}
	if start < 0 {
		return appendHarnessBlock(path, newName, h)
	}
	// The block runs to the next table header (or EOF). Keep any blank lines that
	// precede that header as a separator rather than swallowing them.
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "[") {
			end = i
			break
		}
	}
	for end > start+1 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	block := strings.Split(strings.TrimRight(harnessBlock(newName, h), "\n"), "\n")
	out := make([]string, 0, len(lines)-(end-start)+len(block))
	out = append(out, lines[:start]...)
	out = append(out, block...)
	out = append(out, lines[end:]...)
	return writeFileAtomic(path, []byte(strings.Join(out, "\n")), 0o600)
}

// harnessBlock renders a complete [harness.<name>] TOML table for h, omitting
// empty fields. Shared by add (append) and modify (replace) so both round-trip
// cleanly through config.Load and modify never drops a field the form omits.
func harnessBlock(name string, h config.Harness) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[harness.%s]\n", name)
	fmt.Fprintf(&b, "adapter = %s\n", strconv.Quote(h.Adapter))
	fmt.Fprintf(&b, "command = %s\n", strconv.Quote(h.Command))
	if len(h.Args) > 0 {
		fmt.Fprintf(&b, "args = %s\n", tomlStringArray(h.Args))
	}
	if len(h.ResumeArgs) > 0 {
		fmt.Fprintf(&b, "resume_args = %s\n", tomlStringArray(h.ResumeArgs))
	}
	if h.PromptArg != "" {
		fmt.Fprintf(&b, "prompt_arg = %s\n", strconv.Quote(h.PromptArg))
	}
	if h.PromptPositional {
		b.WriteString("prompt_positional = true\n")
	}
	if h.IdleTimeout > 0 {
		fmt.Fprintf(&b, "idle_timeout = %d\n", h.IdleTimeout)
	}
	if h.WaitingPattern != "" {
		fmt.Fprintf(&b, "waiting_pattern = %s\n", strconv.Quote(h.WaitingPattern))
	}
	if len(h.Env) > 0 {
		keys := make([]string, 0, len(h.Env))
		for k := range h.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = strconv.Quote(k) + " = " + strconv.Quote(h.Env[k])
		}
		fmt.Fprintf(&b, "env = {%s}\n", strings.Join(parts, ", "))
	}
	return b.String()
}

// tomlStringArray renders a []string as a TOML inline array of quoted values.
func tomlStringArray(vals []string) string {
	quoted := make([]string, len(vals))
	for i, v := range vals {
		quoted[i] = strconv.Quote(v)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// restoreConfig rolls the config file back to its pre-append content, or removes
// it when it did not exist before, so a block that fails to reload never leaves
// the file corrupt. origErr is the error from reading the file before the write
// (non-nil means it was absent).
func restoreConfig(path string, orig []byte, origErr error) {
	if origErr != nil {
		_ = os.Remove(path)
		return
	}
	_ = writeFileAtomic(path, orig, 0o600)
}

// setDefaultInConfig writes default_harness = "name" to the config file.
// If a default_harness line already exists, it replaces it. Otherwise, it
// prepends one at the top of the file.
func setDefaultInConfig(path string, name string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	lines := strings.Split(string(data), "\n")
	var out []string
	found := false
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "default_harness") {
			found = true
			// Replace the old default_harness line.
			out = append(out, fmt.Sprintf("default_harness = %q", name))
			continue
		}
		out = append(out, line)
	}
	// If no line was replaced (not found), prepend at the top.
	if !found {
		out = append([]string{fmt.Sprintf("default_harness = %q", name)}, out...)
	}
	return writeFileAtomic(path, []byte(strings.Join(out, "\n")), 0o600)
}

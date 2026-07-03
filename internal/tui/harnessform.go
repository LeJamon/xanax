package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"xanax/internal/config"
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

// startAddHarness opens the add-harness form (from the harness picker).
func (m model) startAddHarness() (tea.Model, tea.Cmd) {
	m.picking = false
	m.addingHarness = true
	m.formField = 0
	m.formErr = ""
	for i := range m.formInputs {
		m.formInputs[i].SetValue("")
		m.formInputs[i].Blur()
	}
	return m, m.formInputs[0].Focus()
}

// updateFormKey handles keys while the add-harness form is open.
func (m model) updateFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.addingHarness = false
		return m, m.composer.Focus()
	case tea.KeyTab, tea.KeyDown:
		return m.focusFormField(m.formField + 1)
	case tea.KeyShiftTab, tea.KeyUp:
		return m.focusFormField(m.formField - 1)
	case tea.KeyEnter:
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

// submitHarness validates the form, appends the harness to config.toml, reloads
// config, and selects the new harness.
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

	// Check uniqueness against the config on disk, not the (possibly stale)
	// in-memory list, and refuse to append to a config we cannot parse — either
	// would risk writing a duplicate [harness.<name>] table that breaks loading.
	existing, err := config.Load(m.deps.ConfigPath)
	if err != nil {
		m.formErr = "existing config invalid: " + err.Error()
		return m, nil
	}
	if _, ok := existing.Harnesses[spec.name]; ok {
		m.formErr = "a harness named " + spec.name + " already exists"
		return m, nil
	}

	// Snapshot the (valid) file so a block that appends but does not round-trip
	// through TOML — e.g. a value with a byte strconv.Quote escapes as \a — is
	// rolled back instead of leaving config.toml unparseable.
	orig, origErr := os.ReadFile(m.deps.ConfigPath)

	if err := writeHarnessBlock(m.deps.ConfigPath, spec); err != nil {
		m.formErr = "write failed: " + err.Error()
		return m, nil
	}
	cfg, err := config.Load(m.deps.ConfigPath)
	if err != nil {
		restoreConfig(m.deps.ConfigPath, orig, origErr)
		m.formErr = "reload failed: " + err.Error()
		return m, nil
	}
	m.deps.Cfg = cfg
	m.harnesses = harnessNames(cfg)
	if i := slices.Index(m.harnesses, spec.name); i >= 0 {
		m.harnessIdx = i
	}
	m.addingHarness = false
	m.status = "added harness " + spec.name
	return m, m.composer.Focus()
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
	var b strings.Builder
	fmt.Fprintf(&b, "\n[harness.%s]\nadapter = \"generic\"\ncommand = %s\n", h.name, strconv.Quote(h.command))
	if len(h.args) > 0 {
		quoted := make([]string, len(h.args))
		for i, a := range h.args {
			quoted[i] = strconv.Quote(a)
		}
		fmt.Fprintf(&b, "args = [%s]\n", strings.Join(quoted, ", "))
	}
	if h.promptArg != "" {
		fmt.Fprintf(&b, "prompt_arg = %s\n", strconv.Quote(h.promptArg))
	}
	if h.idleTimeout > 0 {
		fmt.Fprintf(&b, "idle_timeout = %d\n", h.idleTimeout)
	}
	if h.waitingPattern != "" {
		fmt.Fprintf(&b, "waiting_pattern = %s\n", strconv.Quote(h.waitingPattern))
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(b.String())
	return err
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
	_ = os.WriteFile(path, orig, 0o600)
}

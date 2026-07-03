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
	numFields
)

var formLabels = [numFields]string{"name", "command", "prompt arg (optional)"}

var harnessNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// newFormInputs builds the blank add-harness inputs.
func newFormInputs() []textinput.Model {
	ins := make([]textinput.Model, numFields)
	placeholders := [numFields]string{"goose", "goose", "--message"}
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
	name := strings.TrimSpace(m.formInputs[fieldName].Value())
	command := strings.TrimSpace(m.formInputs[fieldCommand].Value())
	promptArg := strings.TrimSpace(m.formInputs[fieldPromptArg].Value())

	switch {
	case name == "":
		m.formErr = "name is required"
		return m, nil
	case !harnessNameRe.MatchString(name):
		m.formErr = "name may contain only letters, digits, '-' and '_'"
		return m, nil
	case slices.Contains(m.harnesses, name):
		m.formErr = "a harness named " + name + " already exists"
		return m, nil
	case command == "":
		m.formErr = "command is required"
		return m, nil
	case m.deps.ConfigPath == "":
		m.formErr = "no config path"
		return m, nil
	}

	if err := writeHarnessBlock(m.deps.ConfigPath, name, command, promptArg); err != nil {
		m.formErr = "write failed: " + err.Error()
		return m, nil
	}
	cfg, err := config.Load(m.deps.ConfigPath)
	if err != nil {
		m.formErr = "reload failed: " + err.Error()
		return m, nil
	}
	m.deps.Cfg = cfg
	m.harnesses = harnessNames(cfg)
	if i := slices.Index(m.harnesses, name); i >= 0 {
		m.harnessIdx = i
	}
	m.addingHarness = false
	m.status = "added harness " + name
	return m, m.composer.Focus()
}

// writeHarnessBlock appends a generic [harness.<name>] table to the config file
// (creating it if absent). Appending preserves the user's existing file and
// comments; the name is pre-validated as unique so no duplicate table results.
func writeHarnessBlock(path, name, command, promptArg string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n[harness.%s]\nadapter = \"generic\"\ncommand = %s\n", name, strconv.Quote(command))
	if promptArg != "" {
		fmt.Fprintf(&b, "prompt_arg = %s\n", strconv.Quote(promptArg))
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(b.String())
	return err
}

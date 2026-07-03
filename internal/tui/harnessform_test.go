package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"xanax/internal/config"
)

func formModel(t *testing.T) (model, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	m := newTestModel(nil)
	m.deps.ConfigPath = path
	m.addingHarness = true
	return m, path
}

func TestAddHarnessWritesAndReloads(t *testing.T) {
	m, path := formModel(t)
	m.formInputs[fieldName].SetValue("goose")
	m.formInputs[fieldCommand].SetValue("goose")
	m.formInputs[fieldPromptArg].SetValue("--message")

	next, _ := m.submitHarness()
	m = next.(model)
	if m.addingHarness || m.formErr != "" {
		t.Fatalf("submit failed: adding=%v err=%q", m.addingHarness, m.formErr)
	}

	// The file has a valid, reloadable harness block.
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "[harness.goose]") {
		t.Errorf("config missing harness block:\n%s", data)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("written config does not reload: %v", err)
	}
	if h := cfg.Harnesses["goose"]; h.Adapter != "generic" || h.Command != "goose" || h.PromptArg != "--message" {
		t.Errorf("reloaded harness wrong: %+v", h)
	}

	// The new harness is now listed and selected.
	if !slices.Contains(m.harnesses, "goose") {
		t.Errorf("harness list missing goose: %v", m.harnesses)
	}
	if m.harness() != "goose" {
		t.Errorf("new harness not selected: %q", m.harness())
	}
}

func TestAddHarnessRejectsDuplicate(t *testing.T) {
	m, _ := formModel(t) // built-in harnesses: opencode, pi
	m.formInputs[fieldName].SetValue("opencode")
	m.formInputs[fieldCommand].SetValue("x")
	next, _ := m.submitHarness()
	m = next.(model)
	if m.formErr == "" || !m.addingHarness {
		t.Error("duplicate name should be rejected and keep the form open")
	}
}

func TestAddHarnessRejectsBadNameOrEmpty(t *testing.T) {
	m, _ := formModel(t)
	m.formInputs[fieldName].SetValue("bad name!")
	m.formInputs[fieldCommand].SetValue("x")
	if next, _ := m.submitHarness(); next.(model).formErr == "" {
		t.Error("invalid name should error")
	}

	m2, _ := formModel(t)
	m2.formInputs[fieldName].SetValue("ok")
	m2.formInputs[fieldCommand].SetValue("") // missing command
	if next, _ := m2.submitHarness(); next.(model).formErr == "" {
		t.Error("empty command should error")
	}
}

func TestWriteHarnessBlockAppendPreservesExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("default_harness = \"pi\"\n"), 0o600)
	if err := writeHarnessBlock(path, "goose", "goose bin", ""); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	s := string(data)
	if !strings.Contains(s, `default_harness = "pi"`) || !strings.Contains(s, "[harness.goose]") {
		t.Errorf("append did not preserve + add:\n%s", s)
	}
	if _, err := config.Load(path); err != nil {
		t.Errorf("result not loadable: %v", err)
	}
}

func TestFormFieldFocusWraps(t *testing.T) {
	m, _ := formModel(t)
	next, _ := m.focusFormField(-1) // wrap to last
	if next.(model).formField != numFields-1 {
		t.Errorf("focus -1 = %d, want %d", next.(model).formField, numFields-1)
	}
	next, _ = m.focusFormField(numFields) // wrap to 0
	if next.(model).formField != 0 {
		t.Errorf("focus wrap = %d, want 0", next.(model).formField)
	}
}

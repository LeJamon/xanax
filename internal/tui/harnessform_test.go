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

// TestAddHarnessSplitsCommandArgs confirms a multi-word command is written as
// command + args (not one unlaunchable exec path).
func TestAddHarnessSplitsCommandArgs(t *testing.T) {
	m, path := formModel(t)
	m.formInputs[fieldName].SetValue("goose")
	m.formInputs[fieldCommand].SetValue("goose run")

	next, _ := m.submitHarness()
	m = next.(model)
	if m.formErr != "" {
		t.Fatalf("submit failed: %q", m.formErr)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	h := cfg.Harnesses["goose"]
	if h.Command != "goose" || !slices.Equal(h.Args, []string{"run"}) {
		t.Errorf("command not split: command=%q args=%v", h.Command, h.Args)
	}
}

// TestAddHarnessWritesInferenceFields confirms the idle-timeout and
// waiting-pattern inputs land in the config so a dashboard-added harness can
// actually infer state.
func TestAddHarnessWritesInferenceFields(t *testing.T) {
	m, path := formModel(t)
	m.formInputs[fieldName].SetValue("goose")
	m.formInputs[fieldCommand].SetValue("goose")
	m.formInputs[fieldIdleTimeout].SetValue("120")
	m.formInputs[fieldWaitingPattern].SetValue(`\(y/n\)`)

	next, _ := m.submitHarness()
	m = next.(model)
	if m.formErr != "" {
		t.Fatalf("submit failed: %q", m.formErr)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if h := cfg.Harnesses["goose"]; h.IdleTimeout != 120 || h.WaitingPattern != `\(y/n\)` {
		t.Errorf("inference fields not written: idle=%d pattern=%q", h.IdleTimeout, h.WaitingPattern)
	}
}

func TestAddHarnessRejectsBadIdleOrPattern(t *testing.T) {
	m, _ := formModel(t)
	m.formInputs[fieldName].SetValue("ok")
	m.formInputs[fieldCommand].SetValue("x")
	m.formInputs[fieldIdleTimeout].SetValue("-1")
	if next, _ := m.submitHarness(); next.(model).formErr == "" {
		t.Error("negative idle timeout should error")
	}

	m2, _ := formModel(t)
	m2.formInputs[fieldName].SetValue("ok")
	m2.formInputs[fieldCommand].SetValue("x")
	m2.formInputs[fieldWaitingPattern].SetValue("(") // invalid regexp
	if next, _ := m2.submitHarness(); next.(model).formErr == "" {
		t.Error("invalid waiting pattern should error")
	}
}

// TestAddHarnessRejectsDuplicateOnDisk covers a name already present in the file
// but absent from the (stale) in-memory list: appending anyway would produce a
// duplicate [harness.<name>] table that breaks config loading.
func TestAddHarnessRejectsDuplicateOnDisk(t *testing.T) {
	m, path := formModel(t)
	os.WriteFile(path, []byte("[harness.goose]\nadapter = \"generic\"\ncommand = \"goose\"\n"), 0o600)
	// m.harnesses is the built-in list (opencode, pi) — it does NOT contain goose.
	m.formInputs[fieldName].SetValue("goose")
	m.formInputs[fieldCommand].SetValue("x")

	next, _ := m.submitHarness()
	m = next.(model)
	if m.formErr == "" || !m.addingHarness {
		t.Error("duplicate on disk should be rejected, not appended")
	}
	// The file must still load (no duplicate table appended).
	if _, err := config.Load(path); err != nil {
		t.Errorf("config corrupted by rejected add: %v", err)
	}
}

// TestAddHarnessRefusesInvalidConfig confirms an unparseable config is not
// appended to (which would compound the corruption).
func TestAddHarnessRefusesInvalidConfig(t *testing.T) {
	m, path := formModel(t)
	os.WriteFile(path, []byte("this is = not valid = toml ]["), 0o600)
	m.formInputs[fieldName].SetValue("goose")
	m.formInputs[fieldCommand].SetValue("goose")

	next, _ := m.submitHarness()
	m = next.(model)
	if m.formErr == "" || !strings.Contains(m.formErr, "invalid") {
		t.Errorf("invalid existing config should be reported, got err=%q", m.formErr)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "[harness.goose]") {
		t.Error("must not append to an unparseable config")
	}
}

// TestRestoreConfigRollsBack covers the rollback used when an appended harness
// block fails to reload: an existing file reverts to its original bytes, and a
// file that did not exist beforehand is removed.
func TestRestoreConfigRollsBack(t *testing.T) {
	dir := t.TempDir()

	path := filepath.Join(dir, "config.toml")
	orig := []byte("default_harness = \"pi\"\n")
	os.WriteFile(path, orig, 0o600)
	os.WriteFile(path, append(orig, []byte("\n[harness.bad] ][ garbage")...), 0o600)
	restoreConfig(path, orig, nil)
	if got, _ := os.ReadFile(path); string(got) != string(orig) {
		t.Errorf("restore did not revert content: %q", got)
	}

	missing := filepath.Join(dir, "new.toml")
	os.WriteFile(missing, []byte("[harness.x]\n"), 0o600) // a created-then-bad file
	restoreConfig(missing, nil, os.ErrNotExist)
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Errorf("restore should remove a file that did not exist before, stat err=%v", err)
	}
}

func TestWriteHarnessBlockAppendPreservesExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("default_harness = \"pi\"\n"), 0o600)
	if err := writeHarnessBlock(path, harnessSpec{name: "goose", command: "goose", args: []string{"bin"}}); err != nil {
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

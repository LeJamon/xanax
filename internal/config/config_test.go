package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xanax/internal/config"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadDefaultsWhenFileMissing(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DefaultHarness != "opencode" {
		t.Errorf("DefaultHarness = %q, want opencode", cfg.DefaultHarness)
	}
	if !cfg.AutoResume {
		t.Error("AutoResume = false, want true by default")
	}
	if cfg.InteractExitKey != `ctrl+\` {
		t.Errorf("InteractExitKey = %q, want ctrl+\\", cfg.InteractExitKey)
	}
	if got := cfg.Harnesses["opencode"].Adapter; got != config.AdapterOpencode {
		t.Errorf("opencode adapter = %q, want %q", got, config.AdapterOpencode)
	}
	if got := cfg.Harnesses["pi"].Adapter; got != config.AdapterPi {
		t.Errorf("pi adapter = %q, want %q", got, config.AdapterPi)
	}
}

func TestLoadMergesFileOverDefaults(t *testing.T) {
	path := writeConfig(t, `
default_harness = "pi"
auto_resume = false

[harness.opencode]
command = "/opt/opencode/bin/opencode"

[harness.goose]
command = "goose"
args = ["session"]
resume_args = ["session", "--resume"]
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DefaultHarness != "pi" {
		t.Errorf("DefaultHarness = %q, want pi", cfg.DefaultHarness)
	}
	if cfg.AutoResume {
		t.Error("AutoResume = true, want false from file")
	}
	if cfg.InteractExitKey != `ctrl+\` {
		t.Errorf("InteractExitKey = %q, want default preserved", cfg.InteractExitKey)
	}

	oc := cfg.Harnesses["opencode"]
	if oc.Adapter != config.AdapterOpencode {
		t.Errorf("partial override lost adapter: got %q", oc.Adapter)
	}
	if oc.Command != "/opt/opencode/bin/opencode" {
		t.Errorf("opencode command = %q, want file override", oc.Command)
	}

	goose := cfg.Harnesses["goose"]
	if goose.Adapter != config.AdapterGeneric {
		t.Errorf("new harness adapter = %q, want defaulted to generic", goose.Adapter)
	}
	if len(goose.Args) != 1 || goose.Args[0] != "session" {
		t.Errorf("goose args = %v", goose.Args)
	}
	if len(goose.ResumeArgs) != 2 {
		t.Errorf("goose resume_args = %v", goose.ResumeArgs)
	}
}

func TestThemeMergeOverridesOnlyProvidedFields(t *testing.T) {
	path := writeConfig(t, `
[theme]
accent = "#ff8800"
branch = "5"
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Theme.Accent != "#ff8800" {
		t.Errorf("accent = %q, want overridden hex", cfg.Theme.Accent)
	}
	if cfg.Theme.Branch != "5" {
		t.Errorf("branch = %q, want overridden", cfg.Theme.Branch)
	}
	// Unspecified fields keep the defaults.
	def := config.DefaultTheme()
	if cfg.Theme.Waiting != def.Waiting || cfg.Theme.Failed != def.Failed {
		t.Errorf("unspecified theme fields were not defaulted: %+v", cfg.Theme)
	}
}

func TestDefaultThemeFullyPopulated(t *testing.T) {
	th := config.DefaultTheme()
	if th.Accent == "" || th.Muted == "" || th.Text == "" || th.PR == "" ||
		th.Waiting == "" || th.Running == "" || th.Completed == "" ||
		th.Failed == "" || th.Cancelled == "" || th.Branch == "" {
		t.Errorf("DefaultTheme has empty fields: %+v", th)
	}
}

func TestLoadDefaultsCommandToHarnessName(t *testing.T) {
	path := writeConfig(t, "[harness.mytool]\nadapter = \"generic\"\n")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Harnesses["mytool"].Command; got != "mytool" {
		t.Errorf("command = %q, want harness name", got)
	}
}

func TestLoadRejectsUnknownAdapter(t *testing.T) {
	path := writeConfig(t, "[harness.foo]\nadapter = \"bogus\"\n")
	_, err := config.Load(path)
	if err == nil || !strings.Contains(err.Error(), "adapter") {
		t.Fatalf("want unknown-adapter error, got %v", err)
	}
}

func TestLoadRejectsUnknownDefaultHarness(t *testing.T) {
	path := writeConfig(t, "default_harness = \"nope\"\n")
	_, err := config.Load(path)
	if err == nil || !strings.Contains(err.Error(), "default_harness") {
		t.Fatalf("want unknown default_harness error, got %v", err)
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	path := writeConfig(t, "detachkey = \"typo\"\n")
	_, err := config.Load(path)
	if err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Fatalf("want unknown-key error, got %v", err)
	}
}

func TestLoadRejectsInvalidThemeColor(t *testing.T) {
	for _, bad := range []string{"256", "300", "-1", "gren", "13.5", "0x0d",
		"#zzzzzz", "#ff88", "#ff8800aa", "13 ", " 13"} {
		path := writeConfig(t, "[theme]\naccent = \""+bad+"\"\n")
		_, err := config.Load(path)
		if err == nil || !strings.Contains(err.Error(), "theme accent") {
			t.Errorf("accent=%q: want theme-color error, got %v", bad, err)
		}
	}
}

func TestLoadAcceptsValidThemeColors(t *testing.T) {
	path := writeConfig(t, `
[theme]
accent    = "0"
waiting   = "255"
running   = "#ff8800"
completed = "#FFAA00"
failed    = "#f80"
cancelled = "#FFF"
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Theme.Accent != "0" || cfg.Theme.Running != "#ff8800" || cfg.Theme.Failed != "#f80" {
		t.Errorf("valid theme colors not applied: %+v", cfg.Theme)
	}
}

// The built-in defaults must themselves pass validation. Load merges DefaultTheme
// under any present config, so an empty file routes every default field through
// validate — this fails loudly if a default is ever set to an invalid color.
func TestDefaultThemePassesValidation(t *testing.T) {
	path := writeConfig(t, "auto_resume = true\n")
	if _, err := config.Load(path); err != nil {
		t.Fatalf("default theme failed validation: %v", err)
	}
}

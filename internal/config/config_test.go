package config_test

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"xanax/internal/config"
	"xanax/internal/session"
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
	codex := cfg.Harnesses["codex"]
	if codex.Adapter != config.AdapterGeneric || codex.Command != "codex" {
		t.Errorf("codex default = %+v, want generic codex", codex)
	}
	if !codex.PromptPositional {
		t.Error("codex default prompt_positional = false, want true")
	}
	if !slices.Equal(codex.ResumeArgs, []string{"resume", "--last"}) {
		t.Errorf("codex default resume_args = %v, want [resume --last]", codex.ResumeArgs)
	}
	if codex.IdleTimeout != 120 {
		t.Errorf("codex default idle_timeout = %d, want 120", codex.IdleTimeout)
	}
	if !codex.FullScreen {
		t.Error("codex default full_screen = false, want true")
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

[harness.work-codex]
command = "/opt/codex/bin/codex"
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

	workCodex := cfg.Harnesses["work-codex"]
	if workCodex.Adapter != config.AdapterGeneric || !workCodex.FullScreen {
		t.Errorf("codex command alias = %+v, want generic full_screen harness", workCodex)
	}
}

func TestLoadMergesPartialCodexConfigOverDefault(t *testing.T) {
	path := writeConfig(t, `
default_harness = "codex"

[harness.codex]
adapter = "generic"
command = "codex"
args = ["-a", "never", "-s", "workspace-write"]
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	codex := cfg.Harnesses["codex"]
	if cfg.DefaultHarness != "codex" {
		t.Errorf("DefaultHarness = %q, want codex", cfg.DefaultHarness)
	}
	if !slices.Equal(codex.Args, []string{"-a", "never", "-s", "workspace-write"}) {
		t.Errorf("codex args = %v, want local override", codex.Args)
	}
	if !codex.PromptPositional {
		t.Error("partial codex override dropped prompt_positional")
	}
	if !slices.Equal(codex.ResumeArgs, []string{"resume", "--last"}) {
		t.Errorf("partial codex override dropped resume_args: %v", codex.ResumeArgs)
	}
	if codex.IdleTimeout != 120 {
		t.Errorf("partial codex override dropped idle_timeout: %d", codex.IdleTimeout)
	}
	if !codex.FullScreen {
		t.Error("partial codex override dropped full_screen")
	}
}

func TestLoadCodexDefaultsCanBeExplicitlyDisabled(t *testing.T) {
	path := writeConfig(t, `
[harness.codex]
prompt_positional = false
full_screen = false
idle_timeout = 0

[harness.work-codex]
command = "codex"
full_screen = false
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	codex := cfg.Harnesses["codex"]
	if codex.PromptPositional {
		t.Error("codex prompt_positional = true, want explicit false override")
	}
	if codex.IdleTimeout != 0 {
		t.Errorf("codex idle_timeout = %d, want explicit zero override", codex.IdleTimeout)
	}
	if codex.FullScreen {
		t.Error("codex full_screen = true, want explicit false override")
	}
	if codex.Adapter != config.AdapterGeneric || codex.Command != "codex" {
		t.Errorf("codex default identity = %+v, want generic codex", codex)
	}
	if !slices.Equal(codex.ResumeArgs, []string{"resume", "--last"}) {
		t.Errorf("codex resume_args = %v, want default resume args preserved", codex.ResumeArgs)
	}
	if cfg.Harnesses["work-codex"].FullScreen {
		t.Error("codex command alias full_screen = true, want explicit false override")
	}
}

// TestLoadCodexGenericExample loads the documented codex block (README/SPEC)
// and asserts it decodes as intended.
func TestLoadCodexGenericExample(t *testing.T) {
	path := writeConfig(t, `
[harness.codex]
adapter           = "generic"
command           = "codex"
full_screen       = true
prompt_positional = true
resume_args       = ["resume", "--last"]
idle_timeout      = 120
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	codex := cfg.Harnesses["codex"]
	if codex.Adapter != config.AdapterGeneric {
		t.Errorf("codex adapter = %q, want %q", codex.Adapter, config.AdapterGeneric)
	}
	if codex.Command != "codex" {
		t.Errorf("codex command = %q, want codex", codex.Command)
	}
	if !codex.PromptPositional {
		t.Error("codex prompt_positional = false, want true")
	}
	if len(codex.ResumeArgs) != 2 || codex.ResumeArgs[0] != "resume" || codex.ResumeArgs[1] != "--last" {
		t.Errorf("codex resume_args = %v, want [resume --last]", codex.ResumeArgs)
	}
	if codex.IdleTimeout != 120 {
		t.Errorf("codex idle_timeout = %d, want 120", codex.IdleTimeout)
	}
	if !codex.FullScreen {
		t.Error("codex full_screen = false, want true")
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

func TestLoadAcceptsValidInteractExitKey(t *testing.T) {
	path := writeConfig(t, "interact_exit_key = \"ctrl+]\"\n")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.InteractExitKey != "ctrl+]" {
		t.Fatalf("InteractExitKey = %q, want ctrl+]", cfg.InteractExitKey)
	}
}

func TestLoadRejectsInvalidInteractExitKey(t *testing.T) {
	for _, c := range []struct {
		spec string
		want string
	}{
		{"ctrl+space", "interact_exit_key"},
		{"F12", "interact_exit_key"},
		{"super+F12", "interact_exit_key"},
		{"ctrl+m", "Enter"},
		{"ctrl+j", "Enter"},
		{"ctrl+i", "Tab"},
		{"ctrl+h", "Backspace"},
	} {
		t.Run(c.spec, func(t *testing.T) {
			path := writeConfig(t, "interact_exit_key = "+strconv.Quote(c.spec)+"\n")
			_, err := config.Load(path)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("interact_exit_key=%q: want error containing %q, got %v", c.spec, c.want, err)
			}
		})
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

// TestLoadDefaultKeysWhenFileMissing checks the built-in key map is present with
// no config file, so the dashboard always resolves bindings.
func TestLoadDefaultKeysWhenFileMissing(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !slices.Equal(cfg.Keys.Up, config.Binding{"up", "k"}) {
		t.Errorf("default up keys = %v, want [up k]", cfg.Keys.Up)
	}
	if !slices.Equal(cfg.Keys.Down, config.Binding{"down", "j"}) {
		t.Errorf("default down keys = %v, want [down j]", cfg.Keys.Down)
	}
	if !slices.Equal(cfg.Keys.Remove, config.Binding{"ctrl+x"}) {
		t.Errorf("default remove keys = %v, want [ctrl+x]", cfg.Keys.Remove)
	}
	if !slices.Equal(cfg.Keys.Quit, config.Binding{"ctrl+c"}) {
		t.Errorf("default quit keys = %v, want [ctrl+c]", cfg.Keys.Quit)
	}
	if !slices.Equal(cfg.Keys.Preview, config.Binding{"space"}) {
		t.Errorf("default preview keys = %v, want [space]", cfg.Keys.Preview)
	}
	if !slices.Equal(cfg.Keys.Settings, config.Binding{"s"}) {
		t.Errorf("default settings keys = %v, want [s]", cfg.Keys.Settings)
	}
}

// TestKeyMapActionsCoverEveryBinding guards that Actions() enumerates the whole
// KeyMap — validate and the in-TUI editor both rely on it, so a new field that
// is not added here would silently escape both.
func TestKeyMapActionsCoverEveryBinding(t *testing.T) {
	actions := config.DefaultKeys().Actions()
	names := make(map[string]bool, len(actions))
	for _, a := range actions {
		if a.Name == "" || a.Desc == "" {
			t.Errorf("action has an empty name or description: %+v", a)
		}
		names[a.Name] = true
	}
	for _, want := range []string{"settings", "remove", "confirm", "cancel", "quit", "form_prev"} {
		if !names[want] {
			t.Errorf("Actions() is missing the %q action", want)
		}
	}
}

// TestLoadMergesKeysFieldWise verifies a [keys] table overrides only the actions
// it names; every other action keeps its built-in binding.
func TestLoadMergesKeysFieldWise(t *testing.T) {
	path := writeConfig(t, `
[keys]
remove = ["x"]
filter = ["ctrl+f", "/"]
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !slices.Equal(cfg.Keys.Remove, config.Binding{"x"}) {
		t.Errorf("remove = %v, want [x] from file", cfg.Keys.Remove)
	}
	if !slices.Equal(cfg.Keys.Filter, config.Binding{"ctrl+f", "/"}) {
		t.Errorf("filter = %v, want [ctrl+f /] from file", cfg.Keys.Filter)
	}
	// Unspecified actions keep the defaults.
	def := config.DefaultKeys()
	if !slices.Equal(cfg.Keys.Open, def.Open) || !slices.Equal(cfg.Keys.Quit, def.Quit) {
		t.Errorf("unspecified key actions were not defaulted: %+v", cfg.Keys)
	}
}

// TestLoadKeysUnbind confirms an explicit empty array unbinds an action (a
// non-nil empty binding), distinct from omitting it (which keeps the default).
func TestLoadKeysUnbind(t *testing.T) {
	path := writeConfig(t, "[keys]\nquit_list = []\n")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Keys.QuitList == nil || len(cfg.Keys.QuitList) != 0 {
		t.Errorf("quit_list = %v, want an empty (unbound) binding", cfg.Keys.QuitList)
	}
	// A sibling action left unset still carries its default.
	if !slices.Equal(cfg.Keys.Filter, config.DefaultKeys().Filter) {
		t.Errorf("unset action lost its default: filter = %v", cfg.Keys.Filter)
	}
}

func TestLoadRejectsUnknownKeyAction(t *testing.T) {
	path := writeConfig(t, "[keys]\nremvoe = [\"x\"]\n") // typo'd action name
	_, err := config.Load(path)
	if err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Fatalf("want unknown-key error for a bogus [keys] action, got %v", err)
	}
}

func TestLoadRejectsEmptyKeyBinding(t *testing.T) {
	path := writeConfig(t, "[keys]\nremove = [\"\"]\n")
	_, err := config.Load(path)
	if err == nil || !strings.Contains(err.Error(), "keys remove") {
		t.Fatalf("want empty-key-binding error, got %v", err)
	}
}

// CanResume is the shared gate for `xanax resume` and the dashboard's open
// action. The "generic with resume_args" case is the regression that mattered:
// generic harnesses never capture a session ref, so a completed session has an
// empty ref yet must still be openable via its configured resume_args.
func TestCanResume(t *testing.T) {
	cfg := &config.Config{Harnesses: map[string]config.Harness{
		"opencode": {Adapter: config.AdapterOpencode}, // native, no resume_args
		"codex":    {Adapter: config.AdapterGeneric, ResumeArgs: []string{"resume", "--last"}},
		"aider":    {Adapter: config.AdapterGeneric}, // generic, nothing
	}}

	cases := []struct {
		name string
		sess *session.Session
		want bool
	}{
		{"captured ref always resumable", &session.Session{Harness: "opencode", HarnessSessionRef: "ses_1"}, true},
		{"completed generic with resume_args, no ref", &session.Session{Harness: "codex", Status: session.StatusCompleted}, true},
		{"generic without resume_args", &session.Session{Harness: "aider"}, false},
		{"native without ref", &session.Session{Harness: "opencode"}, false},
		{"unknown harness", &session.Session{Harness: "gone"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cfg.CanResume(c.sess); got != c.want {
				t.Errorf("CanResume = %v, want %v", got, c.want)
			}
		})
	}
}

// Package config resolves xanax configuration: built-in harness defaults
// overlaid with the optional ~/.config/xanax/config.toml (SPEC.md §8).
package config

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"xanax/internal/attach"
	"xanax/internal/session"
)

// Adapter names accepted in harness configuration.
const (
	AdapterOpencode = "opencode"
	AdapterPi       = "pi"
	AdapterGeneric  = "generic"
)

// Harness describes how to launch one agent CLI.
type Harness struct {
	Adapter    string            `toml:"adapter"`
	Command    string            `toml:"command"`
	Args       []string          `toml:"args,omitempty"`
	ResumeArgs []string          `toml:"resume_args,omitempty"`
	Env        map[string]string `toml:"env,omitempty"`

	// FullScreen forces attach replay through the rendered screen snapshot even
	// when the harness is not currently in the terminal alternate screen. Use it
	// for diff-rendered TUIs whose raw scrollback cannot reconstruct the chat.
	FullScreen bool `toml:"full_screen,omitempty"`

	// Prompt delivery for the generic adapter. PromptArg passes the initial
	// prompt as a flag value (command … <prompt_arg> "<prompt>"). PromptPositional
	// appends it as the last argument (command … "<prompt>"). With neither set
	// the generic adapter falls back to typing the prompt into the PTY. Ignored
	// by the native pi/opencode adapters.
	PromptArg        string `toml:"prompt_arg,omitempty"`
	PromptPositional bool   `toml:"prompt_positional,omitempty"`

	// Approximate state detection for the generic adapter (no native state
	// channel). IdleTimeout marks the session "waiting" after that many seconds
	// with no output; WaitingPattern is a regexp matched against output that
	// marks it "waiting" immediately (e.g. a "(y/n)" prompt). Either resets to
	// "running" when output resumes. Ignored by native adapters.
	IdleTimeout    int    `toml:"idle_timeout,omitempty"`
	WaitingPattern string `toml:"waiting_pattern,omitempty"`
}

// Config is the fully resolved configuration.
type Config struct {
	DefaultHarness string `toml:"default_harness"`
	// InteractExitKey steps out of a session's raw passthrough back to navigate
	// mode (SPEC.md §10). Arrow keys and Escape are reserved by the harness TUIs.
	InteractExitKey string             `toml:"interact_exit_key"`
	AutoResume      bool               `toml:"auto_resume"`
	Notifications   bool               `toml:"notifications"`
	Theme           Theme              `toml:"theme"`
	Keys            KeyMap             `toml:"keys"`
	Harnesses       map[string]Harness `toml:"harness"`
}

// CanResume reports whether a dead session can be relaunched: either a
// harness-native session ref was captured (pi/opencode), or its harness has
// resume_args configured (generic "continue last session in this repo"). This
// is the single source of truth for both the `xanax resume` CLI path and the
// dashboard's open-session action, so a terminal session (e.g. completed) that
// resume can revive is never wrongly reported as unopenable.
func (c *Config) CanResume(sess *session.Session) bool {
	if sess.HarnessSessionRef != "" {
		return true
	}
	h, ok := c.Harnesses[sess.Harness]
	return ok && len(h.ResumeArgs) > 0
}

// Theme colors the dashboard TUI. Each value is an ANSI palette index ("0"–
// "255") or a hex color ("#rgb" or "#rrggbb"). Empty fields fall back to the
// defaults.
type Theme struct {
	Accent    string `toml:"accent"`    // selection / cursor
	Waiting   string `toml:"waiting"`   // needs-input state
	Running   string `toml:"running"`   // running state
	Completed string `toml:"completed"` // completed state
	Failed    string `toml:"failed"`    // failed state
	Cancelled string `toml:"cancelled"` // cancelled state
	Muted     string `toml:"muted"`     // secondary text / rules
	Text      string `toml:"text"`      // prompt input text
	Branch    string `toml:"branch"`    // git branch
	PR        string `toml:"pr"`        // PR number
}

// Binding is the set of keys that trigger one dashboard action. Each entry is a
// key name as bubbletea reports it (tea.KeyMsg.String()): a rune ("o", "/",
// "+"), a named key ("enter", "up", "tab", "shift+tab"), a control chord
// ("ctrl+c", "ctrl+o"), or "space" for the spacebar. Any key in the list fires
// the action; an empty list leaves it unbound.
type Binding []string

// KeyAction pairs a bindable action's config name and one-line description with
// its current keys. KeyMap.Actions returns them in a stable order for the in-TUI
// keybindings editor and for validation.
type KeyAction struct {
	Name string
	Desc string
	Keys Binding
}

// KeyMap binds the dashboard's actions to keys (SPEC.md §8/§10). Every key the
// dashboard handles resolves through it, so the config file can remap which key
// does what. The actions shared across every modal editor — Confirm (the primary
// action) and Cancel (back / dismiss) — are bound once here and reused, so a
// remap applies consistently. The interact-mode detach key is separate
// (interact_exit_key): it lives in the raw passthrough, not the dashboard.
type KeyMap struct {
	// Navigation and global — active in every mode.
	Up      Binding `toml:"up"`      // move the selection up
	Down    Binding `toml:"down"`    // move the selection down
	Confirm Binding `toml:"confirm"` // enter: launch / open / apply / save / select
	Cancel  Binding `toml:"cancel"`  // esc: back / cancel / clear an applied filter
	Quit    Binding `toml:"quit"`    // two-step confirm-then-exit, from any mode

	// Session list — active while a session row is selected.
	Open     Binding `toml:"open"`      // open the live session window
	Remove   Binding `toml:"remove"`    // remove the session; live sessions confirm first
	Resume   Binding `toml:"resume"`    // resume the session
	Rename   Binding `toml:"rename"`    // rename its xanax label
	Preview  Binding `toml:"preview"`   // toggle the screen peek
	Filter   Binding `toml:"filter"`    // open the filter bar
	Settings Binding `toml:"settings"`  // open the in-TUI keybindings editor
	QuitList Binding `toml:"quit_list"` // quit straight from the list

	// Prompt box (composer).
	LaunchAttach  Binding `toml:"launch_attach"`  // launch a session and attach to it
	HarnessPicker Binding `toml:"harness_picker"` // open the harness picker

	// Harness picker.
	AddHarness    Binding `toml:"add_harness"`    // open the add-harness form
	SetDefault    Binding `toml:"set_default"`    // set the highlighted harness as default
	ModifyHarness Binding `toml:"modify_harness"` // edit the highlighted harness
	ToggleSearch  Binding `toml:"toggle_search"`  // toggle search box / action row focus

	// Harness add/modify form.
	FormNext Binding `toml:"form_next"` // next field
	FormPrev Binding `toml:"form_prev"` // previous field
}

// Paths locates everything xanax reads or writes on disk (SPEC.md §7).
type Paths struct {
	ConfigFile string
	DataDir    string
	DBFile     string
	LogsDir    string
	SocketDir  string
}

// DefaultPaths resolves XDG-style locations on macOS and Linux. Sockets live
// under /tmp to stay within the 104-byte sun_path limit on macOS.
func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve home directory: %w", err)
	}
	cfgDir := os.Getenv("XDG_CONFIG_HOME")
	if cfgDir == "" {
		cfgDir = filepath.Join(home, ".config")
	}
	dataDir := os.Getenv("XDG_DATA_HOME")
	if dataDir == "" {
		dataDir = filepath.Join(home, ".local", "share")
	}
	data := filepath.Join(dataDir, "xanax")
	return Paths{
		ConfigFile: filepath.Join(cfgDir, "xanax", "config.toml"),
		DataDir:    data,
		DBFile:     filepath.Join(data, "xanax.db"),
		LogsDir:    filepath.Join(data, "logs"),
		SocketDir:  filepath.Join("/tmp", fmt.Sprintf("xanax-%d", os.Getuid())),
	}, nil
}

// Default returns the built-in harness configuration. opencode and pi use
// native adapters; codex uses the generic adapter with command-line prompt
// delivery because typing into its full-screen TUI races startup.
func Default() *Config {
	return &Config{
		DefaultHarness:  "opencode",
		InteractExitKey: `ctrl+\`,
		AutoResume:      true,
		Notifications:   true,
		Theme:           DefaultTheme(),
		Keys:            DefaultKeys(),
		Harnesses: map[string]Harness{
			"opencode": {Adapter: AdapterOpencode, Command: "opencode"},
			"pi":       {Adapter: AdapterPi, Command: "pi"},
			"codex": {
				Adapter:          AdapterGeneric,
				Command:          "codex",
				ResumeArgs:       []string{"resume", "--last"},
				FullScreen:       true,
				PromptPositional: true,
				IdleTimeout:      120,
			},
		},
	}
}

// DefaultTheme is the built-in color scheme (ANSI palette indices).
func DefaultTheme() Theme {
	return Theme{
		Accent:    "13",  // magenta
		Waiting:   "11",  // yellow
		Running:   "12",  // blue
		Completed: "10",  // green
		Failed:    "9",   // red
		Cancelled: "8",   // grey
		Muted:     "250", // light grey
		Text:      "15",  // white
		Branch:    "6",   // cyan
		PR:        "10",  // green
	}
}

// DefaultKeys is the built-in key map: the bindings the dashboard shipped with
// before keys were configurable. A config file's [keys] table overlays it
// field-wise (mergeKeys), so an override changes only the actions it names.
func DefaultKeys() KeyMap {
	return KeyMap{
		Up:      Binding{"up", "k"},
		Down:    Binding{"down", "j"},
		Confirm: Binding{"enter"},
		Cancel:  Binding{"esc"},
		Quit:    Binding{"ctrl+c"},

		Open:     Binding{"enter", "right", "o"},
		Remove:   Binding{"ctrl+x"},
		Resume:   Binding{"r", "ctrl+r"},
		Rename:   Binding{"e"},
		Preview:  Binding{"space"},
		Filter:   Binding{"/"},
		Settings: Binding{"s"},
		QuitList: Binding{"q"},

		LaunchAttach:  Binding{"ctrl+o", "alt+enter"},
		HarnessPicker: Binding{"tab"},

		AddHarness:    Binding{"+"},
		SetDefault:    Binding{"d"},
		ModifyHarness: Binding{"m"},
		ToggleSearch:  Binding{"tab"},

		FormNext: Binding{"tab", "down"},
		FormPrev: Binding{"shift+tab", "up"},
	}
}

// Actions lists every bindable action with its current keys, in the stable
// order the in-TUI keybindings editor displays them. Adding a KeyMap field means
// adding it here too — validate and the editor both enumerate through this.
func (k KeyMap) Actions() []KeyAction {
	return []KeyAction{
		{"up", "move the selection up", k.Up},
		{"down", "move the selection down", k.Down},
		{"confirm", "primary action: launch / open / apply / save / select", k.Confirm},
		{"cancel", "back, cancel, or clear an applied filter", k.Cancel},
		{"quit", "confirm-then-exit, from any mode", k.Quit},
		{"open", "open the selected session's window", k.Open},
		{"remove", "remove the session; confirms before killing live sessions", k.Remove},
		{"resume", "resume the selected session", k.Resume},
		{"rename", "rename the session's xanax label", k.Rename},
		{"preview", "toggle the screen peek", k.Preview},
		{"filter", "filter the session list", k.Filter},
		{"settings", "open this keybindings editor", k.Settings},
		{"quit_list", "quit straight from the session list", k.QuitList},
		{"launch_attach", "launch a new session and attach to it", k.LaunchAttach},
		{"harness_picker", "open the harness picker", k.HarnessPicker},
		{"add_harness", "open the add-harness form", k.AddHarness},
		{"set_default", "set the highlighted harness as default", k.SetDefault},
		{"modify_harness", "edit the highlighted harness", k.ModifyHarness},
		{"toggle_search", "toggle the picker's search / action row", k.ToggleSearch},
		{"form_next", "next form field", k.FormNext},
		{"form_prev", "previous form field", k.FormPrev},
	}
}

// mergeKeys overlays the bindings over provides onto base. A nil binding means
// the file did not set that action (keep the default); a present binding —
// including an empty list, which unbinds the action — replaces it wholesale.
func mergeKeys(base, over KeyMap) KeyMap {
	pick := func(b, o Binding) Binding {
		if o != nil {
			return o
		}
		return b
	}
	return KeyMap{
		Up:            pick(base.Up, over.Up),
		Down:          pick(base.Down, over.Down),
		Confirm:       pick(base.Confirm, over.Confirm),
		Cancel:        pick(base.Cancel, over.Cancel),
		Quit:          pick(base.Quit, over.Quit),
		Open:          pick(base.Open, over.Open),
		Remove:        pick(base.Remove, over.Remove),
		Resume:        pick(base.Resume, over.Resume),
		Rename:        pick(base.Rename, over.Rename),
		Preview:       pick(base.Preview, over.Preview),
		Filter:        pick(base.Filter, over.Filter),
		Settings:      pick(base.Settings, over.Settings),
		QuitList:      pick(base.QuitList, over.QuitList),
		LaunchAttach:  pick(base.LaunchAttach, over.LaunchAttach),
		HarnessPicker: pick(base.HarnessPicker, over.HarnessPicker),
		AddHarness:    pick(base.AddHarness, over.AddHarness),
		SetDefault:    pick(base.SetDefault, over.SetDefault),
		ModifyHarness: pick(base.ModifyHarness, over.ModifyHarness),
		ToggleSearch:  pick(base.ToggleSearch, over.ToggleSearch),
		FormNext:      pick(base.FormNext, over.FormNext),
		FormPrev:      pick(base.FormPrev, over.FormPrev),
	}
}

// mergeTheme overlays the non-empty fields of over onto base.
func mergeTheme(base, over Theme) Theme {
	pick := func(b, o string) string {
		if o != "" {
			return o
		}
		return b
	}
	return Theme{
		Accent:    pick(base.Accent, over.Accent),
		Waiting:   pick(base.Waiting, over.Waiting),
		Running:   pick(base.Running, over.Running),
		Completed: pick(base.Completed, over.Completed),
		Failed:    pick(base.Failed, over.Failed),
		Cancelled: pick(base.Cancelled, over.Cancelled),
		Muted:     pick(base.Muted, over.Muted),
		Text:      pick(base.Text, over.Text),
		Branch:    pick(base.Branch, over.Branch),
		PR:        pick(base.PR, over.PR),
	}
}

// fileConfig uses pointers so absent keys are distinguishable from zero
// values when merging over defaults.
type fileConfig struct {
	DefaultHarness  *string                `toml:"default_harness"`
	InteractExitKey *string                `toml:"interact_exit_key"`
	AutoResume      *bool                  `toml:"auto_resume"`
	Notifications   *bool                  `toml:"notifications"`
	Theme           Theme                  `toml:"theme"`
	Keys            KeyMap                 `toml:"keys"`
	Harness         map[string]fileHarness `toml:"harness"`
}

type fileHarness struct {
	Adapter    string            `toml:"adapter"`
	Command    string            `toml:"command"`
	Args       []string          `toml:"args,omitempty"`
	ResumeArgs []string          `toml:"resume_args,omitempty"`
	Env        map[string]string `toml:"env,omitempty"`
	FullScreen *bool             `toml:"full_screen,omitempty"`

	PromptArg        string `toml:"prompt_arg,omitempty"`
	PromptPositional *bool  `toml:"prompt_positional,omitempty"`

	IdleTimeout    *int   `toml:"idle_timeout,omitempty"`
	WaitingPattern string `toml:"waiting_pattern,omitempty"`
}

// Load reads the config file at path (if it exists) and merges it over the
// built-in defaults. Harness entries merge field-wise, so a user can override
// just the command of a built-in harness. Unknown keys and unknown adapters
// are errors.
func Load(path string) (*Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var fc fileConfig
	md, err := toml.Decode(string(data), &fc)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return nil, fmt.Errorf("%s: unknown key %q", path, undecoded[0].String())
	}

	if fc.DefaultHarness != nil {
		cfg.DefaultHarness = *fc.DefaultHarness
	}
	if fc.InteractExitKey != nil {
		cfg.InteractExitKey = *fc.InteractExitKey
	}
	if fc.AutoResume != nil {
		cfg.AutoResume = *fc.AutoResume
	}
	if fc.Notifications != nil {
		cfg.Notifications = *fc.Notifications
	}
	cfg.Theme = mergeTheme(cfg.Theme, fc.Theme)
	cfg.Keys = mergeKeys(cfg.Keys, fc.Keys)
	for name, fh := range fc.Harness {
		merged := cfg.Harnesses[name]
		if fh.Adapter != "" {
			merged.Adapter = fh.Adapter
		}
		if fh.Command != "" {
			merged.Command = fh.Command
		}
		if fh.Args != nil {
			merged.Args = fh.Args
		}
		if fh.ResumeArgs != nil {
			merged.ResumeArgs = fh.ResumeArgs
		}
		if fh.FullScreen != nil {
			merged.FullScreen = *fh.FullScreen
		}
		if fh.PromptArg != "" {
			merged.PromptArg = fh.PromptArg
		}
		if fh.PromptPositional != nil {
			merged.PromptPositional = *fh.PromptPositional
		}
		if fh.IdleTimeout != nil {
			merged.IdleTimeout = *fh.IdleTimeout
		}
		if fh.WaitingPattern != "" {
			merged.WaitingPattern = fh.WaitingPattern
		}
		if len(fh.Env) > 0 {
			if merged.Env == nil {
				merged.Env = make(map[string]string, len(fh.Env))
			}
			maps.Copy(merged.Env, fh.Env)
		}
		if merged.Adapter == "" {
			merged.Adapter = AdapterGeneric
		}
		if merged.Command == "" {
			merged.Command = name
		}
		if fh.FullScreen == nil && merged.Adapter == AdapterGeneric && filepath.Base(merged.Command) == "codex" {
			merged.FullScreen = true
		}
		cfg.Harnesses[name] = merged
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func (c *Config) validate() error {
	if _, ok := c.Harnesses[c.DefaultHarness]; !ok {
		return fmt.Errorf("default_harness %q is not a configured harness", c.DefaultHarness)
	}
	if _, err := attach.ParseExitKey(c.InteractExitKey); err != nil {
		return err
	}
	for name, h := range c.Harnesses {
		switch h.Adapter {
		case AdapterOpencode, AdapterPi, AdapterGeneric:
		default:
			return fmt.Errorf("harness %q: unknown adapter %q (want %q, %q, or %q)",
				name, h.Adapter, AdapterOpencode, AdapterPi, AdapterGeneric)
		}
		if h.IdleTimeout < 0 {
			return fmt.Errorf("harness %q: idle_timeout must be >= 0, got %d", name, h.IdleTimeout)
		}
	}
	if err := c.Theme.validate(); err != nil {
		return err
	}
	if err := c.Keys.validate(); err != nil {
		return err
	}
	return nil
}

// validate rejects a key binding that contains an empty (or whitespace-only)
// key — almost always a typo (e.g. a stray comma). Load merges defaults in
// first, so an omitted action is never empty here; only an explicit bad entry
// trips this. Unknown key *names* (e.g. "supr" for a nonexistent key) cannot be
// validated exhaustively — they simply never fire.
func (k KeyMap) validate() error {
	for _, a := range k.Actions() {
		for _, key := range a.Keys {
			if strings.TrimSpace(key) == "" {
				return fmt.Errorf("keys %s: contains an empty key binding", a.Name)
			}
		}
	}
	return nil
}

// validate rejects theme colors that are neither an ANSI palette index nor a
// hex color. Load merges defaults in first, so every field is populated here.
func (t Theme) validate() error {
	for _, f := range []struct{ name, val string }{
		{"accent", t.Accent},
		{"waiting", t.Waiting},
		{"running", t.Running},
		{"completed", t.Completed},
		{"failed", t.Failed},
		{"cancelled", t.Cancelled},
		{"muted", t.Muted},
		{"text", t.Text},
		{"branch", t.Branch},
		{"pr", t.PR},
	} {
		if !validColor(f.val) {
			return fmt.Errorf("theme %s: invalid color %q (want ANSI index \"0\"–\"255\" or hex \"#rgb\"/\"#rrggbb\")",
				f.name, f.val)
		}
	}
	return nil
}

// validColor reports whether s is one of the forms lipgloss.Color renders: an
// ANSI palette index ("0"–"255"), or a hex color in short "#rgb" or long
// "#rrggbb" form (both accepted by go-colorful's Hex, which the renderer uses).
func validColor(s string) bool {
	if strings.HasPrefix(s, "#") {
		if len(s) != 4 && len(s) != 7 {
			return false
		}
		for _, c := range s[1:] {
			switch {
			case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
			default:
				return false
			}
		}
		return true
	}
	n, err := strconv.Atoi(s)
	return err == nil && n >= 0 && n <= 255
}

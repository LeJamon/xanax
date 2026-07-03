// Package config resolves xanax configuration: built-in harness defaults
// overlaid with the optional ~/.config/xanax/config.toml (SPEC.md §8).
package config

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
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
}

// Config is the fully resolved configuration.
type Config struct {
	DefaultHarness string `toml:"default_harness"`
	// InteractExitKey steps out of a session's raw passthrough back to navigate
	// mode (SPEC.md §10). Arrow keys and Escape are reserved by the harness TUIs.
	InteractExitKey string             `toml:"interact_exit_key"`
	AutoResume      bool               `toml:"auto_resume"`
	Notifications   bool               `toml:"notifications"`
	Harnesses       map[string]Harness `toml:"harness"`
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

// Default returns the built-in configuration: opencode and pi with their
// native adapters, no config file required.
func Default() *Config {
	return &Config{
		DefaultHarness:  "opencode",
		InteractExitKey: `ctrl+\`,
		AutoResume:      true,
		Notifications:   true,
		Harnesses: map[string]Harness{
			"opencode": {Adapter: AdapterOpencode, Command: "opencode"},
			"pi":       {Adapter: AdapterPi, Command: "pi"},
		},
	}
}

// fileConfig uses pointers so absent keys are distinguishable from zero
// values when merging over defaults.
type fileConfig struct {
	DefaultHarness  *string            `toml:"default_harness"`
	InteractExitKey *string            `toml:"interact_exit_key"`
	AutoResume      *bool              `toml:"auto_resume"`
	Notifications   *bool              `toml:"notifications"`
	Harness         map[string]Harness `toml:"harness"`
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
	for name, h := range c.Harnesses {
		switch h.Adapter {
		case AdapterOpencode, AdapterPi, AdapterGeneric:
		default:
			return fmt.Errorf("harness %q: unknown adapter %q (want %q, %q, or %q)",
				name, h.Adapter, AdapterOpencode, AdapterPi, AdapterGeneric)
		}
	}
	return nil
}

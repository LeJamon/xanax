package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/LeJamon/rvr/internal/config"
)

// TestSetKeyBindingInConfig covers the config writer: appending a [keys] table
// without disturbing existing content, replacing an action's line in place, and
// keeping prefix-colliding names (quit vs quit_list) independent.
func TestSetKeyBindingInConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("default_harness = \"pi\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Append a [keys] table, preserving the existing key.
	if err := setKeyBindingInConfig(path, "remove", []string{"x"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load after append: %v", err)
	}
	if cfg.DefaultHarness != "pi" {
		t.Errorf("append clobbered existing content: default_harness = %q", cfg.DefaultHarness)
	}
	if !slices.Equal(cfg.Keys.Remove, config.Binding{"x"}) {
		t.Errorf("remove = %v, want [x]", cfg.Keys.Remove)
	}

	// Replace the action's line in place with a multi-key binding.
	if err := setKeyBindingInConfig(path, "remove", []string{"z", "ctrl+k"}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	cfg, _ = config.Load(path)
	if !slices.Equal(cfg.Keys.Remove, config.Binding{"z", "ctrl+k"}) {
		t.Errorf("remove after replace = %v, want [z ctrl+k]", cfg.Keys.Remove)
	}

	// quit and quit_list share a prefix; writing one must not touch the other.
	if err := setKeyBindingInConfig(path, "quit", []string{"ctrl+c"}); err != nil {
		t.Fatal(err)
	}
	if err := setKeyBindingInConfig(path, "quit_list", []string{"Q"}); err != nil {
		t.Fatal(err)
	}
	cfg, _ = config.Load(path)
	if !slices.Equal(cfg.Keys.Quit, config.Binding{"ctrl+c"}) {
		t.Errorf("quit = %v, want [ctrl+c] (quit_list must not overwrite it)", cfg.Keys.Quit)
	}
	if !slices.Equal(cfg.Keys.QuitList, config.Binding{"Q"}) {
		t.Errorf("quit_list = %v, want [Q]", cfg.Keys.QuitList)
	}
}

// TestSetKeyBindingInConfigCreatesFile writes into a nonexistent file (and dir),
// producing a valid config with just the one binding overridden.
func TestSetKeyBindingInConfigCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	if err := setKeyBindingInConfig(path, "settings", []string{"ctrl+s"}); err != nil {
		t.Fatalf("write to fresh path: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !slices.Equal(cfg.Keys.Settings, config.Binding{"ctrl+s"}) {
		t.Errorf("settings = %v, want [ctrl+s]", cfg.Keys.Settings)
	}
	// Everything else stays at its default.
	if !slices.Equal(cfg.Keys.Remove, config.DefaultKeys().Remove) {
		t.Errorf("unset action lost its default: remove = %v", cfg.Keys.Remove)
	}
}

// TestSetKeyBindingInConfigTolerantFormatting exercises the writer against the
// hand-written TOML a user may bring: a [keys] header carrying an inline comment,
// a tab-aligned assignment, and a multi-line array value. Each must be replaced
// in place — not duplicated (which would make [keys]/the key defined twice) or
// left with orphaned continuation lines — so config.Load still succeeds.
func TestSetKeyBindingInConfigTolerantFormatting(t *testing.T) {
	t.Run("inline-comment header and tab alignment", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.toml")
		body := "[keys]  # my bindings\nremove\t= [\"k\", \"ctrl+k\"]\n"
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := setKeyBindingInConfig(path, "remove", []string{"x"}); err != nil {
			t.Fatalf("rebind: %v", err)
		}
		cfg, err := config.Load(path)
		if err != nil {
			t.Fatalf("load after rebind (writer duplicated a table/key): %v", err)
		}
		if !slices.Equal(cfg.Keys.Remove, config.Binding{"x"}) {
			t.Errorf("remove = %v, want [x]", cfg.Keys.Remove)
		}
		raw, _ := os.ReadFile(path)
		if n := strings.Count(string(raw), "[keys]"); n != 1 {
			t.Errorf("want a single [keys] table, got %d:\n%s", n, raw)
		}
		if n := strings.Count(string(raw), "remove"); n != 1 {
			t.Errorf("want a single remove assignment, got %d:\n%s", n, raw)
		}
	})

	t.Run("multi-line array value", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.toml")
		body := "[keys]\nremove = [\n  \"k\",\n  \"ctrl+k\",\n]\nfilter = [\"/\"]\n"
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := setKeyBindingInConfig(path, "remove", []string{"x"}); err != nil {
			t.Fatalf("rebind: %v", err)
		}
		cfg, err := config.Load(path)
		if err != nil {
			t.Fatalf("load after rebind (multi-line array orphaned): %v", err)
		}
		if !slices.Equal(cfg.Keys.Remove, config.Binding{"x"}) {
			t.Errorf("remove = %v, want [x]", cfg.Keys.Remove)
		}
		// The sibling binding after the multi-line array must survive intact.
		if !slices.Equal(cfg.Keys.Filter, config.Binding{"/"}) {
			t.Errorf("filter = %v, want [/] (replace clipped the wrong lines)", cfg.Keys.Filter)
		}
	})

	t.Run("bracket runes in the replaced value round-trip", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := setKeyBindingInConfig(path, "remove", []string{"["}); err != nil {
			t.Fatalf("write bracket binding: %v", err)
		}
		// A '[' inside the string must not be counted as opening a multi-line
		// array; a follow-up replace must still find the single-line assignment.
		if err := setKeyBindingInConfig(path, "remove", []string{"]"}); err != nil {
			t.Fatalf("replace bracket binding: %v", err)
		}
		cfg, err := config.Load(path)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if !slices.Equal(cfg.Keys.Remove, config.Binding{"]"}) {
			t.Errorf("remove = %v, want []]", cfg.Keys.Remove)
		}
	})
}

// TestSettingsCaptureIgnoresUnnamedKey guards that a key event bubbletea reports
// with no canonical name never enters a binding — otherwise it would serialize to
// an empty key that config validation rejects, reverting the whole rebind.
func TestSettingsCaptureIgnoresUnnamedKey(t *testing.T) {
	m := selectSession(newTestModel(sampleSessions()), 0)
	m = send(m, "s")
	for _, r := range "remove" {
		m = send(m, string(r))
	}
	m = send(m, "enter")                                // start capture
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes}) // empty String(), no name
	m = next.(model)
	m = send(m, "x")
	if !slices.Equal(m.settingsPending, []string{"x"}) {
		t.Fatalf("pending = %v, want [x] (unnamed key must be dropped)", m.settingsPending)
	}
}

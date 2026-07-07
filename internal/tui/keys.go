package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"xanax/internal/config"
)

// keys returns the resolved key bindings the dashboard dispatches through. It
// reads them straight off the config, so tests that build a model from
// config.Default() get the defaults with no extra wiring.
func (m model) keys() config.KeyMap { return m.deps.Cfg.Keys }

// keyMatches reports whether msg triggers any key in b. Keys are compared by
// bubbletea's canonical name (tea.KeyMsg.String()) — "enter", "ctrl+o", "o",
// "shift+tab" — after folding the two spellings of the spacebar so a binding may
// write it "space" or " ".
func keyMatches(b config.Binding, msg tea.KeyMsg) bool {
	s := canonKey(msg.String())
	for _, k := range b {
		if canonKey(k) == s {
			return true
		}
	}
	return false
}

// canonKey normalizes a key token: it folds the spacebar (reported by bubbletea
// as " ", written in config as "space") to one form and trims incidental
// whitespace around a token.
func canonKey(s string) string {
	if s == " " {
		return "space"
	}
	return strings.ToLower(strings.TrimSpace(s))
}

// keyHint renders a compact label for a binding's primary (first) key, for the
// footer hints — so the hints track whatever the keys are actually bound to.
func keyHint(b config.Binding) string {
	if len(b) == 0 {
		return ""
	}
	return keyLabel(b[0])
}

// keyLabel abbreviates one key for display: arrows as glyphs, control chords as
// "^x", shift+tab as "⇧tab"; anything else prints as written.
func keyLabel(k string) string {
	switch canonKey(k) {
	case "up":
		return "↑"
	case "down":
		return "↓"
	case "left":
		return "←"
	case "right":
		return "→"
	case "shift+tab":
		return "⇧tab"
	case "ctrl+c":
		return "^c"
	case "ctrl+o":
		return "^o"
	case "ctrl+k":
		return "^k"
	case "ctrl+r":
		return "^r"
	default:
		return k
	}
}

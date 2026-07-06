package tui

import (
	"github.com/charmbracelet/lipgloss"

	"xanax/internal/config"
	"xanax/internal/session"
)

// Colors and styles are package-level and rebuilt from the configured theme by
// applyTheme at startup (the TUI is single-instance, so mutating globals once
// before the program runs is safe). init seeds them with the defaults so tests
// that never call Run still have valid styles.
var (
	colWaiting, colRunning, colCompleted, colFailed, colCancelled lipgloss.Color
	colMuted, colAccent, colWhite, colBranch, colPR               lipgloss.Color

	titleStyle, groupStyle, mutedStyle, selectStyle, cursorStyle lipgloss.Style
	footerStyle, errStyle, fieldStyle, branchStyle, prStyle      lipgloss.Style
)

// warnStyle marks a transient safety prompt (the "press Ctrl+C again to exit"
// confirmation). Fixed orange (256-color 214), independent of the theme so the
// caution always reads as a caution whatever palette the user chose. 214 is
// deliberate: on a 16-color terminal it degrades to bright yellow, not red, so
// the prompt stays "orange or yellow" everywhere rather than mimicking an error.
var warnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)

// pickBarBg is the subtle slate highlight behind the selected harness-picker
// row (Telescope-style). A fixed dark tint rather than a theme color: it only
// needs to read as "slightly lighter than the terminal" on any dark palette.
var pickBarBg = lipgloss.Color("#33334d")

func init() { applyTheme(config.DefaultTheme()) }

// applyTheme rebuilds every color and derived style from t. t is expected to
// have all fields populated (config merges defaults), so each is used directly.
func applyTheme(t config.Theme) {
	colWaiting = lipgloss.Color(t.Waiting)
	colRunning = lipgloss.Color(t.Running)
	colCompleted = lipgloss.Color(t.Completed)
	colFailed = lipgloss.Color(t.Failed)
	colCancelled = lipgloss.Color(t.Cancelled)
	colMuted = lipgloss.Color(t.Muted)
	colAccent = lipgloss.Color(t.Accent)
	colWhite = lipgloss.Color(t.Text)
	colBranch = lipgloss.Color(t.Branch)
	colPR = lipgloss.Color(t.PR)

	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	groupStyle = lipgloss.NewStyle().Bold(true)
	mutedStyle = lipgloss.NewStyle().Foreground(colMuted)
	selectStyle = lipgloss.NewStyle().Bold(true)
	cursorStyle = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	footerStyle = lipgloss.NewStyle().Foreground(colMuted)
	errStyle = lipgloss.NewStyle().Foreground(colFailed).Bold(true)
	fieldStyle = lipgloss.NewStyle().Foreground(colMuted)
	branchStyle = lipgloss.NewStyle().Foreground(colBranch)
	prStyle = lipgloss.NewStyle().Foreground(colPR)
}

// whiteText is the style for editable input text.
func whiteText() lipgloss.Style { return lipgloss.NewStyle().Foreground(colWhite) }

// hRules wraps content with full-width top and bottom horizontal rules only
// (no left/right border), colored as given. Used to mark the selected session
// (accent) and frame the prompt box (grey).
func hRules(color lipgloss.Color, width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), true, false, true, false).
		BorderForeground(color).
		Width(width).
		PaddingLeft(1)
}

// statusColor maps a session status to its accent color.
func statusColor(s session.Status) lipgloss.Color {
	switch s {
	case session.StatusWaiting:
		return colWaiting
	case session.StatusRunning, session.StatusStarting:
		return colRunning
	case session.StatusCompleted:
		return colCompleted
	case session.StatusFailed:
		return colFailed
	case session.StatusCancelled:
		return colCancelled
	default:
		return colMuted
	}
}

// statusGlyph is a compact indicator per status.
func statusGlyph(s session.Status) string {
	switch s {
	case session.StatusWaiting:
		return "?"
	case session.StatusRunning:
		return "•"
	case session.StatusStarting:
		return "◦"
	case session.StatusCompleted:
		return "✓"
	case session.StatusFailed:
		return "✗"
	case session.StatusCancelled:
		return "⊘"
	default:
		return " "
	}
}

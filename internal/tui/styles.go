package tui

import (
	"github.com/charmbracelet/lipgloss"

	"xanax/internal/session"
)

var (
	colWaiting   = lipgloss.Color("11") // yellow: needs input
	colRunning   = lipgloss.Color("12") // blue: working
	colCompleted = lipgloss.Color("10") // green
	colFailed    = lipgloss.Color("9")  // red
	colCancelled = lipgloss.Color("8")  // grey
	colMuted     = lipgloss.Color("244")
	colAccent    = lipgloss.Color("13")
	colWhite     = lipgloss.Color("15")

	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	groupStyle  = lipgloss.NewStyle().Bold(true)
	mutedStyle  = lipgloss.NewStyle().Foreground(colMuted)
	selectStyle = lipgloss.NewStyle().Bold(true)
	cursorStyle = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	footerStyle = lipgloss.NewStyle().Foreground(colMuted)
	errStyle    = lipgloss.NewStyle().Foreground(colFailed).Bold(true)
	fieldStyle  = lipgloss.NewStyle().Foreground(colMuted)
	branchStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6")) // cyan
	prStyle     = lipgloss.NewStyle().Foreground(colCompleted)        // green
)

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

package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"xanax/internal/session"
)

func (m model) View() string {
	if m.width == 0 {
		return "loading…"
	}
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n\n")
	b.WriteString(m.renderList())
	b.WriteString("\n")
	switch {
	case m.renaming:
		b.WriteString(m.renderRename())
	case m.picking:
		b.WriteString(m.renderPicker())
	default:
		b.WriteString(m.renderComposer(m.onComposer))
	}
	b.WriteString("\n")
	b.WriteString(m.footer())
	return b.String()
}

func (m model) header() string {
	counts := map[int]int{}
	for _, s := range m.sessions {
		counts[groupRank(s.Status)]++
	}
	summary := fmt.Sprintf("%d sessions", len(m.sessions))
	if counts[0] > 0 {
		summary += mutedStyle.Render(fmt.Sprintf("  ·  %d awaiting input", counts[0]))
	}
	if counts[1] > 0 {
		summary += mutedStyle.Render(fmt.Sprintf("  ·  %d running", counts[1]))
	}
	line := titleStyle.Render("xanax")
	if m.deps.Scope != "" {
		line += mutedStyle.Render(" ▸ " + repoName(m.deps.Scope))
	}
	line += "   " + summary
	if m.err != nil {
		line += "   " + errStyle.Render(m.err.Error())
	}
	return line
}

func (m model) renderList() string {
	if len(m.sessions) == 0 {
		return mutedStyle.Render("No sessions yet — type a prompt below and press enter.")
	}

	var b strings.Builder
	lastRank := -1
	for i, s := range m.sessions {
		if r := groupRank(s.Status); r != lastRank {
			if lastRank != -1 {
				b.WriteString("\n")
			}
			hdr := groupStyle.Foreground(statusColor(s.Status)).Render(
				fmt.Sprintf("▍ %s", groupLabel(r)))
			b.WriteString(hdr)
			b.WriteByte('\n')
			lastRank = r
		}
		b.WriteString(m.renderRow(s, !m.onComposer && i == m.cursor))
		b.WriteString("\n")
	}
	return b.String()
}

func (m model) renderRow(s *session.Session, selected bool) string {
	glyph := lipgloss.NewStyle().Foreground(statusColor(s.Status)).Render(statusGlyph(s.Status))
	title := truncate(s.Title, 46)
	if selected {
		title = selectStyle.Render(title)
	}
	meta := mutedStyle.Render(fmt.Sprintf("%s · %s · %s",
		s.Harness, repoName(s.RepoPath), humanAge(s.CreatedAt)))

	content := fmt.Sprintf("%s %s   %s", glyph, title, meta)
	if s.Status == session.StatusWaiting && s.StatusDetail != "" {
		content += mutedStyle.Render("  — " + truncate(s.StatusDetail, 40))
	}

	if !selected {
		// Align with the ruled row's interior (padding-left = 1 col).
		return " " + content
	}
	// The selected session gets full-width top+bottom rules in the accent color.
	return hRules(colAccent, m.width).Render(content)
}

// renderComposer draws the always-on prompt block: a label plus the input
// framed by full-width top+bottom rules — accent when the box is selected
// (typing goes here), grey otherwise. The label names the harness the next
// session will use; Tab cycles it.
func (m model) renderComposer(selected bool) string {
	color := colMuted
	label := mutedStyle.Render(
		"New " + m.harness() + " session  ·  ↓ to select, then type a prompt")
	if selected {
		color = colAccent
		title := groupStyle.Foreground(colAccent).Render("New " + m.harness() + " session")
		label = title + mutedStyle.Render("  ·  tab to switch harness")
	}
	return label + "\n" + hRules(color, m.width).Render(m.composer.View())
}

// renderPicker draws the harness selector in the composer's slot: one row per
// configured harness, ↑/↓ to move, enter to pick.
func (m model) renderPicker() string {
	label := groupStyle.Foreground(colAccent).Render("Select harness") +
		mutedStyle.Render("  ·  ↑/↓ move, enter select, esc cancel")
	var rows []string
	for i, name := range m.harnesses {
		adapter := m.deps.Cfg.Harnesses[name].Adapter
		line := "  " + name
		if i == m.pickIdx {
			line = cursorStyle.Render("▸ ") + selectStyle.Render(name)
		}
		if i == m.harnessIdx {
			line += mutedStyle.Render("  (current)")
		}
		line += mutedStyle.Render("  · " + adapter)
		rows = append(rows, line)
	}
	return label + "\n" + hRules(colAccent, m.width).Render(strings.Join(rows, "\n"))
}

// renderRename draws the in-place rename editor for the selected session.
func (m model) renderRename() string {
	label := groupStyle.Foreground(colAccent).Render(
		"Rename session  ·  enter to save, esc to cancel")
	return label + "\n" + hRules(colAccent, m.width).Render(m.renameInput.View())
}

func (m model) footer() string {
	var hint string
	switch {
	case m.renaming:
		hint = "enter save · esc cancel"
	case m.picking:
		hint = "↑/↓ move · enter select · esc cancel"
	case m.onComposer:
		hint = "enter launch · ^o launch+attach · tab harness · ↑ sessions · ^c quit"
	default:
		hint = "↑/↓ select · →/enter open · e rename · r resume · k remove · ↓ to prompt · ^c quit"
	}
	out := footerStyle.Render(hint)
	if m.status != "" {
		out = mutedStyle.Render(m.status) + "   " + out
	}
	return out
}

// --- small helpers ---

func repoName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 && i < len(p)-1 {
		return p[i+1:]
	}
	return p
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}

func humanAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

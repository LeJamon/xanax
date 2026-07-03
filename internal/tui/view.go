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
	if p := m.renderPreview(); p != "" {
		b.WriteString(p)
		b.WriteString("\n")
	}
	switch {
	case m.renaming:
		b.WriteString(m.renderRename())
	case m.addingHarness:
		b.WriteString(m.renderHarnessForm())
	case m.picking:
		b.WriteString(m.renderPicker())
	case m.filtering:
		b.WriteString(m.renderFilter())
	default:
		b.WriteString(m.renderComposer(m.onComposer))
	}
	b.WriteString("\n")
	b.WriteString(m.footer())
	return b.String()
}

// renderPreview shows a peek of the selected session's screen when one is
// selected and a preview has been fetched.
func (m model) renderPreview() string {
	if m.onComposer || m.previewText == "" || m.previewID != m.selectedID() {
		return ""
	}
	label := mutedStyle.Render("Preview  ·  " + m.previewID[:min(8, len(m.previewID))])
	body := lipgloss.NewStyle().Foreground(colMuted).Render(m.previewText)
	return label + "\n" + hRules(colMuted, m.width).Render(body)
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
	if m.filter != "" {
		line += "   " + branchStyle.Render("filter: "+m.filter)
	}
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

	// Live git context (branch · #PR), right-aligned against the row edge.
	content = padRight(content, m.gitSuffix(s.RepoPath), m.width-2)

	if !selected {
		// Align with the ruled row's interior (padding-left = 1 col).
		return " " + content
	}
	// The selected session gets full-width top+bottom rules in the accent color.
	return hRules(colAccent, m.width).Render(content)
}

// gitSuffix renders " branch · #pr" for a repo, or "" when unknown.
func (m model) gitSuffix(repo string) string {
	gi := m.gitCache[repo]
	if gi.branch == "" {
		return ""
	}
	out := branchStyle.Render(" " + gi.branch)
	if gi.pr != "" {
		out += mutedStyle.Render(" · ") + prStyle.Render("#"+gi.pr)
	}
	return out
}

// padRight places right at the row's right edge, or drops it when there is no
// room (both operands may contain ANSI styling, so widths are measured).
func padRight(left, right string, width int) string {
	if right == "" {
		return left
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

// renderComposer draws the always-on prompt block: a label plus the input
// framed by full-width top+bottom rules — accent when the box is selected
// (typing goes here), grey otherwise. The label names the harness the next
// session will use; Tab opens the picker to switch it.
func (m model) renderComposer(selected bool) string {
	color := colMuted
	label := mutedStyle.Render(
		"New " + m.harness() + " session  ·  ↓ to select, then type a prompt")
	if selected {
		color = colAccent
		title := groupStyle.Foreground(colAccent).Render("New " + m.harness() + " session")
		hint := "  ·  type a prompt, enter to launch"
		if len(m.harnesses) > 1 { // only when Tab has a harness to switch to
			hint = "  ·  tab to switch harness"
		}
		label = title + mutedStyle.Render(hint)
	}
	return label + "\n" + hRules(color, m.width).Render(m.composer.View())
}

// renderHarnessForm draws the add-harness form in the composer's slot.
func (m model) renderHarnessForm() string {
	var b strings.Builder
	b.WriteString(groupStyle.Foreground(colAccent).Render("Add harness (generic)"))
	b.WriteString(mutedStyle.Render("  ·  tab next field, enter save, esc cancel"))
	b.WriteByte('\n')
	for i, in := range m.formInputs {
		marker := "  "
		if i == m.formField {
			marker = cursorStyle.Render("▸ ")
		}
		fmt.Fprintf(&b, "%s%s %s\n", marker, fieldStyle.Render(fmt.Sprintf("%-22s", formLabels[i])), in.View())
	}
	if m.formErr != "" {
		b.WriteString(errStyle.Render("  " + m.formErr))
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderPicker draws the harness selector in the composer's slot: one row per
// configured harness, ↑/↓ to move, enter to pick, + to add a new one.
func (m model) renderPicker() string {
	label := groupStyle.Foreground(colAccent).Render("Select harness") +
		mutedStyle.Render("  ·  ↑/↓ move, enter select, + add, esc cancel")
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

// renderFilter draws the filter input bar.
func (m model) renderFilter() string {
	label := groupStyle.Foreground(colAccent).Render("Filter") +
		mutedStyle.Render("  ·  title / repo / harness — enter apply, esc clear")
	return label + "\n" + hRules(colAccent, m.width).Render(m.filterInput.View())
}

// renderRename draws the in-place rename editor for the selected session.
func (m model) renderRename() string {
	label := groupStyle.Foreground(colAccent).Render(
		"Rename session  ·  enter to save, esc to cancel")
	return label + "\n" + hRules(colAccent, m.width).Render(m.renameInput.View())
}

func (m model) footer() string {
	// A pending quit confirmation takes over the footer with a caution prompt.
	if m.confirmQuit {
		return warnStyle.Render("Press Ctrl+C again to exit xanax")
	}
	var hint string
	switch {
	case m.addingHarness:
		hint = "tab next · enter save · esc cancel"
	case m.renaming:
		hint = "enter save · esc cancel"
	case m.picking:
		hint = "↑/↓ move · enter select · + add · esc cancel"
	case m.filtering:
		hint = "type to filter · enter apply · esc clear"
	case m.onComposer:
		hint = "enter launch · ^o launch+attach · tab harness (+ add) · ↑ sessions · ^c quit"
	default:
		hint = "↑/↓ select · →/enter open · e rename · r resume · k remove · / filter · ^c quit"
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

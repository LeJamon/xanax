package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/LeJamon/rvr/internal/config"
	"github.com/LeJamon/rvr/internal/session"
)

// pickerModalWidth returns the modal's content width (the columns between the
// two vertical borders), scaled to ~65% of the terminal and clamped so the box
// never overflows a narrow window.
func pickerModalWidth(termWidth int) int {
	w := int(math.Max(40, math.Min(float64(termWidth)*0.65, 80)))
	return min(w, max(20, termWidth-2))
}

// modalBox draws body inside a rounded border colored col, w columns wide (the
// span between the two vertical bars), with title centered on the top edge.
// Each body line is clamped and padded to exactly w columns, so callers control
// their own inner margins — a full-width selection bar reaches both borders.
func modalBox(col lipgloss.Color, w int, title, body string) string {
	bd := lipgloss.RoundedBorder()
	edge := lipgloss.NewStyle().Foreground(col)
	left, right := edge.Render(bd.Left), edge.Render(bd.Right)
	var b strings.Builder
	b.WriteString(borderTop(bd, edge, w, title))
	b.WriteByte('\n')
	for ln := range strings.SplitSeq(body, "\n") {
		ln = lipgloss.NewStyle().MaxWidth(w).Render(ln)
		pad := max(0, w-lipgloss.Width(ln))
		b.WriteString(left)
		b.WriteString(ln)
		b.WriteString(strings.Repeat(" ", pad))
		b.WriteString(right)
		b.WriteByte('\n')
	}
	bottom := edge.Render(bd.BottomLeft + strings.Repeat(bd.Bottom, w) + bd.BottomRight)
	b.WriteString(bottom)
	return b.String()
}

// borderTop builds a box's top edge with title centered among the dashes.
func borderTop(bd lipgloss.Border, edge lipgloss.Style, w int, title string) string {
	if title == "" {
		return edge.Render(bd.TopLeft + strings.Repeat(bd.Top, w) + bd.TopRight)
	}
	t := " " + title + " "
	tw := min(lipgloss.Width(t), w)
	left := (w - tw) / 2
	right := w - tw - left
	return edge.Render(bd.TopLeft+strings.Repeat(bd.Top, left)) +
		groupStyle.Foreground(colAccent).Render(t) +
		edge.Render(strings.Repeat(bd.Top, right)+bd.TopRight)
}

// renderPickerModal draws the harness picker as two stacked Telescope-style
// panels: a "Harnesses" results box above a search box that carries the prompt
// and a live match count. The whole block is returned left-aligned; the caller
// centers it in the terminal.
func (m model) renderPickerModal() string {
	w := pickerModalWidth(m.width)
	filtered := m.filteredHarnesses()

	// Results panel — a fixed-height window sized to the harness count (so it
	// does not jump as filtering narrows the matches) but capped to what fits.
	rows := min(m.visibleRows(), max(1, len(m.harnesses)))
	start := m.pickScroll
	if start > len(filtered)-rows {
		start = max(0, len(filtered)-rows)
	}
	start = max(0, start)
	end := min(start+rows, len(filtered))

	var lines []string
	if len(filtered) == 0 {
		lines = append(lines, "   "+mutedStyle.Render("no matching harness"))
	} else {
		for i := start; i < end; i++ {
			lines = append(lines, m.pickRow(filtered[i], i == m.pickIdx, w))
		}
	}
	for len(lines) < rows { // keep the panel height stable while filtering
		lines = append(lines, "")
	}
	results := modalBox(colMuted, w, "Harnesses", strings.Join(lines, "\n"))

	// Search panel — accent prompt, the live search input, and a match count
	// (matches / total) pinned to the right edge, mirroring Telescope.
	prompt := cursorStyle.Render(" > ") + m.searchInput.View()
	count := mutedStyle.Render(fmt.Sprintf("%d / %d ", len(filtered), len(m.harnesses)))
	search := modalBox(colMuted, w, "Switch harness", padRight(prompt, count, w))

	return results + "\n" + search
}

// pickRow renders one harness entry: the name on the left, its adapter and any
// (current)/(default) tag muted on the right. The selected row is drawn as a
// full-width highlight bar; every segment carries the bar background so inner
// style resets do not punch holes in it.
func (m model) pickRow(name string, selected bool, w int) string {
	adapter := m.deps.Cfg.Harnesses[name].Adapter
	meta := adapter
	switch name {
	case m.harnesses[m.harnessIdx]:
		meta += "  (current)"
	case m.deps.Cfg.DefaultHarness:
		meta += "  (default)"
	}
	if !selected {
		return padRight("   "+name, mutedStyle.Render(meta+" "), w)
	}
	bar := func(fg lipgloss.Color, bold bool) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(fg).Background(pickBarBg).Bold(bold)
	}
	left := bar(colAccent, true).Render(" ▸ " + name)
	right := bar(colMuted, false).Render(meta + " ")
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 { // no room for the meta — drop it and just fill the bar
		right = ""
		gap = max(0, w-lipgloss.Width(left))
	}
	return left + bar(colMuted, false).Render(strings.Repeat(" ", gap)) + right
}

// renderSettingsModal draws the keybindings editor as two stacked panels like
// the harness picker: an "action → keys" results box above a search box. While
// capturing a new key the results title prompts for it and the lower box shows
// the abort hint instead of the search input.
func (m model) renderSettingsModal() string {
	w := pickerModalWidth(m.width)
	filtered := m.filteredActions()
	total := len(m.deps.Cfg.Keys.Actions())

	rows := min(m.visibleRows(), max(1, total))
	start := m.settingsScroll
	if start > len(filtered)-rows {
		start = max(0, len(filtered)-rows)
	}
	start = max(0, start)
	end := min(start+rows, len(filtered))

	var lines []string
	if len(filtered) == 0 {
		lines = append(lines, "   "+mutedStyle.Render("no matching action"))
	} else {
		for i := start; i < end; i++ {
			lines = append(lines, m.settingsRow(filtered[i], i == m.settingsIdx, w))
		}
	}
	for len(lines) < rows { // keep the panel height stable while filtering
		lines = append(lines, "")
	}

	title := "Keybindings"
	if m.settingsCapture && m.settingsIdx < len(filtered) {
		title = "Press keys for " + filtered[m.settingsIdx].Name
	}
	results := modalBox(colMuted, w, title, strings.Join(lines, "\n"))

	if m.settingsCapture {
		left := cursorStyle.Render(" > ")
		if len(m.settingsPending) == 0 {
			left += mutedStyle.Render("press keys…")
		} else {
			left += whiteText().Render(strings.Join(m.settingsPending, ", "))
		}
		hint := mutedStyle.Render("enter save · esc cancel ")
		return results + "\n" + modalBox(colMuted, w, "Rebind", padRight(left, hint, w))
	}
	prompt := cursorStyle.Render(" > ") + m.settingsInput.View()
	count := mutedStyle.Render(fmt.Sprintf("%d / %d ", len(filtered), total))
	return results + "\n" + modalBox(colMuted, w, "Settings", padRight(prompt, count, w))
}

// settingsRow renders one action: its name on the left and its current keys
// (muted) on the right, with the highlighted row drawn as a full-width bar like
// the harness picker's selected row. Unbound actions read "unbound".
func (m model) settingsRow(a config.KeyAction, selected bool, w int) string {
	keys := strings.Join(a.Keys, ", ")
	if keys == "" {
		keys = "unbound"
	}
	if !selected {
		return padRight("   "+a.Name, mutedStyle.Render(keys+" "), w)
	}
	bar := func(fg lipgloss.Color, bold bool) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(fg).Background(pickBarBg).Bold(bold)
	}
	left := bar(colAccent, true).Render(" ▸ " + a.Name)
	right := bar(colMuted, false).Render(keys + " ")
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		right = ""
		gap = max(0, w-lipgloss.Width(left))
	}
	return left + bar(colMuted, false).Render(strings.Repeat(" ", gap)) + right
}

func (m model) View() string {
	if m.width == 0 {
		return "loading…"
	}
	fit := lipgloss.NewStyle().MaxWidth(m.width)
	header := fit.Render(m.header())
	bottom := m.inputBlock() + "\n" + m.footer()
	bottom = fit.Render(bottom)
	preview := ""
	if p := m.renderPreview(); p != "" {
		preview = fit.Render(p)
	}

	listRows := m.listRowsAvailable(header, bottom, preview)
	top := header + "\n\n" + m.renderList(listRows)
	if preview != "" {
		top += "\n" + preview
	}
	// Clamp every line to the terminal width so nothing soft-wraps.
	top = fit.Render(top)

	if modal := m.centeredModal(); modal != "" {
		return m.renderCenteredModal(top, modal)
	}

	gap := max(1, m.height-lipgloss.Height(top)-lipgloss.Height(bottom)+1)
	return top + strings.Repeat("\n", gap) + bottom
}

func (m model) listRowsAvailable(header, bottom, preview string) int {
	if m.height <= 0 {
		return len(m.sessions)*3 + 8
	}
	previewRows := 0
	if preview != "" {
		previewRows = 1 + lipgloss.Height(preview)
	}
	return max(1, m.height-lipgloss.Height(header)-2-previewRows-lipgloss.Height(bottom)-1)
}

// centeredModal returns the modal that floats over the dashboard — the harness
// picker or the harness add/modify form — or "" when neither is open.
func (m model) centeredModal() string {
	switch {
	case m.picking:
		return m.renderPickerModal()
	case m.settingsOn:
		return m.renderSettingsModal()
	case m.addingHarness:
		return m.renderHarnessFormModal()
	}
	return ""
}

// renderCenteredModal floats a modal in the vertical center of the terminal: the
// header/session list stays at the top for context and the footer pins to the
// bottom, with blank rows above and below the modal splitting the slack so the
// box lands in the middle of the screen.
func (m model) renderCenteredModal(top, modal string) string {
	modal = lipgloss.PlaceHorizontal(m.width, lipgloss.Center, modal)
	footer := lipgloss.NewStyle().MaxWidth(m.width).Render(m.footer())

	modalH := lipgloss.Height(modal)
	above := max(0, (m.height-modalH)/2) // equal space above and below centers the modal
	below := max(0, m.height-modalH-above)
	blankAbove := max(0, above-lipgloss.Height(top))
	blankBelow := max(0, below-lipgloss.Height(footer))

	var b strings.Builder
	b.WriteString(top)
	b.WriteByte('\n')
	b.WriteString(strings.Repeat("\n", blankAbove))
	b.WriteString(modal)
	b.WriteByte('\n')
	b.WriteString(strings.Repeat("\n", blankBelow))
	b.WriteString(footer)
	return b.String()
}

// inputBlock renders whichever editor occupies the bottom prompt slot: the
// always-present composer, or a modal editor (rename, filter) when one is open.
// The harness picker and add/modify form float as centered modals instead (see
// centeredModal), so they are not listed here.
func (m model) inputBlock() string {
	switch {
	case m.renaming:
		return m.renderRename()
	case m.filtering:
		return m.renderFilter()
	default:
		return m.renderComposer(m.onComposer)
	}
}

// renderPreview shows a peek of the selected session's screen once space has
// opened the preview and a snapshot has been fetched.
func (m model) renderPreview() string {
	if !m.previewOn || m.onComposer {
		return ""
	}
	s := m.current()
	if s == nil {
		return ""
	}
	bodyText := m.previewText
	if bodyText == "" {
		bodyText = terminalDetailText(s)
	}
	if bodyText == "" {
		return ""
	}
	id := s.ID
	title := m.previewLabel
	if title == "" {
		title = "Preview"
	}
	label := mutedStyle.Render(title + "  ·  " + id[:min(8, len(id))])
	body := lipgloss.NewStyle().Foreground(colMuted).Render(bodyText)
	return label + "\n" + hRules(colMuted, m.width).Render(body)
}

// pill is the rvr logo: a small rounded capsule ("pink pill") whose three
// rows sit to the left of the three-line title/path/counts block, mirroring the
// masthead layout. Colored with the accent (pink/magenta by default).
const pill = "▟█████▙\n" +
	"███████\n" +
	"▜█████▛"

func (m model) header() string {
	logo := lipgloss.NewStyle().Foreground(colAccent).Render(pill)

	title := titleStyle.Render("rvr")
	if m.deps.Version != "" {
		title += mutedStyle.Render(" v" + m.deps.Version)
	}

	counts := m.counts()
	if m.filter != "" {
		counts += "   " + branchStyle.Render("filter: "+m.filter)
	}
	if m.err != nil {
		counts += "   " + errStyle.Render(m.err.Error())
	}

	lines := []string{title}
	if m.path != "" { // omit the path row entirely rather than show a blank line
		lines = append(lines, mutedStyle.Render(m.path))
	}
	lines = append(lines, counts)
	text := strings.Join(lines, "\n")
	return lipgloss.JoinHorizontal(lipgloss.Top, logo, "   ", text)
}

// counts renders the session tallies. The three primary buckets — awaiting
// input, working, completed — are always shown (even at zero) with each number
// in its status color; cancelled, failed, and other are appended only when
// non-empty, so the header always reconciles with the list below without
// cluttering the common case.
func (m model) counts() string {
	var c [7]int // indexed by groupRank: waiting, idle, running, completed, cancelled, failed, other
	for _, s := range m.sessions {
		r := groupRank(s.Status)
		if r >= len(c) {
			r = len(c) - 1 // fold any unexpected rank into "other" rather than drop it
		}
		c[r]++
	}
	seg := func(n int, col lipgloss.Color, label string) string {
		return lipgloss.NewStyle().Foreground(col).Render(fmt.Sprintf("%d", n)) +
			mutedStyle.Render(" "+label)
	}
	dot := mutedStyle.Render("  ·  ")
	out := seg(c[0], colWaiting, "awaiting input")
	if c[1] > 0 {
		out += dot + seg(c[1], colMuted, "idle")
	}
	out += dot + seg(c[2], colRunning, "working") + dot +
		seg(c[3], colCompleted, "completed")
	if c[4] > 0 {
		out += dot + seg(c[4], colCancelled, "cancelled")
	}
	if c[5] > 0 {
		out += dot + seg(c[5], colFailed, "failed")
	}
	if c[6] > 0 {
		out += dot + seg(c[6], colMuted, "other")
	}
	return out
}

type listBlock struct {
	lines        []string
	sessionIndex int
}

func (m model) renderList(maxRows int) string {
	if len(m.sessions) == 0 {
		return mutedStyle.Render("No sessions yet — type a prompt below and press enter.")
	}
	if maxRows <= 0 {
		return listOverflowLine(len(m.sessions), "hidden")
	}

	blocks := m.listBlocks()
	if blocksHeight(blocks) <= maxRows {
		return joinBlocks(blocks)
	}

	anchor := -1
	if !m.onComposer {
		anchor = blockForSession(blocks, m.cursor)
	}
	start, end := chooseListWindow(blocks, anchor, maxRows)
	topHidden := hiddenSessions(blocks[:start])
	bottomHidden := hiddenSessions(blocks[end:])

	contentRows := maxRows
	if topHidden > 0 {
		contentRows--
	}
	if bottomHidden > 0 {
		contentRows--
	}
	if contentRows <= 0 {
		return listOverflowLine(len(m.sessions), "hidden")
	}

	start, end = chooseListWindow(blocks, anchor, contentRows)
	topHidden = hiddenSessions(blocks[:start])
	bottomHidden = hiddenSessions(blocks[end:])

	var lines []string
	if topHidden > 0 {
		lines = append(lines, listOverflowLine(topHidden, "above"))
	}
	for _, b := range blocks[start:end] {
		lines = append(lines, b.lines...)
	}
	if bottomHidden > 0 {
		lines = append(lines, listOverflowLine(bottomHidden, "below"))
	}
	if len(lines) > maxRows {
		lines = lines[:maxRows]
	}
	return strings.Join(lines, "\n")
}

func (m model) listBlocks() []listBlock {
	var blocks []listBlock
	lastRank := -1
	for i, s := range m.sessions {
		if r := groupRank(s.Status); r != lastRank {
			if lastRank != -1 {
				blocks = append(blocks, listBlock{lines: []string{""}, sessionIndex: -1})
			}
			hdr := groupStyle.Foreground(statusColor(s.Status)).Render(
				fmt.Sprintf("▍ %s", groupLabel(r)))
			blocks = append(blocks, listBlock{lines: []string{hdr}, sessionIndex: -1})
			lastRank = r
		}
		blocks = append(blocks, listBlock{
			lines:        strings.Split(m.renderRow(s, !m.onComposer && i == m.cursor), "\n"),
			sessionIndex: i,
		})
	}
	return blocks
}

func chooseListWindow(blocks []listBlock, anchor, maxRows int) (int, int) {
	if len(blocks) == 0 || maxRows <= 0 {
		return 0, 0
	}
	if anchor < 0 {
		rows, end := 0, 0
		for end < len(blocks) && rows+len(blocks[end].lines) <= maxRows {
			rows += len(blocks[end].lines)
			end++
		}
		return 0, max(1, end)
	}

	start, end := anchor, anchor+1
	rows := len(blocks[anchor].lines)
	preferAbove := true
	for rows < maxRows && (start > 0 || end < len(blocks)) {
		added := false
		if preferAbove {
			added = extendListWindowAbove(blocks, &start, &rows, maxRows)
			if !added {
				added = extendListWindowBelow(blocks, &end, &rows, maxRows)
			}
		} else {
			added = extendListWindowBelow(blocks, &end, &rows, maxRows)
			if !added {
				added = extendListWindowAbove(blocks, &start, &rows, maxRows)
			}
		}
		if !added {
			break
		}
		preferAbove = !preferAbove
	}
	return start, end
}

func extendListWindowAbove(blocks []listBlock, start, rows *int, maxRows int) bool {
	if *start == 0 {
		return false
	}
	h := len(blocks[*start-1].lines)
	if *rows+h > maxRows {
		return false
	}
	*start = *start - 1
	*rows += h
	return true
}

func extendListWindowBelow(blocks []listBlock, end, rows *int, maxRows int) bool {
	if *end >= len(blocks) {
		return false
	}
	h := len(blocks[*end].lines)
	if *rows+h > maxRows {
		return false
	}
	*end = *end + 1
	*rows += h
	return true
}

func blockForSession(blocks []listBlock, sessionIndex int) int {
	for i, b := range blocks {
		if b.sessionIndex == sessionIndex {
			return i
		}
	}
	return -1
}

func blocksHeight(blocks []listBlock) int {
	rows := 0
	for _, b := range blocks {
		rows += len(b.lines)
	}
	return rows
}

func hiddenSessions(blocks []listBlock) int {
	n := 0
	for _, b := range blocks {
		if b.sessionIndex >= 0 {
			n++
		}
	}
	return n
}

func joinBlocks(blocks []listBlock) string {
	var lines []string
	for _, b := range blocks {
		lines = append(lines, b.lines...)
	}
	return strings.Join(lines, "\n")
}

func listOverflowLine(n int, where string) string {
	label := "sessions"
	if n == 1 {
		label = "session"
	}
	return mutedStyle.Render(fmt.Sprintf("… %d more %s %s", n, label, where))
}

func (m model) renderRow(s *session.Session, selected bool) string {
	glyph := lipgloss.NewStyle().Foreground(statusColor(s.Status)).Render(statusGlyph(s.Status))
	title := truncate(s.Title, 46)
	if selected {
		title = selectStyle.Render(title)
	}
	meta := mutedStyle.Render(fmt.Sprintf("%s · %s · %s · %s",
		shortID(s.ID), s.Harness, repoName(s.RepoPath), humanAge(s.CreatedAt)))

	content := fmt.Sprintf("%s %s   %s", glyph, title, meta)
	if detail := rowDetailText(s); detail != "" {
		content += mutedStyle.Render("  — " + truncate(detail, 48))
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

func rowDetailText(s *session.Session) string {
	var parts []string
	if detail := strings.TrimSpace(s.StatusDetail); detail != "" {
		parts = append(parts, detail)
	}
	if (s.Status == session.StatusFailed || s.Status == session.StatusCancelled) && s.ExitCode != nil {
		parts = append(parts, fmt.Sprintf("exit %d", *s.ExitCode))
	}
	return strings.Join(parts, " · ")
}

func terminalDetailText(s *session.Session) string {
	if s.Status != session.StatusFailed && s.Status != session.StatusCancelled {
		return ""
	}
	return rowDetailText(s)
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

// renderHarnessFormModal draws the harness add/modify form as a centered modal
// matching the picker: a titled box with one labelled input per row, the focused
// row marked with an accent ▸, and a trailing status line (error, or the adapter
// being edited). editHarness switches the title between add and modify.
func (m model) renderHarnessFormModal() string {
	w := pickerModalWidth(m.width)
	title := "Add harness (generic)"
	if m.editHarness != "" {
		title = "Modify harness: " + m.editHarness
	}

	var lines []string
	for i, in := range m.formInputs {
		label := fmt.Sprintf("%-*s", formLabelCol, formLabels[i])
		if i == m.formField {
			lines = append(lines, cursorStyle.Render(" ▸ ")+groupStyle.Render(label)+" "+in.View())
		} else {
			lines = append(lines, "   "+fieldStyle.Render(label)+" "+in.View())
		}
	}
	lines = append(lines, "")
	switch {
	case m.formErr != "":
		lines = append(lines, "   "+errStyle.Render(m.formErr))
	case m.editHarness != "":
		lines = append(lines, "   "+mutedStyle.Render("adapter: "+m.deps.Cfg.Harnesses[m.editHarness].Adapter+"  ·  command splits on spaces"))
	default:
		lines = append(lines, "   "+mutedStyle.Render("command splits into command + args on spaces"))
	}
	return modalBox(colMuted, w, title, strings.Join(lines, "\n"))
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
	k := m.keys()
	// A pending quit confirmation takes over the footer with a caution prompt.
	if m.confirmQuit {
		return warnStyle.Render("Press " + keyHint(k.Quit) + " again to exit rvr")
	}
	if m.confirmRemoveID != "" {
		return warnStyle.Render("Press " + keyHint(k.Remove) + " again to kill and remove " + shortID(m.confirmRemoveID))
	}
	// Hints are built from the live bindings (keyHint), so remapping a key in the
	// config updates the footer to match rather than advertising a stale default.
	updown := keyHint(k.Up) + "/" + keyHint(k.Down)
	var hint string
	switch {
	case m.addingHarness:
		hint = fmt.Sprintf("%s next · %s save · %s cancel",
			keyHint(k.FormNext), keyHint(k.Confirm), keyHint(k.Cancel))
	case m.renaming:
		hint = fmt.Sprintf("%s save · %s cancel", keyHint(k.Confirm), keyHint(k.Cancel))
	case m.picking:
		if m.searchFocused {
			hint = fmt.Sprintf("type to search · %s move · %s select · %s add · %s for %s/%s · %s",
				updown, keyHint(k.Confirm), keyHint(k.AddHarness), keyHint(k.ToggleSearch),
				keyHint(k.SetDefault), keyHint(k.ModifyHarness), keyHint(k.Cancel))
		} else {
			hint = fmt.Sprintf("%s default · %s modify · %s move · %s select · %s add · %s search · %s",
				keyHint(k.SetDefault), keyHint(k.ModifyHarness), updown, keyHint(k.Confirm),
				keyHint(k.AddHarness), keyHint(k.ToggleSearch), keyHint(k.Cancel))
		}
	case m.filtering:
		hint = fmt.Sprintf("type to filter · %s apply · %s clear", keyHint(k.Confirm), keyHint(k.Cancel))
	case m.settingsOn:
		if m.settingsCapture {
			hint = "press keys to add · enter save · esc cancel"
		} else {
			hint = fmt.Sprintf("type to search · %s move · %s rebind · %s close",
				updown, keyHint(k.Confirm), keyHint(k.Cancel))
		}
	case m.onComposer && m.composer.Value() != "":
		hint = fmt.Sprintf("%s launch · %s launch+attach · %s harness (+ add) · ↑/↓ cursor · %s clear · %s quit",
			keyHint(k.Confirm), keyHint(k.LaunchAttach), keyHint(k.HarnessPicker),
			keyHint(k.Cancel), keyHint(k.Quit))
	case m.onComposer:
		hint = fmt.Sprintf("%s launch · %s launch+attach · %s harness (+ add) · %s sessions · %s quit",
			keyHint(k.Confirm), keyHint(k.LaunchAttach), keyHint(k.HarnessPicker),
			keyHint(k.Up), keyHint(k.Quit))
	default:
		hint = fmt.Sprintf("%s select · %s open · ← back · %s logs · %s preview · %s rename · %s resume · %s remove · %s settings · %s filter · %s quit",
			updown, keyHint(k.Open), keyHint(k.Logs), keyHint(k.Preview), keyHint(k.Rename), keyHint(k.Resume),
			keyHint(k.Remove), keyHint(k.Settings), keyHint(k.Filter), keyHint(k.Quit))
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

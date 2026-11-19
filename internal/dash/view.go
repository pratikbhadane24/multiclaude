package dash

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"multiclaude/internal/status"
	"multiclaude/internal/usage"
)

const (
	skipPermsArg = "--dangerously-skip-permissions"
	badgeText    = "⚠ skip-permissions"
	sep          = " · "
	// paneChrome is the number of lines around the doctor pane (title, blank, footer).
	paneChrome = 3
)

// View renders the whole screen, never wider or taller than the window.
func (m Model) View() string {
	var body []string
	switch {
	case m.mode == modeHelp:
		body = m.helpLines()
	case m.mode == modeDoctor:
		body = []string{styleHeader.Render("Doctor") + styleDim.Render(sep+"↑↓ scroll · esc close"), "", m.pane.View()}
	case !m.loaded:
		body = []string{styleHeader.Render("ccm") + sep + "loading accounts…"}
	default:
		body = m.mainLines()
	}
	footer := m.footerLines()
	body = fitHeight(body, m.height-len(footer))
	return clip(strings.Join(append(body, footer...), "\n"), m.width)
}

func (m Model) mainLines() []string {
	lines := []string{m.headerLine(), ""}
	lines = append(lines, m.tableLines()...)
	lines = append(lines, "")
	lines = append(lines, m.detailLines()...)
	return append(lines, "", m.statusLine())
}

// fitHeight drops lines above the status line (details first) to fit h.
func fitHeight(lines []string, h int) []string {
	if h <= 0 {
		return nil
	}
	if len(lines) <= h {
		return lines
	}
	last := lines[len(lines)-1]
	return append(slices.Clone(lines[:h-1]), last)
}

// clip truncates every line to w cells.
func clip(s string, w int) string {
	return lipgloss.NewStyle().MaxWidth(w).Render(s)
}

func (m Model) selectedArgs() []string {
	name := m.selectedName()
	if name == "" {
		return nil
	}
	return m.deps.ArgsFor(name)
}

func (m Model) headerLine() string {
	c := m.snap.Combined
	parts := []string{
		styleHeader.Render("ccm"),
		fmt.Sprintf("%d accounts", len(m.snap.Rows)),
		"5h left " + leftText(c.FiveHourLeft, c.Counted),
		"7d left " + leftText(c.SevenDayLeft, c.Counted),
	}
	if t := m.nextReset(); t != nil {
		parts = append(parts, "next reset in "+status.FormatDuration(t.Sub(m.deps.Now())))
	}
	if m.loading {
		parts = append(parts, styleDim.Render("refreshing…"))
	}
	head := strings.Join(parts, sep)
	if !slices.Contains(m.selectedArgs(), skipPermsArg) {
		return head
	}
	badge := styleBadge.Render(badgeText)
	room := m.width - lipgloss.Width(badge) - len(colGap)
	return clip(head, max(room, 0)) + colGap + badge
}

func leftText(left float64, counted int) string {
	if counted == 0 {
		return naText
	}
	return fmt.Sprintf("%.1f/%d (%.0f%%)", left, counted, left/float64(counted)*fullPct)
}

// nextReset is the combined next reset (an exhausted window) or, when no
// window is exhausted, the soonest 5h reset.
func (m Model) nextReset() *time.Time {
	if m.snap.Combined.NextResetAt != nil {
		return m.snap.Combined.NextResetAt
	}
	var best *time.Time
	for _, r := range m.snap.Rows {
		if r.Usage == nil || r.Usage.FiveHour.ResetsAt == nil {
			continue
		}
		if t := r.Usage.FiveHour.ResetsAt; best == nil || t.Before(*best) {
			best = t
		}
	}
	return best
}

func (m Model) statusLine() string {
	switch m.mode {
	case modeAddName:
		return styleBold.Render("New account name: ") + m.input.View() + styleDim.Render("  ⏎ next · esc cancel")
	case modeAddEmail:
		return styleBold.Render("Email for "+m.addName+": ") + m.input.View() + styleDim.Render("  ⏎ add · esc cancel")
	}
	if m.snap.Err != nil {
		return styleBad.Render("error: " + m.snap.Err.Error())
	}
	return styleDim.Render(m.toast)
}

// footerLines packs every key hint into as few lines as the width allows.
func (m Model) footerLines() []string {
	hints := make([]string, 0, len(keyHelp))
	for _, k := range keyHelp {
		hints = append(hints, styleKey.Render(k.key)+" "+k.label)
	}
	if m.mode == modeDoctor {
		hints = []string{styleKey.Render("↑↓") + " scroll", styleKey.Render("esc") + " close"}
	}
	var lines []string
	cur := ""
	for _, h := range hints {
		next := h
		if cur != "" {
			next = cur + colGap + h
		}
		if cur != "" && lipgloss.Width(next) > m.width {
			lines, next = append(lines, cur), h
		}
		cur = next
	}
	return append(lines, cur)
}

func (m Model) helpLines() []string {
	lines := []string{
		styleHeader.Render("ccm dashboard") + sep + "every action is one key away",
		"",
		styleBold.Render("Keys"),
	}
	for _, k := range keyHelp {
		lines = append(lines, "  "+styleKey.Render(pad(k.key, 4))+k.help)
	}
	return append(lines, "",
		"Bars show how much of each plan window is used (green < 70%, yellow < 90%, red ≥ 90%).",
		bestText+" marks the account "+styleKey.Render("b")+" would pick. Press ? or esc to close.")
}

// bestName is the account `b` would launch, or "".
func (m Model) bestName() string {
	list := make([]usage.AccountUsage, len(m.snap.Rows))
	for i, r := range m.snap.Rows {
		list[i] = r.AccountUsage
	}
	best, ok := status.PickBest(list)
	if !ok {
		return ""
	}
	return best.Account
}

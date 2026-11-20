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
	autoBadge    = "⟳ auto-switch"
	sep          = " · "
	// paneChrome is the number of lines around the doctor pane (title, blank, footer).
	paneChrome = 3
)

// View renders the whole screen, never wider or taller than the window.
func (m Model) View() string {
	var body []string
	switch {
	case m.mode == modeHelp:
		body = []string{styleHeader.Render("Help") + styleDim.Render(sep+"↑↓ scroll · esc close"), "", m.pane.View()}
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
	lines := append(m.headerLines(), "")
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
	args, _ := m.argsFor(name)
	return args
}

// headerLines is the summary line plus, when there is anything to show, a
// second line with today's combined tokens and the selected account's badge.
func (m Model) headerLines() []string {
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
	var extra []string
	if t := m.todayHeader(); t != "" {
		extra = append(extra, t)
	}
	if slices.Contains(m.selectedArgs(), skipPermsArg) {
		extra = append(extra, styleBadge.Render(badgeText))
	}
	if m.snap.AutoSwitch {
		extra = append(extra, styleOK.Render(autoBadge))
	}
	lines := []string{strings.Join(parts, sep)}
	if len(extra) > 0 {
		lines = append(lines, strings.Join(extra, colGap))
	}
	return lines
}

// todayHeader is the combined today usage, or "" when none was provided.
func (m Model) todayHeader() string {
	if m.snap.TodayErr != "" {
		return "today " + styleBad.Render("err")
	}
	t := m.snap.Today
	if emptyTokens(t) {
		return ""
	}
	return "today " + status.FormatTokens(t.Input) + "/" + status.FormatTokens(t.Output) +
		" cache " + status.FormatTokens(t.CacheRead+t.CacheWrite)
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
	case modeConfirm:
		return styleWarn.Bold(true).Render(m.prompt.text)
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
	switch m.mode {
	case modeDoctor, modeHelp:
		hints = []string{styleKey.Render("↑↓") + " scroll", styleKey.Render("esc") + " close"}
	case modeConfirm:
		hints = []string{styleKey.Render("y") + " yes", styleKey.Render("n") + " no", styleKey.Render("esc") + " cancel"}
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
	return append(append(lines, ""), helpExplain...)
}

// helpExplain explains the numbers and states in plain language.
var helpExplain = []string{
	styleBold.Render("Reading the screen"),
	"  5h left 1.5/3   about 1.5 accounts' worth of 5-hour capacity is left, out of 3 accounts",
	"  7d left         the same for the weekly (7-day) limit",
	"  bars            how much of each window is used: green normally, yellow when low, red when limited",
	"  " + bestText + "               the best free account right now; " + styleKey.Render("b") + " launches it",
	"  today in/out    tokens sent / received today; cache = cached tokens",
	"",
	styleBold.Render("States"),
	"  ok          ready to use",
	"  idle        logged in, nothing used yet: no window is running (empty bar)",
	"  low         a window is 95% or more used (yellow); still usable, switch soon",
	"  limited     a window is used up (red); wait for the reset or pick another account",
	"  logged out  press l to log in",
	"  api error   usage could not be read; ccm retries after a short wait",
	"  rate limited  the usage API asked ccm to slow down; it waits 5m, 10m, 20m, then 30m",
	"  · stale 4m  numbers from the last good read, 4 minutes ago (the latest read failed)",
	"              usage is read at most every 3 minutes; r reads now unless ccm is waiting",
	"  = name      two accounts are signed in as the same login. By default the",
	"              second is merged into the first (main (= admin)); press " + styleKey.Render("A") + " to list",
	"              it as its own row, marked (= main). It is never picked as best.",
	"",
	"Press ? or esc to close.",
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

package dash

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/pratikbhadane24/multiclaude/internal/status"
	"github.com/pratikbhadane24/multiclaude/internal/usage"
)

const (
	skipPermsArg = "--dangerously-skip-permissions"
	badgeText    = "⚠ skip-permissions"
	autoBadge    = "⟳ auto-switch"
	sep          = " · "
	// paneChrome is the number of lines around the doctor pane (title, blank, footer).
	paneChrome = 3
	// cardGaugeWidth is the capacity gauge width on a wide terminal.
	cardGaugeWidth = 10
	// Capacity left under these percentages turns its gauge yellow, then red.
	halfLeftPct = 50.0
	lowLeftPct  = 20.0
)

// View renders the whole screen, never wider or taller than the window.
func (m Model) View() string {
	footer := m.footerLines()
	avail := m.height - len(footer)
	var body []string
	switch {
	case m.mode == modeHelp:
		body = []string{m.paneTitle("Help"), "", m.pane.View()}
	case m.mode == modeDoctor:
		body = []string{m.paneTitle("Doctor"), "", m.pane.View()}
	case !m.loaded:
		body = m.loadingLines(avail)
	default:
		body = m.mainLines(avail)
	}
	body = fitHeight(body, avail)
	return clip(strings.Join(append(body, footer...), "\n"), m.width)
}

func (m Model) paneTitle(name string) string {
	return m.logo() + styleDim.Render(sep) + styleHeader.Render(name) + styleDim.Render(sep+"↑↓ scroll · esc close")
}

// loadingLines centers the first-load spinner in the free space.
func (m Model) loadingLines(avail int) []string {
	text := styleAccent.Render(m.fx.spinner()) + " " + m.logo() + styleDim.Render(sep+"loading accounts…")
	if avail < 1 {
		return []string{text}
	}
	return strings.Split(lipgloss.Place(m.width, avail, lipgloss.Center, lipgloss.Center, text), "\n")
}

// mainLines stacks header, accounts and details into avail lines. When they
// do not fit it gives up, in order: the summary cards (they shrink to one
// line), detail lines, then account rows (the list scrolls with the selection).
func (m Model) mainLines(avail int) []string {
	head := m.headerLines()
	rows := len(m.snap.Rows)
	fixed := func() int { return len(head) + 1 + tableChrome + 1 }
	if fixed()+rows > avail {
		head = []string{head[0], m.summaryLine()}
	}
	lo, hi := window(rows, m.selected, max(avail-fixed(), 1))
	lines := append(slices.Clone(head), "")
	lines = append(lines, panel(m.tableTitle(lo, hi), m.tableWindow(lo, hi), m.width)...)
	if room := avail - fixed() - (hi - lo) - panelBorders; room > 0 {
		details := m.detailLines()
		lines = append(lines, panel(styleHeader.Render("Details"), details[:min(room, len(details))], m.width)...)
	}
	return append(lines, m.statusLine())
}

// tableTitle names the accounts panel and, when the list is scrolled, says
// which rows are showing.
func (m Model) tableTitle(lo, hi int) string {
	title := styleHeader.Render("Accounts")
	if n := len(m.snap.Rows); hi-lo < n {
		title += styleDim.Render(fmt.Sprintf(" %d–%d of %d", lo+1, hi, n))
	}
	return title
}

// fitHeight drops lines above the status line to fit h.
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

// logo is the product name; its colors cycle while the dashboard is busy.
func (m Model) logo() string {
	var b strings.Builder
	for i, r := range "ccm" {
		c := logoRamp[(i+m.fx.frame/spinnerEvery)%len(logoRamp)]
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(c).Render(string(r)))
	}
	return b.String()
}

// headerLines is the title line (name, account count, badges, activity)
// and, below a blank line, the summary cards.
func (m Model) headerLines() []string {
	parts := []string{styleAccent.Render("◆ ") + m.logo(), fmt.Sprintf("%d accounts", len(m.snap.Rows))}
	if slices.Contains(m.selectedArgs(), skipPermsArg) {
		parts = append(parts, styleBadge.Render(badgeText))
	}
	if m.snap.AutoSwitch {
		parts = append(parts, styleOK.Render(autoBadge))
	}
	title := spread(strings.Join(parts, styleDim.Render(sep)), m.activity(), m.width)
	return append([]string{title, ""}, cardLines(m.cards())...)
}

// activity is the right end of the title line: a spinner while usage is
// being read, else a quiet "live" mark.
func (m Model) activity() string {
	if m.loading {
		return styleAccent.Render(m.fx.spinner()) + styleDim.Render(" refreshing…")
	}
	return styleOK.Render("●") + styleDim.Render(" live")
}

// gaugeWidth is the width of the capacity gauges in the cards: none on a
// terminal too narrow to keep every card whole.
func (m Model) gaugeWidth() int {
	switch {
	case m.width >= wideWidth:
		return cardGaugeWidth
	case m.width >= defaultWidth:
		return narrowBarWidth
	}
	return 0
}

// cards are the summary figures: capacity left in each window, the next
// reset and, when there is any, today's combined tokens.
func (m Model) cards() []card {
	c := m.snap.Combined
	out := []card{
		{"5h left", m.leftValue(leftGauge(gaugeLeft5h, c.FiveHourLeft, c.Counted), c.FiveHourLeft, c.Counted)},
		{"7d left", m.leftValue(leftGauge(gaugeLeft7d, c.SevenDayLeft, c.Counted), c.SevenDayLeft, c.Counted)},
	}
	if t := m.nextReset(); t != nil {
		out = append(out, card{"next reset", styleBold.Render("in " + status.FormatDuration(t.Sub(m.deps.Now())))})
	}
	if t := m.todayValue(); t != "" {
		out = append(out, card{"today", t})
	}
	return out
}

// summaryLine is the cards on one line, for a window too short for them.
func (m Model) summaryLine() string {
	c := m.snap.Combined
	parts := []string{
		styleDim.Render("5h left ") + styleBold.Render(leftText(c.FiveHourLeft, c.Counted)),
		styleDim.Render("7d left ") + styleBold.Render(leftText(c.SevenDayLeft, c.Counted)),
	}
	if t := m.nextReset(); t != nil {
		parts = append(parts, styleDim.Render("next reset ")+styleBold.Render("in "+status.FormatDuration(t.Sub(m.deps.Now()))))
	}
	if t := m.todayValue(); t != "" {
		parts = append(parts, styleDim.Render("today ")+t)
	}
	return strings.Join(parts, styleDim.Render(sep))
}

// leftValue is a capacity card's value: a gauge of what is left (green,
// yellow when under half, red when nearly gone) and the figure.
func (m Model) leftValue(g gauge, left float64, counted int) string {
	text := styleBold.Render(leftText(left, counted))
	w := m.gaugeWidth()
	if counted == 0 || w == 0 {
		return text
	}
	color := colorOK
	switch {
	case g.target < lowLeftPct:
		color = colorBad
	case g.target < halfLeftPct:
		color = colorWarn
	}
	return meter(m.fx.value(g), w, solid(color), plain) + " " + text
}

// todayValue is the combined today usage, or "" when none was provided.
func (m Model) todayValue() string {
	if m.snap.TodayErr != "" {
		return styleBad.Render("err")
	}
	t := m.snap.Today
	if emptyTokens(t) {
		return ""
	}
	return styleBold.Render(status.FormatTokens(t.Input)+"/"+status.FormatTokens(t.Output)) +
		styleDim.Render(" cache "+status.FormatTokens(t.CacheRead+t.CacheWrite))
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
	if len(m.running) > 0 {
		return styleAccent.Render(m.fx.spinner()) + " " + styleDim.Render(m.toast)
	}
	return styleDim.Render(m.toast)
}

// footerLines packs every key hint into as few lines as the width allows.
func (m Model) footerLines() []string {
	hints := make([]string, 0, len(keyHelp))
	for _, k := range keyHelp {
		hints = append(hints, styleKey.Render(k.key)+" "+styleDim.Render(k.label))
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
	best, _, ok := m.bestScored()
	if !ok {
		return ""
	}
	return best.Account
}

// bestScored is the account `b` would launch and why.
func (m Model) bestScored() (usage.AccountUsage, status.Score, bool) {
	list := make([]usage.AccountUsage, len(m.snap.Rows))
	for i, r := range m.snap.Rows {
		list[i] = r.AccountUsage
	}
	return status.PickBestScored(list, m.deps.Now())
}

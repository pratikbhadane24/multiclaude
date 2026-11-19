package status

import (
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"multiclaude/internal/tokens"
	"multiclaude/internal/usage"
)

const (
	tabPad     = 2
	headerHead = "ACCOUNT\tEMAIL\tPLAN\t5H USED\t5H LEFT\t5H RESET\t7D USED\t7D LEFT\t7D RESET"
	modelCols  = "\t7D OPUS\t7D SONNET"
	headerTail = "\tTODAY TOK\tSTATE"
)

func hasModelWindows(rows []Row) bool {
	for _, r := range rows {
		if r.Usage != nil && (r.Usage.SevenDayOpus != nil || r.Usage.SevenDaySonnet != nil) {
			return true
		}
	}
	return false
}

func modelPct(w *usage.Window) string {
	if w == nil {
		return ""
	}
	return fmtPct(w.UsedPct)
}

func todayTotal(t tokens.Tokens) int64 { return t.Input + t.Output + t.CacheRead + t.CacheWrite }

func rowLine(r Row, now time.Time, withModels bool) string {
	five, seven := Window5h(r, now), Window7d(r, now)
	cols := []string{
		r.DisplayName(), r.Email, r.Plan,
		five.Used, five.Left, five.Reset,
		seven.Used, seven.Left, seven.Reset,
	}
	if withModels {
		var opus, sonnet *usage.Window
		if r.Usage != nil {
			opus, sonnet = r.Usage.SevenDayOpus, r.Usage.SevenDaySonnet
		}
		cols = append(cols, modelPct(opus), modelPct(sonnet))
	}
	// Per-account token totals are meaningless: all accounts share one history.
	cols = append(cols, "-", StateLabel(r))
	for i, c := range cols {
		if c == "" {
			cols[i] = "-"
		}
	}
	return strings.Join(cols, "\t")
}

func allLine(c usage.Combined, now time.Time) string {
	pct := func(left float64) string {
		if c.Counted == 0 {
			return naText
		}
		return fmt.Sprintf("%.0f%%", left/float64(c.Counted)*fullPct)
	}
	parts := []string{
		fmt.Sprintf("5h left %.1f/%d acct (%s)", c.FiveHourLeft, c.Counted, pct(c.FiveHourLeft)),
		fmt.Sprintf("7d left %.1f/%d (%s)", c.SevenDayLeft, c.Counted, pct(c.SevenDayLeft)),
		fmt.Sprintf("available now %d", c.AvailableNow),
	}
	if c.NextResetAt != nil {
		parts = append(parts, "next reset in "+fmtDuration(c.NextResetAt.Sub(now)))
	}
	if len(c.Excluded) > 0 {
		parts = append(parts, "excluded: "+strings.Join(c.Excluded, ", "))
	}
	if c.Today != (tokens.Tokens{}) {
		parts = append(parts, fmt.Sprintf("today %s/%s in/out, %s cache", humanize(c.Today.Input), humanize(c.Today.Output), humanize(c.Today.CacheRead+c.Today.CacheWrite)))
	}
	return "ALL  " + strings.Join(parts, " · ")
}

// Render builds the status table, the ALL line and any link warnings.
func Render(rows []Row, c usage.Combined, now time.Time) string {
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 0, tabPad, ' ', 0)
	withModels := hasModelWindows(rows)
	head := headerHead
	if withModels {
		head += modelCols
	}
	fmt.Fprintln(tw, head+headerTail)
	for _, r := range rows {
		fmt.Fprintln(tw, rowLine(r, now, withModels))
	}
	_ = tw.Flush()
	b.WriteString(allLine(c, now) + "\n")
	if c.TodayErr != "" {
		b.WriteString("⚠ token scan failed\n")
	}
	for _, r := range rows {
		if r.LinkErr != "" {
			fmt.Fprintf(&b, "⚠ %s: link check failed — run ccm doctor\n", r.Account)
		}
		for _, item := range r.LinkWarnings {
			fmt.Fprintf(&b, "⚠ %s: %s is no longer shared — run ccm doctor\n", r.Account, item)
		}
	}
	return b.String()
}

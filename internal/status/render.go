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
	tabPad = 2
	header = "ACCOUNT\tEMAIL\tPLAN\t5H USED\t5H LEFT\t5H RESET\t7D USED\t7D LEFT\t7D RESET\tTODAY TOK\tSTATE"
)

func todayTotal(t tokens.Tokens) int64 { return t.Input + t.Output + t.CacheRead + t.CacheWrite }

func stateText(r Row) string {
	switch r.State {
	case usage.StateOK:
		return "ok"
	case usage.StateExpiredRefreshed:
		return "ok (token refreshed)"
	case usage.StateLoggedOut:
		return "logged out → ccm login " + r.Account
	}
	if r.Err != "" {
		return "api error: " + r.Err
	}
	return "api error"
}

func rowLine(r Row, now time.Time) string {
	var five, seven usage.Window
	if r.Usage != nil {
		five, seven = r.Usage.FiveHour, r.Usage.SevenDay
	}
	cols := []string{
		r.Account, r.Email, r.Plan,
		fmtPct(five.UsedPct), fmtLeft(five.UsedPct), fmtReset(five.ResetsAt, now),
		fmtPct(seven.UsedPct), fmtLeft(seven.UsedPct), fmtReset(seven.ResetsAt, now),
		humanize(todayTotal(r.Today)), stateText(r),
	}
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
	return "ALL  " + strings.Join(parts, " · ")
}

// Render builds the status table, the ALL line and any link warnings.
func Render(rows []Row, c usage.Combined, now time.Time) string {
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 0, tabPad, ' ', 0)
	fmt.Fprintln(tw, header)
	for _, r := range rows {
		fmt.Fprintln(tw, rowLine(r, now))
	}
	_ = tw.Flush()
	b.WriteString(allLine(c, now) + "\n")
	for _, r := range rows {
		for _, item := range r.LinkWarnings {
			fmt.Fprintf(&b, "⚠ %s: %s is no longer shared — run ccm doctor\n", r.Account, item)
		}
	}
	return b.String()
}

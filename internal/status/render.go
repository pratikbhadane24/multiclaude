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

// limitedWindow returns "5h" or "7d" when that window is fully used.
func limitedWindow(r Row) string {
	if r.State != usage.StateOK && r.State != usage.StateExpiredRefreshed || r.Usage == nil {
		return ""
	}
	if p := r.Usage.FiveHour.UsedPct; p != nil && *p >= fullPct {
		return "5h"
	}
	if p := r.Usage.SevenDay.UsedPct; p != nil && *p >= fullPct {
		return "7d"
	}
	return ""
}

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

func stateText(r Row) string {
	if lim := limitedWindow(r); lim != "" {
		return "limited (" + lim + ")"
	}
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

func todayText(r Row) string {
	if r.TodayErr != "" {
		return "err"
	}
	return humanize(todayTotal(r.Today))
}

func rowLine(r Row, now time.Time, withModels bool) string {
	var five, seven usage.Window
	if r.Usage != nil {
		five, seven = r.Usage.FiveHour, r.Usage.SevenDay
	}
	cols := []string{
		r.Account, r.Email, r.Plan,
		fmtPct(five.UsedPct), fmtLeft(five.UsedPct), fmtReset(five.ResetsAt, now),
		fmtPct(seven.UsedPct), fmtLeft(seven.UsedPct), fmtReset(seven.ResetsAt, now),
	}
	if withModels {
		var opus, sonnet *usage.Window
		if r.Usage != nil {
			opus, sonnet = r.Usage.SevenDayOpus, r.Usage.SevenDaySonnet
		}
		cols = append(cols, modelPct(opus), modelPct(sonnet))
	}
	cols = append(cols, todayText(r), stateText(r))
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
	for _, r := range rows {
		if r.TodayErr != "" {
			fmt.Fprintf(&b, "⚠ %s: token scan failed\n", r.Account)
		}
		if r.LinkErr != "" {
			fmt.Fprintf(&b, "⚠ %s: link check failed — run ccm doctor\n", r.Account)
		}
		for _, item := range r.LinkWarnings {
			fmt.Fprintf(&b, "⚠ %s: %s is no longer shared — run ccm doctor\n", r.Account, item)
		}
	}
	return b.String()
}

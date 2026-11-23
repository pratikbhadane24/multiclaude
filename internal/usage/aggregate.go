package usage

import (
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/tokens"
)

const fullPct = 100.0

// Combined is the cross-account total.
type Combined struct {
	FiveHourLeft, SevenDayLeft float64
	Counted                    int
	Excluded                   []string
	AvailableNow               int
	NextResetAt                *time.Time
	// Today is today's token use. Conversation history is shared by every
	// account, so it is counted once here, not per account. Set by status.Collect.
	Today    tokens.Tokens
	TodayErr string
}

func countable(s State) bool { return s == StateOK || s == StateExpiredRefreshed }

func leftFraction(used float64) float64 {
	if used >= fullPct {
		return 0
	}
	return (fullPct - used) / fullPct
}

// soonest returns the earlier of cur and w's reset when w is exhausted.
func soonest(cur *time.Time, w Window) *time.Time {
	if w.UsedPct == nil || *w.UsedPct < fullPct || w.ResetsAt == nil {
		return cur
	}
	if cur == nil || w.ResetsAt.Before(*cur) {
		t := *w.ResetsAt
		return &t
	}
	return cur
}

// Aggregate combines per-account usage without mutating its input. A nil
// window on a countable account is idle: fully free. Accounts signed in as the
// same login are counted once (the first in list order).
func Aggregate(list []AccountUsage) Combined {
	out := Combined{Excluded: []string{}}
	for _, a := range Distinct(list) {
		if !countable(a.State) || a.Usage == nil {
			out.Excluded = append(out.Excluded, a.Account)
			continue
		}
		five, seven := a.Usage.FiveHour, a.Usage.SevenDay
		out.FiveHourLeft += windowLeft(five)
		out.SevenDayLeft += windowLeft(seven)
		out.Counted++
		if !exhausted(five) && !exhausted(seven) {
			out.AvailableNow++
		}
		out.NextResetAt = soonest(soonest(out.NextResetAt, five), seven)
	}
	return out
}

// windowLeft is the free fraction; an idle (nil) window is fully free.
func windowLeft(w Window) float64 {
	if w.UsedPct == nil {
		return 1
	}
	return leftFraction(*w.UsedPct)
}

func exhausted(w Window) bool { return w.UsedPct != nil && *w.UsedPct >= fullPct }

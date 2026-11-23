package status

import "github.com/pratikbhadane24/multiclaude/internal/usage"

const fullPct = 100.0

// headroom returns the smaller remaining percentage across windows (an idle,
// nil window is fully free); ok is false when any window is exhausted.
func headroom(u *usage.Usage) (left float64, ok bool) {
	left = fullPct
	for _, w := range []usage.Window{u.FiveHour, u.SevenDay} {
		if w.UsedPct == nil {
			continue // idle: fully free
		}
		l := fullPct - *w.UsedPct
		if l <= 0 {
			return 0, false
		}
		if l < left {
			left = l
		}
	}
	return left, true
}

// PickBest returns the usable account with the most headroom (ties: first).
// A duplicate of an earlier login is never returned.
func PickBest(list []usage.AccountUsage) (usage.AccountUsage, bool) {
	var best usage.AccountUsage
	bestLeft, found := 0.0, false
	for _, a := range usage.Distinct(list) {
		if a.Usage == nil || (a.State != usage.StateOK && a.State != usage.StateExpiredRefreshed) {
			continue
		}
		left, ok := headroom(a.Usage)
		if ok && (!found || left > bestLeft) {
			best, bestLeft, found = a, left, true
		}
	}
	return best, found
}

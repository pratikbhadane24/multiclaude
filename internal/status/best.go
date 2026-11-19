package status

import "multiclaude/internal/usage"

const fullPct = 100.0

// headroom returns the smaller remaining percentage across known windows;
// ok is false when no window is known or any known window is exhausted.
func headroom(u *usage.Usage) (left float64, ok bool) {
	left = fullPct
	known := false
	for _, w := range []usage.Window{u.FiveHour, u.SevenDay} {
		if w.UsedPct == nil {
			continue
		}
		known = true
		l := fullPct - *w.UsedPct
		if l <= 0 {
			return 0, false
		}
		if l < left {
			left = l
		}
	}
	return left, known
}

// PickBest returns the usable account with the most headroom (ties: first).
func PickBest(list []usage.AccountUsage) (usage.AccountUsage, bool) {
	var best usage.AccountUsage
	bestLeft, found := 0.0, false
	for _, a := range list {
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

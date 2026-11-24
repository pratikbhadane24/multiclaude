package status

import (
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/usage"
)

const fullPct = 100.0

// usableState reports whether a fetched account can be run on.
func usableState(a usage.AccountUsage) bool {
	return a.Usage != nil && (a.State == usage.StateOK || a.State == usage.StateExpiredRefreshed)
}

// PickBest returns the usable account that is best to run on at now (see
// Score; ties: first). A duplicate of an earlier login is never returned.
func PickBest(list []usage.AccountUsage, now time.Time) (usage.AccountUsage, bool) {
	best, _, found := PickBestScored(list, now)
	return best, found
}

// PickBestScored is PickBest that also returns the winning score.
func PickBestScored(list []usage.AccountUsage, now time.Time) (usage.AccountUsage, Score, bool) {
	var best usage.AccountUsage
	var bestScore Score
	found := false
	for _, a := range usage.Distinct(list) {
		if !usableState(a) {
			continue
		}
		s, ok := ScoreUsage(a.Usage, now)
		if ok && (!found || s.Better(bestScore)) {
			best, bestScore, found = a, s, true
		}
	}
	return best, bestScore, found
}

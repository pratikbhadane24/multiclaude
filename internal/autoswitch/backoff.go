package autoswitch

import "time"

const (
	// quickRehit is how soon after a reset-wait relaunch a new limit hit on
	// the same account counts as "the reset did not really happen".
	quickRehit = 10 * time.Minute
	// firstBackoff and maxBackoff bound the doubling wait after quick re-hits.
	firstBackoff = 15 * time.Minute
	maxBackoff   = time.Hour
	// minResetRelaunchGap is the least time between a reset-wait relaunch and
	// the next relaunch.
	minResetRelaunchGap = 5 * time.Minute
)

// resetGuard spaces relaunches after a reset wait, so a reset that did not
// free the account (stale or wrong reset time) cannot cause a relaunch loop.
type resetGuard struct {
	// acct and at are the account and time of the last reset-wait relaunch
	// (zero at: none yet).
	acct    string
	at      time.Time
	backoff time.Duration
}

// relaunched returns g after a reset-wait relaunch of acct at now.
func (g resetGuard) relaunched(acct string, now time.Time) resetGuard {
	g.acct, g.at = acct, now
	return g
}

// hit returns g after a limit hit on acct at now; quick is true when acct is
// the reset account hitting again within quickRehit, which doubles the
// backoff (firstBackoff .. maxBackoff). Any other hit clears the backoff.
func (g resetGuard) hit(acct string, now time.Time) (resetGuard, bool) {
	if g.at.IsZero() {
		return g, false
	}
	if acct != g.acct || now.Sub(g.at) >= quickRehit {
		g.backoff = 0
		return g, false
	}
	g.backoff = min(max(2*g.backoff, firstBackoff), maxBackoff)
	return g, true
}

// floor is the least wait at now before the next relaunch.
func (g resetGuard) floor(now time.Time) time.Duration {
	if g.at.IsZero() {
		return 0
	}
	return max(g.backoff, minResetRelaunchGap-now.Sub(g.at), 0)
}

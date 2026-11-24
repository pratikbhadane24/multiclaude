package autoswitch

import (
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/paths"
	"github.com/pratikbhadane24/multiclaude/internal/registry"
	"github.com/pratikbhadane24/multiclaude/internal/status"
	"github.com/pratikbhadane24/multiclaude/internal/usage"
)

const fullPct = 100.0

// cachedList is every registered account with its cached usage (registry
// order). Errors (unreadable registry or cache) yield nothing.
func cachedList(p paths.Paths, now time.Time) ([]registry.Account, []usage.AccountUsage) {
	accts, err := registry.Load(p)
	if err != nil {
		return nil, nil
	}
	names := make([]string, len(accts))
	for i, a := range accts {
		names[i] = a.Name
	}
	list, err := status.CachedUsage(p, names, now)
	if err != nil {
		return nil, nil
	}
	return accts, list
}

// excludedLogins expands exclude to every account of the same login, so
// excluding main also excludes an admin signed in as the same user.
func excludedLogins(list []usage.AccountUsage, exclude []string) map[string]bool {
	canon := usage.CanonicalIndex(list)
	excludedCanon := map[int]bool{}
	out := map[string]bool{}
	for _, name := range exclude {
		out[name] = true
	}
	for i, u := range list {
		if out[u.Account] {
			excludedCanon[canon[i]] = true
		}
	}
	for i, u := range list {
		if excludedCanon[canon[i]] {
			out[u.Account] = true
		}
	}
	return out
}

// PickNext chooses the account with the most headroom from the usage cache
// only (no request, no keychain), skipping exclude and every alias of an
// excluded login, and accounts that are limited, logged out or unknown.
func PickNext(p paths.Paths, exclude []string, now time.Time) (registry.Account, bool) {
	accts, list := cachedList(p, now)
	skip := excludedLogins(list, exclude)
	candidates := make([]usage.AccountUsage, 0, len(list))
	for _, u := range list {
		if !skip[u.Account] {
			candidates = append(candidates, u)
		}
	}
	best, ok := status.PickBest(candidates, now)
	if !ok {
		return registry.Account{}, false
	}
	return registry.Find(accts, best.Account)
}

// NextReset returns the account that becomes usable soonest according to the
// cached reset times, and when. An account counts only when every exhausted
// window has a known reset; one already past its reset is usable at now.
func NextReset(p paths.Paths, now time.Time) (registry.Account, time.Time, bool) {
	accts, list := cachedList(p, now)
	var bestName string
	var bestAt time.Time
	for _, u := range usage.Distinct(list) {
		at, ok := usableAt(u, now)
		if ok && (bestName == "" || at.Before(bestAt)) {
			bestName, bestAt = u.Account, at
		}
	}
	if bestName == "" {
		return registry.Account{}, time.Time{}, false
	}
	acct, ok := registry.Find(accts, bestName)
	return acct, bestAt, ok
}

// usableAt is the latest reset of u's exhausted windows (now when none is).
func usableAt(u usage.AccountUsage, now time.Time) (time.Time, bool) {
	if u.Usage == nil || (u.State != usage.StateOK && u.State != usage.StateExpiredRefreshed) {
		return time.Time{}, false
	}
	at := now
	for _, w := range []usage.Window{u.Usage.FiveHour, u.Usage.SevenDay} {
		if w.UsedPct == nil || *w.UsedPct < fullPct {
			continue
		}
		if w.ResetsAt == nil {
			return time.Time{}, false
		}
		if w.ResetsAt.After(at) {
			at = *w.ResetsAt
		}
	}
	return at, true
}

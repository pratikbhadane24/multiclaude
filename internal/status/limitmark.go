package status

import (
	"errors"
	"os"
	"strings"
	"time"

	"multiclaude/internal/paths"
	"multiclaude/internal/usage"
)

// CachedUsage returns the cached usage of each named account, in order,
// without any request: last good data (stale or not), or an api-error row
// when the cache has nothing usable for it. A missing cache yields error rows.
func CachedUsage(p paths.Paths, names []string, now time.Time) ([]usage.AccountUsage, error) {
	cache, err := ReadCache(p)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	byName := indexByName(cache.Accounts)
	out := make([]usage.AccountUsage, len(names))
	for i, name := range names {
		e, ok := byName[name]
		if !ok {
			out[i] = usage.AccountUsage{Account: name, State: usage.StateAPIError}
			continue
		}
		out[i] = fromCacheEntry(e, now)
		if e.State == string(usage.StateLoggedOut) {
			out[i].State = usage.StateLoggedOut
		}
	}
	return out, nil
}

// LimitMark records that an account just hit a usage limit.
type LimitMark struct {
	Name string
	// Weekly marks the 7-day window, else the 5-hour one.
	Weekly bool
	// ResetsAt replaces the window's cached reset time when set.
	ResetsAt *time.Time
}

// MarkLimited records m in the usage cache under the cache lock: the hit
// window becomes fully used and the state limited, for the account and every
// cached entry of the same login. Other fields and fetch state are kept, so
// the next real fetch replaces the mark with fresh data.
func MarkLimited(p paths.Paths, m LimitMark) error {
	return updateCache(p, func(old Cache) Cache {
		next := old
		next.Accounts = make([]CacheAccount, 0, len(old.Accounts)+1)
		target, found := indexByName(old.Accounts)[m.Name]
		for _, a := range old.Accounts {
			if a.Name == m.Name || (found && sameCachedLogin(a, target)) {
				a = markedEntry(a, m)
			}
			next.Accounts = append(next.Accounts, a)
		}
		if !found {
			next.Accounts = append(next.Accounts, markedEntry(CacheAccount{Name: m.Name}, m))
		}
		return next
	})
}

// sameCachedLogin reports whether two cache entries are the same login.
func sameCachedLogin(a, b CacheAccount) bool {
	if a.LoginID != "" && b.LoginID != "" {
		return a.LoginID == b.LoginID
	}
	return a.Email != "" && strings.EqualFold(a.Email, b.Email)
}

func markedEntry(e CacheAccount, m LimitMark) CacheAccount {
	out := e
	full := fullPct
	if m.Weekly {
		out.SevenDayUsed = &full
		if m.ResetsAt != nil {
			out.SevenDayResetsAt = m.ResetsAt
		}
	} else {
		out.FiveHourUsed = &full
		if m.ResetsAt != nil {
			out.FiveHourResetsAt = m.ResetsAt
		}
	}
	out.State = StateLimited
	return out
}

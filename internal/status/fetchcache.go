package status

import (
	"context"
	"sync"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/paths"
	"github.com/pratikbhadane24/multiclaude/internal/registry"
	"github.com/pratikbhadane24/multiclaude/internal/usage"
)

const (
	// MinFetchInterval is the least time between two usage requests for one
	// account, across every ccm process; sooner callers get the cached result.
	MinFetchInterval = 3 * time.Minute
	// ErrorBackoff is the wait after a failed fetch other than a 429.
	ErrorBackoff = 2 * time.Minute
)

// RateLimitBackoff is the wait after consecutive 429s; the last step repeats.
// A longer Retry-After from the server wins.
var RateLimitBackoff = []time.Duration{5 * time.Minute, 10 * time.Minute, 20 * time.Minute, 30 * time.Minute}

// FetchFunc fetches one account's usage.
type FetchFunc func(ctx context.Context, a registry.Account) usage.AccountUsage

// CachedFetch wraps a usage fetch with the persistent fetch state in the
// usage cache: recent results are reused, failures back off and keep the last
// good usage. Every status.Collect caller fetches through it.
type CachedFetch struct {
	paths paths.Paths
	fetch FetchFunc
	now   func() time.Time
	// force skips MinFetchInterval (never the backoff).
	force bool

	mu  sync.Mutex
	err error
}

// NewCachedFetch builds the wrapper; force skips MinFetchInterval only.
func NewCachedFetch(p paths.Paths, fetch FetchFunc, now func() time.Time, force bool) *CachedFetch {
	return &CachedFetch{paths: p, fetch: fetch, now: now, force: force}
}

// Err is the first failure to persist fetch state, if any.
func (c *CachedFetch) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *CachedFetch) setErr(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err == nil {
		c.err = err
	}
}

// Fetch returns a's usage, from the cache when it is recent or the account is
// backing off, else from the API (recording the outcome). The written entry
// is built under the cache lock from the entry current at write time, so a
// limit mark landing during the request survives (see mergeFetch).
func (c *CachedFetch) Fetch(ctx context.Context, a registry.Account) usage.AccountUsage {
	started := c.now()
	prev, has := c.load(a.Name)
	if has && c.servable(prev, started) {
		return fromCacheEntry(prev, started)
	}
	res := c.fetch(ctx, a)
	var next CacheAccount
	err := updateCache(c.paths, func(old Cache) Cache {
		next = mergeFetch(indexByName(old.Accounts)[a.Name], a.Name, res, started)
		return withEntry(old, next)
	})
	if err != nil {
		c.setErr(err)
		next = mergeFetch(prev, a.Name, res, started)
	}
	if res.State == usage.StateAPIError || markActive(next, started) {
		return fromCacheEntry(next, started)
	}
	return res
}

func (c *CachedFetch) load(name string) (CacheAccount, bool) {
	cache, err := ReadCache(c.paths)
	if err != nil {
		return CacheAccount{}, false
	}
	e, ok := indexByName(cache.Accounts)[name]
	return e, ok
}

// servable: in backoff (always), or fetched successfully within
// MinFetchInterval with no failure since (unless forced).
func (c *CachedFetch) servable(e CacheAccount, now time.Time) bool {
	if e.BackoffUntil != nil && now.Before(*e.BackoffUntil) {
		return true
	}
	if c.force || !e.hasLastGood() || e.LastError != "" {
		return false
	}
	age := now.Sub(*e.FetchedAt)
	return age >= 0 && age < MinFetchInterval
}

// backoffFor is the wait after a failed fetch and the next 429 step.
func backoffFor(step int, res usage.AccountUsage) (time.Duration, int) {
	if !res.RateLimited {
		return ErrorBackoff, step
	}
	wait := RateLimitBackoff[min(step, len(RateLimitBackoff)-1)]
	return max(wait, res.RetryAfter), step + 1
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// nextCacheEntry is prev updated with one fetch result. A failure keeps the
// last good usage and backs off; logged-out clears the entry.
func nextCacheEntry(prev CacheAccount, name string, res usage.AccountUsage, now time.Time) CacheAccount {
	email := firstNonEmpty(res.Email, prev.Email)
	switch res.State {
	case usage.StateLoggedOut:
		return CacheAccount{Name: name, Email: email, State: string(usage.StateLoggedOut)}
	case usage.StateOK, usage.StateExpiredRefreshed:
		e := toCacheAccount(Row{AccountUsage: res})
		at := now.UTC()
		e.Name, e.Email, e.FetchedAt = name, email, &at
		return e
	}
	e := prev
	wait, step := backoffFor(prev.BackoffStep, res)
	until := now.Add(wait).UTC()
	e.Name, e.Email, e.Stale = name, email, prev.hasLastGood()
	e.LastError, e.RateLimited, e.BackoffUntil, e.BackoffStep = res.Err, res.RateLimited, &until, step
	if !prev.hasLastGood() {
		e.State = string(usage.StateAPIError)
		e.Plan, e.LoginID = firstNonEmpty(res.Plan, prev.Plan), firstNonEmpty(res.AccountUUID, prev.LoginID)
	}
	return e
}

// withEntry returns c with e replacing the entry of the same name (or added).
func withEntry(c Cache, e CacheAccount) Cache {
	out := c
	out.Accounts = make([]CacheAccount, 0, len(c.Accounts)+1)
	found := false
	for _, a := range c.Accounts {
		if a.Name == e.Name {
			a, found = e, true
		}
		out.Accounts = append(out.Accounts, a)
	}
	if !found {
		out.Accounts = append(out.Accounts, e)
	}
	return out
}

// fromCacheEntry turns an entry back into a fetch result: last good usage
// (marked stale after a failure), or the error when there is none.
func fromCacheEntry(e CacheAccount, now time.Time) usage.AccountUsage {
	au := usage.AccountUsage{Account: e.Name, Email: e.Email, Plan: e.Plan, AccountUUID: e.LoginID,
		Err: e.LastError, RateLimited: e.RateLimited}
	if e.BackoffUntil != nil && now.Before(*e.BackoffUntil) {
		at := *e.BackoffUntil
		au.RetryAt, au.RetryIn = &at, at.Sub(now)
	}
	if !e.hasLastGood() {
		au.State = usage.StateAPIError
		return au
	}
	// Windows that reset since the fetch are idle, not their old percentage;
	// with none of the old numbers left the row is no longer stale.
	u := cachedUsage(e).WithoutElapsed(now)
	au.State, au.Usage = usage.StateOK, &u
	if e.LastError != "" && u.HasWindowData() {
		au.Stale, au.StaleFor = true, max(now.Sub(*e.FetchedAt), 0)
	}
	return au
}

func cachedUsage(e CacheAccount) *usage.Usage {
	return &usage.Usage{
		FiveHour:       usage.Window{UsedPct: e.FiveHourUsed, ResetsAt: e.FiveHourResetsAt},
		SevenDay:       usage.Window{UsedPct: e.SevenDayUsed, ResetsAt: e.SevenDayResetsAt},
		SevenDayOpus:   fromCacheWindow(e.SevenDayOpus),
		SevenDaySonnet: fromCacheWindow(e.SevenDaySonnet),
	}
}

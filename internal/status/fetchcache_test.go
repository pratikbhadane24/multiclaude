package status

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"multiclaude/internal/paths"
	"multiclaude/internal/registry"
	"multiclaude/internal/usage"
)

// fakeAPI is an injected usage fetch whose next answer the test sets.
type fakeAPI struct {
	mu    sync.Mutex
	calls map[string]int
	next  func(name string) usage.AccountUsage
}

func newFakeAPI() *fakeAPI {
	api := &fakeAPI{calls: map[string]int{}}
	api.next = func(name string) usage.AccountUsage { return okUsage(name, 40) }
	return api
}

func (f *fakeAPI) fetch(_ context.Context, a registry.Account) usage.AccountUsage {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[a.Name]++
	return f.next(a.Name)
}

func (f *fakeAPI) count(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[name]
}

func okUsage(name string, five float64) usage.AccountUsage {
	return usage.AccountUsage{Account: name, Email: name + "@x.io", Plan: "max", State: usage.StateOK, AccountUUID: "uuid-" + name,
		Usage: &usage.Usage{FiveHour: usage.Window{UsedPct: &five}, SevenDay: usage.Window{UsedPct: fp(3)},
			SevenDayOpus: &usage.Window{UsedPct: fp(9)}}}
}

func rateLimited(name string, retryAfter time.Duration) usage.AccountUsage {
	return usage.AccountUsage{Account: name, Plan: "max", State: usage.StateAPIError, Err: "rate limited",
		RateLimited: true, RetryAfter: retryAfter}
}

type cacheClock struct{ t time.Time }

func (c *cacheClock) now() time.Time          { return c.t }
func (c *cacheClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func cacheEnv(t *testing.T) (paths.Paths, *fakeAPI, *cacheClock) {
	t.Helper()
	return paths.Paths{AccountsRoot: filepath.Join(t.TempDir(), "accts")}, newFakeAPI(), &cacheClock{t: now}
}

func fetchOnce(p paths.Paths, api *fakeAPI, clock *cacheClock, force bool, name string) (usage.AccountUsage, error) {
	cf := NewCachedFetch(p, api.fetch, clock.now, force)
	res := cf.Fetch(context.Background(), registry.Account{Name: name})
	return res, cf.Err()
}

func mustFetch(t *testing.T, p paths.Paths, api *fakeAPI, clock *cacheClock, force bool, name string) usage.AccountUsage {
	t.Helper()
	res, err := fetchOnce(p, api, clock, force, name)
	if err != nil {
		t.Fatalf("cache write: %v", err)
	}
	return res
}

func cachedEntry(t *testing.T, p paths.Paths, name string) CacheAccount {
	t.Helper()
	c, err := ReadCache(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range c.Accounts {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("no cache entry for %s in %+v", name, c.Accounts)
	return CacheAccount{}
}

func TestCachedFetchServesRecentSuccessWithoutHTTP(t *testing.T) {
	p, api, clock := cacheEnv(t)

	first := mustFetch(t, p, api, clock, false, "work")
	clock.advance(MinFetchInterval - time.Second)
	cached := mustFetch(t, p, api, clock, false, "work")
	clock.advance(time.Second)
	mustFetch(t, p, api, clock, false, "work")

	if api.count("work") != 2 {
		t.Fatalf("http calls=%d want 2 (cached in between)", api.count("work"))
	}
	if first.Stale || cached.Stale || cached.State != usage.StateOK || cached.Usage == nil || *cached.Usage.FiveHour.UsedPct != 40 {
		t.Fatalf("cached=%+v", cached)
	}
	if cached.Usage.SevenDayOpus == nil || *cached.Usage.SevenDayOpus.UsedPct != 9 || cached.AccountUUID != "uuid-work" || cached.Email != "work@x.io" {
		t.Fatalf("cached result lost fields: %+v", cached)
	}
}

func TestCachedFetchForceSkipsMinInterval(t *testing.T) {
	p, api, clock := cacheEnv(t)

	mustFetch(t, p, api, clock, false, "work")
	clock.advance(time.Minute)
	mustFetch(t, p, api, clock, true, "work")

	if api.count("work") != 2 {
		t.Fatalf("http calls=%d want 2", api.count("work"))
	}
}

func TestCachedFetch429KeepsLastGoodAndBacksOff(t *testing.T) {
	p, api, clock := cacheEnv(t)
	mustFetch(t, p, api, clock, false, "work")
	api.next = func(name string) usage.AccountUsage { return rateLimited(name, 0) }
	clock.advance(4 * time.Minute)

	res := mustFetch(t, p, api, clock, false, "work")

	wantRetry := clock.t.Add(5 * time.Minute)
	if res.State != usage.StateOK || res.Usage == nil || *res.Usage.FiveHour.UsedPct != 40 || !res.Stale || !res.RateLimited {
		t.Fatalf("res=%+v", res)
	}
	if res.StaleFor != 4*time.Minute || res.RetryAt == nil || !res.RetryAt.Equal(wantRetry) || res.RetryIn != 5*time.Minute {
		t.Fatalf("freshness: stale %v retry %v in %v", res.StaleFor, res.RetryAt, res.RetryIn)
	}
	e := cachedEntry(t, p, "work")
	if e.FetchedAt == nil || !e.FetchedAt.Equal(now) || e.BackoffStep != 1 || e.LastError != "rate limited" || *e.FiveHourUsed != 40 || e.State != "ok" || !e.Stale {
		t.Fatalf("entry=%+v", e)
	}
}

func TestCachedFetchBackoffBlocksEvenForce(t *testing.T) {
	p, api, clock := cacheEnv(t)
	api.next = func(name string) usage.AccountUsage { return rateLimited(name, 0) }
	mustFetch(t, p, api, clock, false, "work")
	clock.advance(5*time.Minute - time.Second)

	res := mustFetch(t, p, api, clock, true, "work")

	if api.count("work") != 1 {
		t.Fatalf("http calls=%d want 1", api.count("work"))
	}
	if res.State != usage.StateAPIError || res.Usage != nil || !res.RateLimited || res.RetryIn != time.Second {
		t.Fatalf("res=%+v", res)
	}
}

func TestCachedFetchRateLimitScheduleAndReset(t *testing.T) {
	p, api, clock := cacheEnv(t)
	api.next = func(name string) usage.AccountUsage { return rateLimited(name, 0) }
	var got []time.Duration
	for i := 0; i < 5; i++ {
		res := mustFetch(t, p, api, clock, false, "work")
		got = append(got, res.RetryIn)
		clock.advance(res.RetryIn)
	}
	api.next = func(name string) usage.AccountUsage { return okUsage(name, 1) }
	mustFetch(t, p, api, clock, false, "work")

	want := fmt.Sprint([]time.Duration{5 * time.Minute, 10 * time.Minute, 20 * time.Minute, 30 * time.Minute, 30 * time.Minute})
	if fmt.Sprint(got) != want {
		t.Fatalf("schedule %v want %v", got, want)
	}
	e := cachedEntry(t, p, "work")
	if e.BackoffStep != 0 || e.BackoffUntil != nil || e.LastError != "" || e.Stale || e.RateLimited {
		t.Fatalf("after success: %+v", e)
	}
}

func TestCachedFetchRetryAfterExtendsBackoff(t *testing.T) {
	p, api, clock := cacheEnv(t)
	api.next = func(name string) usage.AccountUsage { return rateLimited(name, 17*time.Minute) }

	res := mustFetch(t, p, api, clock, false, "work")

	if res.RetryIn != 17*time.Minute {
		t.Fatalf("retry in %v", res.RetryIn)
	}
}

func TestCachedFetchOtherErrorFixedBackoff(t *testing.T) {
	p, api, clock := cacheEnv(t)
	mustFetch(t, p, api, clock, false, "work")
	api.next = func(name string) usage.AccountUsage {
		return usage.AccountUsage{Account: name, State: usage.StateAPIError, Err: "timeout"}
	}
	clock.advance(MinFetchInterval)

	res := mustFetch(t, p, api, clock, false, "work")

	if res.RetryIn != ErrorBackoff || !res.Stale || res.RateLimited || res.Err != "timeout" || res.Usage == nil {
		t.Fatalf("res=%+v", res)
	}
	if e := cachedEntry(t, p, "work"); e.BackoffStep != 0 {
		t.Fatalf("step=%d", e.BackoffStep)
	}
}

func TestCachedFetchLoggedOutNeverBacksOff(t *testing.T) {
	p, api, clock := cacheEnv(t)
	mustFetch(t, p, api, clock, false, "work")
	api.next = func(name string) usage.AccountUsage {
		return usage.AccountUsage{Account: name, State: usage.StateLoggedOut}
	}
	clock.advance(MinFetchInterval)

	first := mustFetch(t, p, api, clock, false, "work")
	second := mustFetch(t, p, api, clock, false, "work")

	if api.count("work") != 3 || first.State != usage.StateLoggedOut || second.State != usage.StateLoggedOut || second.Usage != nil {
		t.Fatalf("calls=%d first=%+v second=%+v", api.count("work"), first, second)
	}
	if e := cachedEntry(t, p, "work"); e.FetchedAt != nil || e.FiveHourUsed != nil || e.State != "logged-out" {
		t.Fatalf("entry=%+v", e)
	}
}

func TestCachedFetchConcurrentWritersMerge(t *testing.T) {
	p, api, clock := cacheEnv(t)
	names := []string{"a", "b", "c", "d", "e", "f", "g"}
	var wg sync.WaitGroup

	for _, name := range names {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			if _, err := fetchOnce(p, api, clock, false, name); err != nil {
				t.Error(err)
			}
		}(name)
	}
	wg.Wait()

	c, err := ReadCache(p)
	if err != nil || len(c.Accounts) != len(names) {
		t.Fatalf("accounts=%+v err=%v", c.Accounts, err)
	}
}

func TestCachedFetchReadsLegacyCache(t *testing.T) {
	p, api, clock := cacheEnv(t)
	legacy := `{"written_at":"2026-11-19T11:59:00Z","accounts":[{"name":"work","email":"w@x","plan":"max","five_hour_used":5,"seven_day_used":1,"five_hour_resets_at":null,"seven_day_resets_at":null,"state":"ok"}],"combined":{}}`
	if err := os.MkdirAll(filepath.Dir(CachePath(p)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CachePath(p), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	res := mustFetch(t, p, api, clock, false, "work")

	if api.count("work") != 1 || res.State != usage.StateOK || *res.Usage.FiveHour.UsedPct != 40 {
		t.Fatalf("legacy entry has no fetch time, so it must fetch: calls=%d res=%+v", api.count("work"), res)
	}
}

func TestWriteCacheKeepsLastGoodAndFetchState(t *testing.T) {
	p, api, clock := cacheEnv(t)
	mustFetch(t, p, api, clock, false, "work")
	api.next = func(name string) usage.AccountUsage { return rateLimited(name, 0) }
	clock.advance(MinFetchInterval)
	mustFetch(t, p, api, clock, false, "work")
	errRow := Row{AccountUsage: usage.AccountUsage{Account: "work", State: usage.StateAPIError, Err: "rate limited"}}

	if err := WriteCache(p, []Row{errRow}, usage.Combined{}, clock.t); err != nil {
		t.Fatal(err)
	}

	e := cachedEntry(t, p, "work")
	if e.FiveHourUsed == nil || *e.FiveHourUsed != 40 || e.State != "ok" || e.FetchedAt == nil || e.BackoffStep != 1 || e.BackoffUntil == nil {
		t.Fatalf("entry=%+v", e)
	}
}

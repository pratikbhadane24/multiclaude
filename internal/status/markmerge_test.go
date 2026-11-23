package status

import (
	"context"
	"testing"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/paths"
	"github.com/pratikbhadane24/multiclaude/internal/registry"
	"github.com/pratikbhadane24/multiclaude/internal/usage"
)

// fetchDuringMark runs one forced CachedFetch whose request blocks until a
// MarkLimited (5h, resetting at reset) has landed, and returns its result and
// the cached entry afterwards.
func fetchDuringMark(t *testing.T, p paths.Paths, res usage.AccountUsage, reset time.Time) (usage.AccountUsage, CacheAccount) {
	t.Helper()
	clock := &cacheClock{t: time.Now().Add(-time.Second)}
	started, release := make(chan struct{}), make(chan struct{})
	fetch := func(context.Context, registry.Account) usage.AccountUsage {
		close(started)
		<-release
		return res
	}
	cf := NewCachedFetch(p, fetch, clock.now, true)
	out := make(chan usage.AccountUsage, 1)
	go func() { out <- cf.Fetch(context.Background(), registry.Account{Name: "work"}) }()

	<-started
	if err := MarkLimited(p, LimitMark{Name: "work", ResetsAt: &reset}); err != nil {
		t.Fatal(err)
	}
	close(release)
	got := <-out
	if err := cf.Err(); err != nil {
		t.Fatal(err)
	}
	return got, cachedEntry(t, p, "work")
}

func assertMarked(t *testing.T, e CacheAccount, reset time.Time) {
	t.Helper()
	if e.MarkedAt == nil || e.State != StateLimited || e.FiveHourUsed == nil || *e.FiveHourUsed != 100 ||
		e.FiveHourResetsAt == nil || !e.FiveHourResetsAt.Equal(reset) {
		t.Fatalf("mark lost: %+v", e)
	}
	if _, limited := EntryLimitedUntil(e, time.Now()); !limited {
		t.Fatalf("entry not limited: %+v", e)
	}
}

func TestMarkDuringInFlightFetchSurvives(t *testing.T) {
	timeout := usage.AccountUsage{Account: "work", State: usage.StateAPIError, Err: "timeout"}
	cases := []struct {
		name  string
		res   usage.AccountUsage
		check func(t *testing.T, got usage.AccountUsage, e CacheAccount)
	}{
		{"success", okUsage("work", 40), func(t *testing.T, got usage.AccountUsage, e CacheAccount) {
			if e.FetchedAt == nil || e.LastError != "" || e.SevenDayOpus == nil || *e.SevenDayOpus.UsedPct != 9 {
				t.Fatalf("bookkeeping/unmarked windows not updated: %+v", e)
			}
			if got.Usage == nil || *got.Usage.FiveHour.UsedPct != 100 {
				t.Fatalf("caller saw pre-mark usage: %+v", got)
			}
		}},
		{"429", rateLimited("work", 0), func(t *testing.T, _ usage.AccountUsage, e CacheAccount) {
			if !e.RateLimited || e.BackoffStep != 1 || e.BackoffUntil == nil {
				t.Fatalf("backoff not recorded: %+v", e)
			}
		}},
		{"error", timeout, func(t *testing.T, _ usage.AccountUsage, e CacheAccount) {
			if e.LastError != "timeout" || e.BackoffUntil == nil {
				t.Fatalf("error not recorded: %+v", e)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			p := markPaths(t)
			reset := time.Now().Add(2 * time.Hour).UTC()

			// Act
			got, e := fetchDuringMark(t, p, tc.res, reset)

			// Assert
			assertMarked(t, e, reset)
			tc.check(t, got, e)
		})
	}
}

func TestFetchStartedAfterMarkReplacesIt(t *testing.T) {
	p, api, _ := cacheEnv(t)
	reset := time.Now().Add(2 * time.Hour)
	if err := MarkLimited(p, LimitMark{Name: "work", ResetsAt: &reset}); err != nil {
		t.Fatal(err)
	}
	clock := &cacheClock{t: time.Now().Add(time.Second)}

	got := mustFetch(t, p, api, clock, true, "work")

	e := cachedEntry(t, p, "work")
	if e.MarkedAt != nil || e.State != "ok" || *e.FiveHourUsed != 40 || got.Usage == nil || *got.Usage.FiveHour.UsedPct != 40 {
		t.Fatalf("fresh fetch did not replace mark: entry=%+v got=%+v", e, got)
	}
}

func TestWriteCacheAfterMarkKeepsMark(t *testing.T) {
	p, api, _ := cacheEnv(t)
	clock := &cacheClock{t: time.Now().Add(-time.Second)}
	mustFetch(t, p, api, clock, false, "work")
	reset := time.Now().Add(time.Hour).UTC()
	if err := MarkLimited(p, LimitMark{Name: "work", ResetsAt: &reset}); err != nil {
		t.Fatal(err)
	}
	preMarkRow := Row{AccountUsage: okUsage("work", 40)}

	if err := WriteCache(p, []Row{preMarkRow}, usage.Combined{}, time.Now()); err != nil {
		t.Fatal(err)
	}

	e := cachedEntry(t, p, "work")
	assertMarked(t, e, reset)
	if e.FetchedAt == nil {
		t.Fatalf("fetch state dropped: %+v", e)
	}
}

func TestWriteCacheKeepsMarkOnNeverFetchedEntry(t *testing.T) {
	p := markPaths(t)
	reset := time.Now().Add(time.Hour).UTC()
	if err := MarkLimitedType(p, "new", "five_hour", reset); err != nil {
		t.Fatal(err)
	}
	if err := MarkLimitedType(p, "gone", "five_hour", reset); err != nil {
		t.Fatal(err)
	}
	errRow := Row{AccountUsage: usage.AccountUsage{Account: "new", State: usage.StateAPIError, Err: "boom"}}

	if err := WriteCache(p, []Row{errRow}, usage.Combined{}, time.Now()); err != nil {
		t.Fatal(err)
	}

	assertMarked(t, cachedEntry(t, p, "new"), reset)
	assertMarked(t, cachedEntry(t, p, "gone"), reset)
}

func TestMarkExpiresAfterReset(t *testing.T) {
	p := markPaths(t)
	reset := time.Now().Add(time.Hour).UTC()
	if err := MarkLimitedType(p, "work", "five_hour", reset); err != nil {
		t.Fatal(err)
	}
	afterReset := reset.Add(ResetGrace + time.Second)
	if err := MarkLimitedType(p, "gone", "five_hour", reset); err != nil {
		t.Fatal(err)
	}

	if err := WriteCache(p, []Row{{AccountUsage: okUsage("work", 40)}}, usage.Combined{}, afterReset); err != nil {
		t.Fatal(err)
	}

	e := cachedEntry(t, p, "work")
	if e.MarkedAt != nil || *e.FiveHourUsed != 40 || e.State != "ok" {
		t.Fatalf("expired mark kept: %+v", e)
	}
	c, _ := ReadCache(p)
	if _, ok := indexByName(c.Accounts)["gone"]; ok {
		t.Fatalf("expired leftover mark kept: %+v", c.Accounts)
	}
}

func TestMergeFetchClearsExpiredMarkOnFailure(t *testing.T) {
	marked, reset := now, now.Add(time.Hour)
	cur := CacheAccount{Name: "work", FiveHourUsed: pf(100), FiveHourResetsAt: &reset, State: StateLimited, MarkedAt: &marked}
	res := usage.AccountUsage{Account: "work", State: usage.StateAPIError, Err: "timeout"}

	before := mergeFetch(cur, "work", res, now.Add(-time.Minute))
	after := mergeFetch(cur, "work", res, reset.Add(ResetGrace))

	if before.MarkedAt == nil || after.MarkedAt != nil || cur.MarkedAt == nil {
		t.Fatalf("before=%+v after=%+v cur=%+v", before, after, cur)
	}
}

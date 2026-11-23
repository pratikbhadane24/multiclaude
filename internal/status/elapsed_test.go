package status

import (
	"testing"
	"time"

	"multiclaude/internal/usage"
)

// staleEntry is a cache entry whose last fetch failed, so its usage is kept
// from fetchedAgo before now.
func staleEntry(five, seven *float64, fiveReset, sevenReset *time.Time) CacheAccount {
	fetched := now.Add(-9 * time.Hour)
	return CacheAccount{
		Name: "gmail", State: string(usage.StateOK),
		FiveHourUsed: five, FiveHourResetsAt: fiveReset,
		SevenDayUsed: seven, SevenDayResetsAt: sevenReset,
		FetchedAt: &fetched, LastError: "rate limited", RateLimited: true,
	}
}

func at(d time.Duration) *time.Time {
	t := now.Add(d)
	return &t
}

func TestFromCacheEntryElapsedWindowIsIdleAndNotStale(t *testing.T) {
	// Arrange: 7d window was 99% but reset an hour ago; 5h window idle.
	e := staleEntry(nil, f(99), nil, at(-time.Hour))

	// Act
	got := fromCacheEntry(e, now)

	// Assert
	if got.Usage.SevenDay.UsedPct != nil || got.Usage.SevenDay.ResetsAt != nil {
		t.Fatalf("elapsed 7d window kept: %+v", got.Usage.SevenDay)
	}
	if got.Stale {
		t.Fatal("row with only elapsed windows marked stale")
	}
	r := Row{AccountUsage: got}
	if label := StateLabel(r); label != "ok" {
		t.Fatalf("state %q, want ok", label)
	}
	if v := Window7d(r, now); !v.Idle {
		t.Fatalf("7d view %+v, want idle", v)
	}
}

func TestFromCacheEntryResetExactlyNowCountsAsElapsed(t *testing.T) {
	got := fromCacheEntry(staleEntry(nil, f(100), nil, at(0)), now)
	if got.Usage.SevenDay.UsedPct != nil {
		t.Fatalf("window resetting now kept: %+v", got.Usage.SevenDay)
	}
}

func TestFromCacheEntryLiveWindowStaysStale(t *testing.T) {
	// 5h window still running keeps the row stale; elapsed 7d is dropped.
	got := fromCacheEntry(staleEntry(f(40), f(99), at(time.Hour), at(-time.Hour)), now)
	if got.Usage.FiveHour.UsedPct == nil || *got.Usage.FiveHour.UsedPct != 40 {
		t.Fatalf("live 5h window lost: %+v", got.Usage.FiveHour)
	}
	if got.Usage.SevenDay.UsedPct != nil {
		t.Fatalf("elapsed 7d window kept: %+v", got.Usage.SevenDay)
	}
	if !got.Stale {
		t.Fatal("row with a live cached window lost its stale mark")
	}
}

func TestFromCacheEntryElapsedSubWindowDropped(t *testing.T) {
	e := staleEntry(nil, nil, nil, nil)
	e.SevenDayOpus = &CacheWindow{UsedPct: f(80), ResetsAt: at(-time.Minute)}
	got := fromCacheEntry(e, now)
	if got.Usage.SevenDayOpus != nil {
		t.Fatalf("elapsed opus window kept: %+v", got.Usage.SevenDayOpus)
	}
	if got.Stale {
		t.Fatal("row with only elapsed windows marked stale")
	}
}

func TestFromCacheEntryDoesNotMutateEntry(t *testing.T) {
	e := staleEntry(nil, f(99), nil, at(-time.Hour))
	_ = fromCacheEntry(e, now)
	if e.SevenDayUsed == nil || *e.SevenDayUsed != 99 {
		t.Fatalf("entry mutated: %+v", e)
	}
}

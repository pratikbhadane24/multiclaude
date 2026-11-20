package status

import (
	"path/filepath"
	"testing"
	"time"

	"multiclaude/internal/paths"
	"multiclaude/internal/usage"
)

func markPaths(t *testing.T) paths.Paths {
	t.Helper()
	return paths.Paths{AccountsRoot: filepath.Join(t.TempDir(), "root")}
}

func seedCache(t *testing.T, p paths.Paths, accts ...CacheAccount) {
	t.Helper()
	if err := updateCache(p, func(Cache) Cache { return Cache{Accounts: accts} }); err != nil {
		t.Fatal(err)
	}
}

func pf(v float64) *float64 { return &v }

func TestMarkLimitedSetsWindowStateAndAliases(t *testing.T) {
	// Arrange
	p := markPaths(t)
	at := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	reset := at.Add(2 * time.Hour)
	seedCache(t, p,
		CacheAccount{Name: "main", LoginID: "u1", FiveHourUsed: pf(40), FetchedAt: &at, State: "ok"},
		CacheAccount{Name: "admin", LoginID: "u1", FiveHourUsed: pf(40), FetchedAt: &at, State: "ok"},
		CacheAccount{Name: "work", LoginID: "u2", FiveHourUsed: pf(10), FetchedAt: &at, State: "ok"},
	)

	// Act
	err := MarkLimited(p, LimitMark{Name: "main", ResetsAt: &reset})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	c, _ := ReadCache(p)
	for _, a := range c.Accounts {
		limited := a.Name != "work"
		if (a.State == StateLimited) != limited {
			t.Errorf("%s state=%s", a.Name, a.State)
		}
		if limited && (*a.FiveHourUsed != 100 || !a.FiveHourResetsAt.Equal(reset) || a.FetchedAt == nil) {
			t.Errorf("%s not marked: %+v", a.Name, a)
		}
	}
}

func TestMarkLimitedWeeklyKeepsResetWhenUnknown(t *testing.T) {
	p := markPaths(t)
	at := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	old := at.Add(48 * time.Hour)
	seedCache(t, p, CacheAccount{Name: "w", SevenDayUsed: pf(90), SevenDayResetsAt: &old, FetchedAt: &at})

	if err := MarkLimited(p, LimitMark{Name: "w", Weekly: true}); err != nil {
		t.Fatal(err)
	}

	c, _ := ReadCache(p)
	if a := c.Accounts[0]; *a.SevenDayUsed != 100 || !a.SevenDayResetsAt.Equal(old) || a.FiveHourUsed != nil {
		t.Fatalf("got %+v", a)
	}
}

func TestMarkLimitedAddsMissingEntry(t *testing.T) {
	p := markPaths(t)
	if err := MarkLimited(p, LimitMark{Name: "new"}); err != nil {
		t.Fatal(err)
	}
	c, _ := ReadCache(p)
	if len(c.Accounts) != 1 || c.Accounts[0].Name != "new" || c.Accounts[0].State != StateLimited {
		t.Fatalf("got %+v", c.Accounts)
	}
}

func TestCachedUsageMapsEntries(t *testing.T) {
	p := markPaths(t)
	at := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	seedCache(t, p,
		CacheAccount{Name: "a", FiveHourUsed: pf(30), FetchedAt: &at, LoginID: "x"},
		CacheAccount{Name: "out", State: string(usage.StateLoggedOut)},
	)

	got, err := CachedUsage(p, []string{"a", "out", "missing"}, at)

	if err != nil || len(got) != 3 {
		t.Fatalf("got %+v err=%v", got, err)
	}
	if got[0].State != usage.StateOK || *got[0].Usage.FiveHour.UsedPct != 30 || got[0].AccountUUID != "x" {
		t.Errorf("a=%+v", got[0])
	}
	if got[1].State != usage.StateLoggedOut || got[2].State != usage.StateAPIError {
		t.Errorf("out=%+v missing=%+v", got[1], got[2])
	}
}

func TestCachedUsageWithoutCacheFile(t *testing.T) {
	got, err := CachedUsage(markPaths(t), []string{"a"}, time.Now())
	if err != nil || got[0].State != usage.StateAPIError {
		t.Fatalf("got %+v err=%v", got, err)
	}
}

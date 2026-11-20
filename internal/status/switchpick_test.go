package status

import (
	"path/filepath"
	"testing"
	"time"

	"multiclaude/internal/paths"
	"multiclaude/internal/registry"
)

func switchAccts(names ...string) []registry.Account {
	out := make([]registry.Account, len(names))
	for i, n := range names {
		out[i] = registry.Account{Name: n, Dir: "/acc/" + n}
	}
	return out
}

func usedEntry(name string, five float64, reset *time.Time, login string) CacheAccount {
	at := now.Add(-time.Minute)
	return CacheAccount{Name: name, FiveHourUsed: f(five), FiveHourResetsAt: reset, SevenDayUsed: f(10),
		FetchedAt: &at, LoginID: login, State: "ok"}
}

func TestPickSwitchPrefersMostHeadroomAndSkipsAvoided(t *testing.T) {
	c := Cache{Accounts: []CacheAccount{usedEntry("a", 50, nil, "A"), usedEntry("b", 80, nil, "B"), usedEntry("c", 20, nil, "C")}}
	avoid := map[string]time.Time{"c": now.Add(time.Hour)}
	got := PickSwitch(switchAccts("a", "b", "c"), c, avoid, now)
	if !got.Found || got.Account.Name != "a" {
		t.Fatalf("got %+v", got)
	}
}

func TestPickSwitchSkipsDuplicateLoginOfAvoided(t *testing.T) {
	c := Cache{Accounts: []CacheAccount{usedEntry("a", 99, nil, "X"), usedEntry("b", 10, nil, "X"), usedEntry("c", 60, nil, "C")}}
	avoid := map[string]time.Time{"a": now.Add(time.Hour)}
	got := PickSwitch(switchAccts("a", "b", "c"), c, avoid, now)
	if got.Account.Name != "c" {
		t.Fatalf("got %+v, want c (b is the same login as a)", got)
	}
}

func TestPickSwitchSkipsLoggedOutAndLimited(t *testing.T) {
	reset := now.Add(2 * time.Hour)
	c := Cache{Accounts: []CacheAccount{
		{Name: "a", State: "logged-out"},
		usedEntry("b", 100, &reset, "B"),
	}}
	got := PickSwitch(switchAccts("a", "b"), c, nil, now)
	if got.Found {
		t.Fatalf("got %+v", got)
	}
	want := reset.Add(ResetGrace)
	if got.WaitUntil == nil || !got.WaitUntil.Equal(want) {
		t.Fatalf("wait %v want %v", got.WaitUntil, want)
	}
}

func TestPickSwitchWaitUsesSoonestOfAvoidAndLimited(t *testing.T) {
	reset := now.Add(2 * time.Hour)
	c := Cache{Accounts: []CacheAccount{usedEntry("b", 100, &reset, "B")}}
	avoid := map[string]time.Time{"a": now.Add(time.Hour)}
	got := PickSwitch(switchAccts("a", "b"), c, avoid, now)
	if got.Found || got.WaitUntil == nil || !got.WaitUntil.Equal(now.Add(time.Hour)) {
		t.Fatalf("got %+v", got)
	}
}

func TestPickSwitchResetPassedCountsFree(t *testing.T) {
	reset := now.Add(-2 * time.Minute)
	c := Cache{Accounts: []CacheAccount{usedEntry("a", 100, &reset, "A")}}
	got := PickSwitch(switchAccts("a"), c, nil, now)
	if !got.Found {
		t.Fatalf("got %+v", got)
	}
}

func TestPickSwitchUnknownRanksLast(t *testing.T) {
	c := Cache{Accounts: []CacheAccount{usedEntry("b", 90, nil, "B")}}
	got := PickSwitch(switchAccts("a", "b"), c, nil, now)
	if got.Account.Name != "b" {
		t.Fatalf("got %+v", got)
	}
	got = PickSwitch(switchAccts("a"), c, nil, now)
	if got.Account.Name != "a" {
		t.Fatalf("unknown account should still be usable: %+v", got)
	}
}

func TestMarkLimitedMergesUnderLock(t *testing.T) {
	root := t.TempDir()
	p := paths.Paths{Home: root, AccountsRoot: filepath.Join(root, "acc")}
	keep := usedEntry("other", 30, nil, "O")
	if err := updateCache(p, func(Cache) Cache { return Cache{Accounts: []CacheAccount{keep, usedEntry("a", 40, nil, "A")}} }); err != nil {
		t.Fatal(err)
	}
	reset := now.Add(time.Hour)
	if err := MarkLimited(p, "a", "five_hour", reset); err != nil {
		t.Fatal(err)
	}
	if err := MarkLimited(p, "new", "seven_day", reset); err != nil {
		t.Fatal(err)
	}
	c, err := ReadCache(p)
	if err != nil {
		t.Fatal(err)
	}
	byName := indexByName(c.Accounts)
	a := byName["a"]
	if *a.FiveHourUsed != 100 || !a.FiveHourResetsAt.Equal(reset) || a.State != StateLimited || a.LoginID != "A" || a.FetchedAt == nil {
		t.Fatalf("a = %+v", a)
	}
	if n := byName["new"]; n.SevenDayUsed == nil || *n.SevenDayUsed != 100 {
		t.Fatalf("new = %+v", n)
	}
	if o := byName["other"]; *o.FiveHourUsed != 30 {
		t.Fatalf("other changed: %+v", o)
	}
	if _, limited := EntryLimitedUntil(a, now); !limited {
		t.Fatal("a not limited")
	}
}

func TestLimitedEntrySubWindows(t *testing.T) {
	reset := now.Add(time.Hour)
	e := limitedEntry(CacheAccount{Name: "a"}, "seven_day_opus", reset)
	if e.SevenDayOpus == nil || *e.SevenDayOpus.UsedPct != 100 || e.FiveHourUsed != nil {
		t.Fatalf("%+v", e)
	}
	e = limitedEntry(CacheAccount{Name: "a"}, "seven_day_sonnet", reset)
	if e.SevenDaySonnet == nil {
		t.Fatalf("%+v", e)
	}
	e = limitedEntry(CacheAccount{Name: "a"}, "overage", reset)
	if e.FiveHourUsed == nil {
		t.Fatalf("%+v", e)
	}
}

func TestEntryLimitedUntilBothWindowsUsesLater(t *testing.T) {
	r5, r7 := now.Add(time.Hour), now.Add(48*time.Hour)
	e := CacheAccount{FiveHourUsed: f(100), FiveHourResetsAt: &r5, SevenDayUsed: f(100), SevenDayResetsAt: &r7}
	until, ok := EntryLimitedUntil(e, now)
	if !ok || !until.Equal(r7.Add(ResetGrace)) {
		t.Fatalf("%v %v", until, ok)
	}
	e.SevenDayResetsAt = nil
	if until, ok := EntryLimitedUntil(e, now); !ok || until != nil {
		t.Fatalf("unknown reset: %v %v", until, ok)
	}
}

package status

import (
	"testing"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/usage"
)

func win(used float64, resetIn time.Duration) usage.Window {
	at := now.Add(resetIn)
	return usage.Window{UsedPct: &used, ResetsAt: &at}
}

func weekly(name string, five, seven usage.Window) usage.AccountUsage {
	return usage.AccountUsage{Account: name, Email: name + "@x.io", Plan: "team", State: usage.StateOK,
		Usage: &usage.Usage{FiveHour: five, SevenDay: seven}}
}

func TestScorePrefersWeeklyQuotaExpiringSoon(t *testing.T) {
	// a: half its weekly quota expires in 8h; b: plenty of runway for 6 days.
	a := weekly("a", win(70, 2*time.Hour), win(50, 8*time.Hour))
	b := weekly("b", usage.Window{}, win(5, 6*24*time.Hour))
	got, ok := PickBest([]usage.AccountUsage{b, a}, now)
	if !ok || got.Account != "a" {
		t.Fatalf("got %q, want a (weekly expiring soon)", got.Account)
	}
}

func TestScoreSkipsAccountsWithoutRunway(t *testing.T) {
	// a's weekly expires soon but its 5h window is nearly gone: switching
	// there would hit a limit within minutes.
	a := weekly("a", win(95, 3*time.Hour), win(40, 6*time.Hour))
	b := weekly("b", win(10, 3*time.Hour), win(20, 5*24*time.Hour))
	got, _ := PickBest([]usage.AccountUsage{a, b}, now)
	if got.Account != "b" {
		t.Fatalf("got %q, want b (a has <%v%% runway)", got.Account, MinRunwayPct)
	}
}

func TestScoreFallsBackToLowRunwayWhenNothingElse(t *testing.T) {
	a := weekly("a", win(95, 3*time.Hour), win(40, 6*time.Hour))
	got, ok := PickBest([]usage.AccountUsage{a}, now)
	if !ok || got.Account != "a" {
		t.Fatalf("got %q ok=%v", got.Account, ok)
	}
}

func TestScoreTreatsPassedResetAsFree(t *testing.T) {
	// a's 5h window says 100% but reset an hour ago: usable again.
	a := weekly("a", win(100, -time.Hour), win(30, 10*time.Hour))
	got, ok := PickBest([]usage.AccountUsage{a}, now)
	if !ok || got.Account != "a" {
		t.Fatalf("got %q ok=%v, want a (5h already reset)", got.Account, ok)
	}
}

func TestScoreTieBreaksOnFiveHourLeft(t *testing.T) {
	a := weekly("a", win(60, time.Hour), win(50, 50*time.Hour))
	b := weekly("b", win(20, time.Hour), win(50, 50*time.Hour))
	got, _ := PickBest([]usage.AccountUsage{a, b}, now)
	if got.Account != "b" {
		t.Fatalf("got %q, want b (same pressure, more 5h left)", got.Account)
	}
}

func TestScoreExplainsPick(t *testing.T) {
	a := weekly("a", win(10, time.Hour), win(48, 7*time.Hour+40*time.Minute))
	s, ok := ScoreUsage(a.Usage, now)
	if !ok {
		t.Fatal("not usable")
	}
	if got := s.Reason(); got != "52% weekly left, resets in 7h40m" {
		t.Fatalf("reason %q", got)
	}
}

func TestPickSwitchPrefersWeeklyExpiringSoon(t *testing.T) {
	soon, late := now.Add(6*time.Hour), now.Add(6*24*time.Hour)
	at := now.Add(-time.Minute)
	c := Cache{Accounts: []CacheAccount{
		{Name: "a", FiveHourUsed: f(5), SevenDayUsed: f(5), SevenDayResetsAt: &late, FetchedAt: &at, LoginID: "A", State: "ok"},
		{Name: "b", FiveHourUsed: f(40), SevenDayUsed: f(50), SevenDayResetsAt: &soon, FetchedAt: &at, LoginID: "B", State: "ok"},
	}}
	got := PickSwitch(switchAccts("a", "b"), c, nil, now)
	if got.Account.Name != "b" {
		t.Fatalf("got %+v, want b", got)
	}
}

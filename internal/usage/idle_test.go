package usage

import (
	"math"
	"testing"
)

func TestAggregateIdleCountsAsFullyFree(t *testing.T) {
	got := Aggregate([]AccountUsage{au("max", StateOK, nil, f64(20), nil)})
	if math.Abs(got.FiveHourLeft-1.0) > 1e-9 || math.Abs(got.SevenDayLeft-0.8) > 1e-9 {
		t.Fatalf("left %+v", got)
	}
	if got.Counted != 1 || got.AvailableNow != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestAggregateBothWindowsIdleIsAvailable(t *testing.T) {
	got := Aggregate([]AccountUsage{au("max", StateExpiredRefreshed, nil, nil, nil)})
	if got.FiveHourLeft != 1 || got.SevenDayLeft != 1 || got.Counted != 1 || got.AvailableNow != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestAggregateLowStillAvailable(t *testing.T) {
	got := Aggregate([]AccountUsage{au("a", StateOK, f64(97), f64(10), nil)})
	if got.AvailableNow != 1 {
		t.Fatalf("got %+v", got)
	}
}

func withID(a AccountUsage, email, uuid string) AccountUsage {
	a.Email, a.AccountUUID = email, uuid
	return a
}

func TestCanonicalIndexByUUID(t *testing.T) {
	list := []AccountUsage{
		withID(au("main", StateOK, nil, nil, nil), "a@x.io", "u1"),
		withID(au("admin", StateOK, nil, nil, nil), "other@x.io", "u1"),
		withID(au("w", StateOK, nil, nil, nil), "w@x.io", "u2"),
	}
	got := CanonicalIndex(list)
	if got[0] != 0 || got[1] != 0 || got[2] != 2 {
		t.Fatalf("got %v", got)
	}
}

func TestCanonicalIndexEmailFallbackCaseInsensitive(t *testing.T) {
	list := []AccountUsage{
		withID(au("main", StateOK, nil, nil, nil), "Admin@X.io", ""),
		withID(au("admin", StateOK, nil, nil, nil), "admin@x.io", "u9"),
		withID(au("third", StateOK, nil, nil, nil), "admin@x.io", ""),
	}
	got := CanonicalIndex(list)
	if got[1] != 0 || got[2] != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestCanonicalIndexDifferentUUIDsSplitDespiteEmail(t *testing.T) {
	list := []AccountUsage{
		withID(au("a", StateOK, nil, nil, nil), "same@x.io", "u1"),
		withID(au("b", StateOK, nil, nil, nil), "same@x.io", "u2"),
	}
	if got := CanonicalIndex(list); got[1] != 1 {
		t.Fatalf("got %v", got)
	}
}

func TestCanonicalIndexIgnoresLoggedOutAndEmptyIdentity(t *testing.T) {
	list := []AccountUsage{
		withID(au("a", StateOK, nil, nil, nil), "same@x.io", ""),
		withID(AccountUsage{Account: "b", State: StateLoggedOut}, "same@x.io", ""),
		au("c", StateOK, nil, nil, nil),
		au("d", StateOK, nil, nil, nil),
	}
	got := CanonicalIndex(list)
	if got[1] != 1 || got[2] != 2 || got[3] != 3 {
		t.Fatalf("got %v", got)
	}
}

func TestAggregateCountsDuplicateLoginOnce(t *testing.T) {
	list := []AccountUsage{
		withID(au("main", StateOK, f64(40), f64(10), nil), "a@x.io", "u1"),
		withID(au("admin", StateOK, f64(40), f64(10), nil), "a@x.io", "u1"),
	}
	got := Aggregate(list)
	if got.Counted != 1 || got.AvailableNow != 1 || math.Abs(got.FiveHourLeft-0.6) > 1e-9 {
		t.Fatalf("got %+v", got)
	}
	if len(list) != 2 {
		t.Fatal("input mutated")
	}
}

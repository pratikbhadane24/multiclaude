package usage

import "testing"

func TestCanonicalPrefersUsableMember(t *testing.T) {
	bad := withID(AccountUsage{Account: "main", State: StateAPIError}, "a@x.io", "")
	good := withID(au("admin", StateOK, f64(10), f64(10), nil), "a@x.io", "")
	list := []AccountUsage{bad, good}
	got := CanonicalIndex(list)
	if got[0] != 1 || got[1] != 1 {
		t.Fatalf("got %v", got)
	}
	if c := Aggregate(list); c.Counted != 1 || c.AvailableNow != 1 {
		t.Fatalf("%+v", c)
	}
}

func TestCanonicalFirstWhenNoneUsable(t *testing.T) {
	list := []AccountUsage{
		withID(AccountUsage{Account: "a", State: StateAPIError}, "a@x.io", ""),
		withID(AccountUsage{Account: "b", State: StateAPIError}, "a@x.io", ""),
	}
	if got := CanonicalIndex(list); got[0] != 0 || got[1] != 0 {
		t.Fatalf("got %v", got)
	}
}

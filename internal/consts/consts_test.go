package consts

import "testing"

func TestReservedNames(t *testing.T) {
	if len(ReservedNames) != 14 {
		t.Fatalf("got %d", len(ReservedNames))
	}
	if _, ok := ReservedNames["t3-sync"]; !ok {
		t.Fatal("missing t3-sync")
	}
}

func TestAccountNameRE(t *testing.T) {
	for name, want := range map[string]bool{"work": true, "a-1": true, "": false, "Up": false, "a_b": false} {
		if AccountNameRE.MatchString(name) != want {
			t.Errorf("%q want %v", name, want)
		}
	}
}

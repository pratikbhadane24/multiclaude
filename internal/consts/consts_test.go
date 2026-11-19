package consts

import "testing"

func TestReservedNames(t *testing.T) {
	if len(ReservedNames) != 18 {
		t.Fatalf("got %d", len(ReservedNames))
	}
	for _, n := range []string{"install", "bin", "browser", "dash"} {
		if _, ok := ReservedNames[n]; !ok {
			t.Fatalf("missing %s", n)
		}
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

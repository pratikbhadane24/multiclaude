package consts

import "testing"

func TestHistoryItemsAreShared(t *testing.T) {
	want := []string{"projects", "file-history", "todos", "plans"}
	if len(HistoryItems) != len(want) {
		t.Fatalf("got %v", HistoryItems)
	}
	for _, h := range want {
		found := false
		for _, s := range SharedItems {
			found = found || s == h
		}
		if !found {
			t.Errorf("%s not in SharedItems", h)
		}
	}
	for _, never := range []string{"history.jsonl", "sessions", "session-env", "statsig", ".claude.json"} {
		for _, s := range SharedItems {
			if s == never {
				t.Errorf("%s must not be shared", never)
			}
		}
	}
}

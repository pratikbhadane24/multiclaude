package statusline

import (
	"testing"
	"time"

	"multiclaude/internal/status"
)

func TestRenderCacheYoungerThanMinFetchIntervalDoesNotSpawn(t *testing.T) {
	c := freshCache()
	c.WrittenAt = now.Add(-status.MinFetchInterval + time.Second)
	h := newHarness(t, c)

	h.render(t, "{}", "")

	if h.spawns != 0 {
		t.Fatalf("spawns=%d: a refresh within the min fetch interval would only hit the cache", h.spawns)
	}
}

func TestRenderStaleEntryKeepsNumbersWithMarker(t *testing.T) {
	c := freshCache()
	c.Accounts[0].Stale = true
	h := newHarness(t, c)

	if got := h.render(t, "{}", ""); got != "ccm: main 5h 76% · 7d 12% ~\n" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderStaleEntryWithOnlyElapsedWindowsDropsMarker(t *testing.T) {
	past := now.Add(-time.Hour)
	c := freshCache()
	c.Accounts[0].Stale = true
	c.Accounts[0].FiveHourUsed, c.Accounts[0].FiveHourResetsAt = nil, nil
	c.Accounts[0].SevenDayUsed, c.Accounts[0].SevenDayResetsAt = pct(99), &past
	h := newHarness(t, c)

	if got := h.render(t, "{}", ""); got != "ccm: main 5h idle · 7d idle\n" {
		t.Fatalf("got %q", got)
	}
}

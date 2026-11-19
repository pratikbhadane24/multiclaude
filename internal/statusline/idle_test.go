package statusline

import (
	"testing"
	"time"

	"multiclaude/internal/status"
)

func renderWith(t *testing.T, accts ...status.CacheAccount) string {
	t.Helper()
	c := &status.Cache{WrittenAt: now.Add(-time.Second), Accounts: accts}
	return newHarness(t, c).render(t, "{}", "")
}

func TestRenderIdleFiveHour(t *testing.T) {
	got := renderWith(t, status.CacheAccount{Name: "main", State: "ok", SevenDayUsed: pct(12)})
	if got != "ccm: main 5h idle · 7d 12%\n" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderIdleBothWindows(t *testing.T) {
	got := renderWith(t, status.CacheAccount{Name: "main", State: "expired-refreshed"})
	if got != "ccm: main 5h idle · 7d idle\n" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderZeroUsedNoResetIsIdle(t *testing.T) {
	got := renderWith(t, status.CacheAccount{Name: "main", State: "ok", FiveHourUsed: pct(0), SevenDayUsed: pct(0)})
	if got != "ccm: main 5h idle · 7d idle\n" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderErrorRowsStayBare(t *testing.T) {
	for _, st := range []string{"api-error", "logged-out"} {
		got := renderWith(t, status.CacheAccount{Name: "main", State: st})
		want := "ccm: main\n"
		if st == "logged-out" {
			want = "ccm: main (logged out)\n"
		}
		if got != want {
			t.Fatalf("%s: got %q", st, got)
		}
	}
}

func TestRenderLowMarkedWarning(t *testing.T) {
	cases := map[float64]string{
		94:  "ccm: main 5h 94% · 7d 1%\n",
		95:  "ccm: main 5h 95% ⚠ · 7d 1%\n",
		99:  "ccm: main 5h 99% ⚠ · 7d 1%\n",
		100: "ccm: main 5h 100% ⛔ · 7d 1%\n",
	}
	for used, want := range cases {
		got := renderWith(t, status.CacheAccount{Name: "main", State: "ok", FiveHourUsed: pct(used), SevenDayUsed: pct(1)})
		if got != want {
			t.Errorf("%v: got %q want %q", used, got, want)
		}
	}
}

func TestRenderAliasEntryResolvesByName(t *testing.T) {
	c := &status.Cache{WrittenAt: now.Add(-time.Second), Accounts: []status.CacheAccount{
		{Name: "work", State: "ok", FiveHourUsed: pct(5), SevenDayUsed: pct(6)},
	}}
	h := newHarness(t, c)
	if got := h.render(t, "{}", h.deps.Paths.AccountsRoot+"/work"); got != "ccm: work 5h 5% · 7d 6%\n" {
		t.Fatalf("got %q", got)
	}
}

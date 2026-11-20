package autoswitch

import (
	"testing"
	"time"

	"multiclaude/internal/paths"
	"multiclaude/internal/status"
)

func TestParseResetVariants(t *testing.T) {
	cal, err := time.LoadLocation("Asia/Calcutta")
	if err != nil {
		t.Skip("no tzdata")
	}
	now := time.Date(2026, 11, 20, 2, 0, 0, 0, cal)
	for _, tc := range []struct {
		text string
		want time.Time
	}{
		{"resets 1:20am (Asia/Calcutta)", time.Date(2026, 11, 21, 1, 20, 0, 0, cal)},
		{"resets Nov 23 at 10:30am (Asia/Calcutta)", time.Date(2026, 11, 23, 10, 30, 0, 0, cal)},
		{"1:20am (Asia/Calcutta) · /upgrade to Max 20x or turn on /extra-usage", time.Date(2026, 11, 21, 1, 20, 0, 0, cal)},
		{"Nov 23 at 10:30am (Asia/Calcutta) · /upgrade", time.Date(2026, 11, 23, 10, 30, 0, 0, cal)},
		{"reset at 5pm (Asia/Calcutta)", time.Date(2026, 11, 20, 17, 0, 0, 0, cal)},
		{"at 5 PM (Asia/Calcutta).", time.Date(2026, 11, 20, 17, 0, 0, 0, cal)},
		{"November 23 at 10:30am (Asia/Calcutta)", time.Date(2026, 11, 23, 10, 30, 0, 0, cal)},
	} {
		got, ok := ParseReset(tc.text, now)
		if !ok || !got.Equal(tc.want) {
			t.Errorf("%q: got %v ok=%v, want %v", tc.text, got, ok, tc.want)
		}
	}
}

func TestParseResetMissingZoneIsLocal(t *testing.T) {
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, time.Local)

	got, ok := ParseReset("5pm", now)

	if !ok || !got.Equal(time.Date(2026, 11, 20, 17, 0, 0, 0, time.Local)) {
		t.Fatalf("got %v ok=%v", got, ok)
	}
}

func TestParseResetRejectsBadNumbers(t *testing.T) {
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	for _, s := range []string{"13pm (UTC)", "3:61pm (UTC)", "Nov 32 at 3pm (UTC)", "/upgrade"} {
		if got, ok := ParseReset(s, now); ok {
			t.Errorf("%q parsed as %v", s, got)
		}
	}
}

func TestResetTextFindsResetAtPhrase(t *testing.T) {
	if got := resetText("Claude usage limit reached. Your limit will reset at 5pm (UTC)."); got != "at 5pm (UTC)." {
		t.Fatalf("got %q", got)
	}
}

func TestMarkLimitUnparsedResetMarksOneHour(t *testing.T) {
	// Arrange
	p := setup(t, []string{"a"}, status.CacheAccount{Name: "a", FiveHourUsed: pf(50), FiveHourResetsAt: tp(-2 * time.Hour), State: "ok"})

	// Act
	err := MarkLimit(p, "a", Limit{Reset: "whenever"}, pickNow)

	// Assert
	assertFiveHourReset(t, p, err, pickNow.Add(time.Hour))
}

func TestMarkLimitNoResetTextMarksOneHour(t *testing.T) {
	p := setup(t, []string{"a"}, status.CacheAccount{Name: "a", FiveHourUsed: pf(50), FiveHourResetsAt: tp(-2 * time.Hour), State: "ok"})

	err := MarkLimit(p, "a", Limit{}, pickNow)

	assertFiveHourReset(t, p, err, pickNow.Add(time.Hour))
}

func TestMarkLimitParsedResetIsKept(t *testing.T) {
	p := setup(t, []string{"a"}, status.CacheAccount{Name: "a", FiveHourUsed: pf(50), State: "ok"})

	err := MarkLimit(p, "a", Limit{Reset: "3pm (UTC)"}, pickNow)

	assertFiveHourReset(t, p, err, time.Date(2026, 11, 20, 15, 0, 0, 0, time.UTC))
}

func assertFiveHourReset(t *testing.T, p paths.Paths, err error, want time.Time) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	cache, err := status.ReadCache(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range cache.Accounts {
		if a.Name == "a" {
			if a.FiveHourResetsAt == nil || !a.FiveHourResetsAt.Equal(want) {
				t.Fatalf("reset %v, want %v", a.FiveHourResetsAt, want)
			}
			return
		}
	}
	t.Fatal("account a not cached")
}

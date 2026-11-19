package status

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"multiclaude/internal/usage"
)

func staleRow(five float64) Row {
	au := ok("work", f(five), f(10), nil)
	au.Stale, au.StaleFor, au.RateLimited, au.Err = true, 4*time.Minute, true, "rate limited"
	retry := now.Add(7 * time.Minute)
	au.RetryAt, au.RetryIn = &retry, 7*time.Minute
	return Row{AccountUsage: au}
}

func limitedNoData(retryIn time.Duration) Row {
	au := usage.AccountUsage{Account: "work", State: usage.StateAPIError, Err: "rate limited", RateLimited: true, RetryIn: retryIn}
	return Row{AccountUsage: au}
}

func TestStateLabelStaleKeepsNumbersAndSaysStale(t *testing.T) {
	cases := map[string]Row{
		"ok · stale 4m":           staleRow(40),
		"limited (5h) · stale 4m": staleRow(100),
		"low (5h) · stale 4m":     staleRow(97),
	}
	for want, r := range cases {
		if got := StateLabel(r); got != want {
			t.Errorf("got %q want %q", got, want)
		}
		if v := Window5h(r, now); v.Used == naText {
			t.Errorf("%s: stale row lost its numbers", want)
		}
	}
}

func TestStateLabelRateLimitedWithoutData(t *testing.T) {
	if got := StateLabel(limitedNoData(7 * time.Minute)); got != "rate limited (retry in 7m)" {
		t.Fatalf("got %q", got)
	}
	if got := StateLabel(limitedNoData(0)); got != "rate limited" {
		t.Fatalf("got %q", got)
	}
}

func TestFetchNote(t *testing.T) {
	timeout := staleRow(1)
	timeout.RateLimited, timeout.Err, timeout.RetryIn = false, "timeout", 90*time.Second
	cases := []struct {
		r    Row
		want string
	}{
		{staleRow(1), "rate limited, retry in 7m"},
		{timeout, "last fetch failed (timeout), retry in 2m"},
		{Row{AccountUsage: ok("a", f(1), f(1), nil)}, ""},
	}
	for _, tc := range cases {
		if got := FetchNote(tc.r); got != tc.want {
			t.Errorf("got %q want %q", got, tc.want)
		}
	}
}

func TestStaleRowsStillCountAndRenderInTable(t *testing.T) {
	r := staleRow(40)
	c := usage.Aggregate([]usage.AccountUsage{r.AccountUsage})

	out := Render([]Row{r}, c, now)

	if c.Counted != 1 || !strings.Contains(out, "ok · stale 4m") || !strings.Contains(out, "40%") {
		t.Fatalf("counted=%d out=\n%s", c.Counted, out)
	}
}

func TestRenderJSONStaleFields(t *testing.T) {
	b, err := RenderJSON([]Row{staleRow(40), limitedNoData(0), {AccountUsage: ok("fresh", f(1), f(1), nil)}}, usage.Combined{})
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Accounts []map[string]any `json:"accounts"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	stale, noData, fresh := out.Accounts[0], out.Accounts[1], out.Accounts[2]
	if stale["stale_seconds"] != 240.0 || stale["retry_at"] != now.Add(7*time.Minute).Format(time.RFC3339) || stale["rate_limited"] != true || stale["state"] != "ok" {
		t.Fatalf("stale=%v", stale)
	}
	if noData["rate_limited"] != true || noData["state"] != "api-error" {
		t.Fatalf("noData=%v", noData)
	}
	for _, k := range []string{"stale_seconds", "retry_at", "rate_limited"} {
		if _, has := fresh[k]; has {
			t.Fatalf("fresh row has %s: %v", k, fresh)
		}
	}
}

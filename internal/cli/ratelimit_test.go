package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	dashboard "multiclaude/internal/dash"
	"multiclaude/internal/status"
)

// withClock makes the app's clock a settable variable.
func (e *cliEnv) withClock() *time.Time {
	clock := testNow
	e.app.Now = func() time.Time { return clock }
	return &clock
}

func (e *cliEnv) resetOutput() {
	e.out.Reset()
	e.err.Reset()
}

func TestStatusReusesRecentFetchAcrossInvocations(t *testing.T) {
	e := newCLI(t, "")
	e.loginWithUsage(e.workDir, usageBody("30", "10"))

	for i := 0; i < 3; i++ {
		if code := e.main("status"); code != ExitOK {
			t.Fatalf("code=%d err=%s", code, e.err)
		}
	}

	if got := e.calls(e.workDir); got != 1 {
		t.Fatalf("usage requests=%d want 1", got)
	}
	if !strings.Contains(e.out.String(), "30%") {
		t.Fatalf("out=%s", e.out)
	}
}

func TestStatusRefreshFlagForcesFetch(t *testing.T) {
	e := newCLI(t, "")
	e.loginWithUsage(e.workDir, usageBody("30", "10"))

	e.main("status")
	e.main("status", "--refresh")

	if got := e.calls(e.workDir); got != 2 {
		t.Fatalf("usage requests=%d want 2", got)
	}
}

func TestStatus429KeepsLastGoodUsageAndBacksOff(t *testing.T) {
	e := newCLI(t, "")
	clock := e.withClock()
	e.loginWithUsage(e.workDir, usageBody("30", "10"))
	e.main("status")
	e.loginWithUsage(e.workDir, rateLimitedBody)
	*clock = clock.Add(4 * time.Minute)
	e.resetOutput()

	e.main("status")
	first := e.out.String()
	*clock = clock.Add(time.Minute)
	e.resetOutput()
	e.main("status", "--refresh", "--json")

	if !strings.Contains(first, "30%") || !strings.Contains(first, "ok · stale 4m") || strings.Contains(first, "api error") {
		t.Fatalf("table:\n%s", first)
	}
	if got := e.calls(e.workDir); got != 2 {
		t.Fatalf("usage requests=%d want 2 (backoff blocks --refresh)", got)
	}
	js := e.out.String()
	if !strings.Contains(js, `"stale_seconds":300`) || !strings.Contains(js, `"rate_limited":true`) || !strings.Contains(js, `"retry_at":"2026-11-19T12:09:00Z"`) {
		t.Fatalf("json=%s", js)
	}
	c, err := status.ReadCache(e.p)
	if err != nil || c.Accounts[1].FiveHourUsed == nil || *c.Accounts[1].FiveHourUsed != 30 || !c.Accounts[1].Stale {
		t.Fatalf("cache poisoned: %+v err=%v", c.Accounts, err)
	}
}

func TestStatus429WithoutDataSaysRateLimited(t *testing.T) {
	e := newCLI(t, "")
	e.loginWithUsage(e.workDir, rateLimitedBody)

	e.main("status")

	if out := e.out.String(); !strings.Contains(out, "rate limited (retry in 5m)") || strings.Contains(out, "api error") {
		t.Fatalf("out=%s", out)
	}
}

func TestDashCollectGoesThroughFetchCache(t *testing.T) {
	e := newCLI(t, "")
	e.loginWithUsage(e.workDir, usageBody("30", "10"))

	e.app.dashCollect(context.Background(), dashboard.CollectOptions{})
	e.app.dashCollect(context.Background(), dashboard.CollectOptions{})
	snap := e.app.dashCollect(context.Background(), dashboard.CollectOptions{Force: true})

	if got := e.calls(e.workDir); got != 2 {
		t.Fatalf("usage requests=%d want 2 (tick cached, r forced)", got)
	}
	if snap.CacheErr != nil {
		t.Fatalf("cache err %v", snap.CacheErr)
	}
}

func TestBestUsesFetchCache(t *testing.T) {
	e := newCLI(t, "")
	e.loginWithUsage(e.workDir, usageBody("30", "10"))
	e.main("status")

	e.main("best")

	if got := e.calls(e.workDir); got != 1 {
		t.Fatalf("usage requests=%d want 1", got)
	}
}

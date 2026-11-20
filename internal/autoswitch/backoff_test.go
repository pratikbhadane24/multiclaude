package autoswitch

import (
	"context"
	"fmt"
	"strings"
	"syscall"
	"testing"
	"time"

	"multiclaude/internal/registry"
)

// longWaits drops the signal-grace waits, leaving switch gaps and reset waits.
func longWaits(ws []time.Duration) []time.Duration {
	var out []time.Duration
	for _, w := range ws {
		if w > termGrace {
			out = append(out, w)
		}
	}
	return out
}

// staleResetHarness has every account limited while the cache claims the
// account resets right now (the endless-relaunch case).
func staleResetHarness(steps []step) *harness {
	h := &harness{steps: steps, pick: pickFrom()}
	h.now = time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	h.reset = func() (registry.Account, time.Time, bool) { return acct("a"), h.now, true }
	return h
}

func limitSteps(n int, last step) []step {
	steps := make([]step, 0, n+1)
	for range n {
		steps = append(steps, step{limit: true, exitOn: syscall.SIGINT})
	}
	return append(steps, last)
}

func TestRunBacksOffWhenResetAccountHitsAgainQuickly(t *testing.T) {
	// Arrange
	h := staleResetHarness(limitSteps(5, step{code: 0}))

	// Act
	code := Run(context.Background(), h.deps(), acct("a"), nil, sid)

	// Assert
	want := []time.Duration{ResetSlack, 15 * time.Minute, 30 * time.Minute, time.Hour, time.Hour}
	if code != 0 || fmt.Sprint(longWaits(h.waits)) != fmt.Sprint(want) {
		t.Fatalf("code=%d waits=%v", code, longWaits(h.waits))
	}
	if !strings.Contains(h.err.String(), "ccm: a hit its limit again within 10m of its reset — backing off 15m") {
		t.Fatalf("err=%q", h.err.String())
	}
}

func TestRunBackoffRestartsAfterALongRun(t *testing.T) {
	steps := limitSteps(3, step{code: 0})
	steps[1].elapse = 11 * time.Minute
	h := staleResetHarness(steps)

	Run(context.Background(), h.deps(), acct("a"), nil, sid)

	want := []time.Duration{ResetSlack, ResetSlack, 15 * time.Minute}
	if fmt.Sprint(longWaits(h.waits)) != fmt.Sprint(want) {
		t.Fatalf("waits=%v", longWaits(h.waits))
	}
}

func TestResetGuardSpacesRelaunchesAfterAResetWait(t *testing.T) {
	t0 := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	g := resetGuard{}.relaunched("a", t0)

	// Another account hitting 2m after the reset relaunch: no backoff, but
	// the relaunch is held to 5m after the reset relaunch.
	g2, quick := g.hit("b", t0.Add(2*time.Minute))
	if quick || g2.floor(t0.Add(2*time.Minute)) != 3*time.Minute {
		t.Fatalf("quick=%v floor=%v", quick, g2.floor(t0.Add(2*time.Minute)))
	}
	// Without a reset wait there is no floor.
	if f := (resetGuard{}).floor(t0); f != 0 {
		t.Fatalf("floor=%v", f)
	}
}

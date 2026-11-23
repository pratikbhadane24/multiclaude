package dash

import (
	"strings"
	"testing"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/status"
	"github.com/pratikbhadane24/multiclaude/internal/usage"
)

func staleDashRow(five float64) status.Row {
	retry := testNow.Add(7 * time.Minute)
	return status.Row{AccountUsage: usage.AccountUsage{Account: "work", Email: "w@x.io", Plan: "max", State: usage.StateOK,
		Usage: &usage.Usage{FiveHour: usage.Window{UsedPct: &five}, SevenDay: usage.Window{UsedPct: &five}},
		Stale: true, StaleFor: 4 * time.Minute, RateLimited: true, Err: "rate limited", RetryAt: &retry, RetryIn: 7 * time.Minute}}
}

func TestStateTextStaleRowKeepsShortStateAndAge(t *testing.T) {
	cases := map[string]status.Row{"ok · stale 4m": staleDashRow(40), "limited · stale 4m": staleDashRow(100)}
	for want, r := range cases {
		got, st := stateText(r)
		if got != want || st.GetForeground() != styleWarn.GetForeground() {
			t.Errorf("got %q (fg %v) want %q in warn style", got, st.GetForeground(), want)
		}
	}
}

func TestStateTextRateLimitedWithoutData(t *testing.T) {
	r := status.Row{AccountUsage: usage.AccountUsage{Account: "work", State: usage.StateAPIError, Err: "rate limited", RateLimited: true, RetryIn: 3 * time.Minute}}

	got, _ := stateText(r)

	if got != "rate limited" {
		t.Fatalf("got %q", got)
	}
}

func TestDetailsExplainStaleUsage(t *testing.T) {
	m, r := newLoaded(t)
	r.snap.Rows = []status.Row{staleDashRow(40)}
	m = update(t, m, snapshotMsg{snap: r.snap, at: testNow})

	out := strings.Join(m.detailLines(), "\n")

	if !strings.Contains(out, "ok · stale 4m") || !strings.Contains(out, "rate limited, retry in 7m") || !strings.Contains(out, "40% used") {
		t.Fatalf("details:\n%s", out)
	}
}

func TestRefreshKeyForcesAndTickDoesNot(t *testing.T) {
	m, r := newLoaded(t)

	_, cmd := press(t, m, "r")
	drain(cmd)
	_, cmd = m.Update(tickMsg{})
	drain(cmd)

	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.forces) < 2 || !r.forces[0] || r.forces[1] {
		t.Fatalf("forces=%v want [true false ...]", r.forces)
	}
}

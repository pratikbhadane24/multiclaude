package dash

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"multiclaude/internal/registry"
	"multiclaude/internal/status"
	"multiclaude/internal/usage"
)

// limitedMainSnapshot is main at 100% of its 5h window (resets in 47m) and
// work free.
func limitedMainSnapshot() Snapshot {
	rows := []status.Row{
		{AccountUsage: usage.AccountUsage{Account: "main", State: usage.StateOK, Usage: &usage.Usage{
			FiveHour: usage.Window{UsedPct: pct(100), ResetsAt: at(47 * time.Minute)},
			SevenDay: usage.Window{UsedPct: pct(50), ResetsAt: at(72 * time.Hour)},
		}}},
		{AccountUsage: usage.AccountUsage{Account: "work", State: usage.StateOK, Usage: &usage.Usage{
			FiveHour: usage.Window{UsedPct: pct(10), ResetsAt: at(2 * time.Hour)},
			SevenDay: usage.Window{UsedPct: pct(20), ResetsAt: at(96 * time.Hour)},
		}}},
	}
	return Snapshot{Rows: rows, Accounts: []registry.Account{{Name: "main"}, {Name: "work", Dir: "/tmp/accts/work"}}}
}

func newLimited(t *testing.T) (Model, *recorder) {
	t.Helper()
	m, r := newLoaded(t)
	r.snap = limitedMainSnapshot()
	m = update(t, m, snapshotMsg{snap: r.snap, at: testNow, started: testNow})
	return m, r
}

func lastExecArgs(r *recorder) []string {
	if len(r.execs) == 0 {
		return nil
	}
	return r.execs[len(r.execs)-1].Args
}

func TestEnterOnLimitedAccountAsksToStartOnBest(t *testing.T) {
	m, r := newLimited(t)

	m, cmd := press(t, m, "enter")

	if cmd != nil || len(r.launched) != 0 || m.mode != modeConfirm {
		t.Fatalf("cmd=%v launched=%v mode=%v", cmd != nil, r.launched, m.mode)
	}
	if want := "main is limited (resets in 47m). Start on work? [y/N]"; !strings.Contains(m.View(), want) {
		t.Fatalf("prompt %q missing:\n%s", want, m.View())
	}
}

func TestLimitedPromptYesLaunchesBest(t *testing.T) {
	m, r := newLimited(t)
	m, _ = press(t, m, "enter")

	m, cmd := press(t, m, "y")
	drain(cmd)

	if !reflect.DeepEqual(r.launched, []string{"work"}) || m.mode != modeList {
		t.Fatalf("launched=%v mode=%v", r.launched, m.mode)
	}
}

func TestLimitedPromptNoLaunchesSelectedAnyway(t *testing.T) {
	for _, k := range []string{"n", "enter"} {
		m, r := newLimited(t)
		m, _ = press(t, m, "enter")

		_, cmd := press(t, m, k)
		drain(cmd)

		if !reflect.DeepEqual(r.launched, []string{"main"}) {
			t.Fatalf("%s: launched=%v", k, r.launched)
		}
	}
}

func TestLimitedPromptEscCancels(t *testing.T) {
	m, r := newLimited(t)
	m, _ = press(t, m, "enter")

	m, cmd := press(t, m, "esc")

	if cmd != nil || len(r.launched) != 0 || m.mode != modeList {
		t.Fatalf("launched=%v mode=%v", r.launched, m.mode)
	}
}

func TestEnterOnLimitedWithNoFreeAccountLaunchesDirectly(t *testing.T) {
	m, r := newLimited(t)
	snap := limitedMainSnapshot()
	snap.Rows = snap.Rows[:1]
	m = update(t, m, snapshotMsg{snap: snap, at: testNow, started: testNow})

	_, cmd := press(t, m, "enter")
	drain(cmd)

	if !reflect.DeepEqual(r.launched, []string{"main"}) {
		t.Fatalf("launched=%v", r.launched)
	}
}

// exitClaude launches the selected account, lets claude exit, and feeds the
// refresh that follows (collected from r.snap, set to after).
func exitClaude(t *testing.T, m Model, r *recorder, after Snapshot) Model {
	t.Helper()
	m, cmd := press(t, m, "enter")
	done, ok := hasMsg[execDoneMsg](drain(cmd))
	if !ok {
		t.Fatal("no exec done")
	}
	r.mu.Lock()
	r.snap = after
	r.mu.Unlock()
	next, cmd := m.Update(done)
	return feed(t, next.(Model), drain(cmd))
}

func TestAfterExitOnNowLimitedAccountYesContinuesOnBest(t *testing.T) {
	m, r := newLoaded(t)
	m.deps.LatestSession = func() (string, error) { return "sess-9", nil }

	m = exitClaude(t, m, r, limitedMainSnapshot())
	if want := "Continue this conversation on work? [y/N]"; m.mode != modeConfirm || !strings.Contains(m.View(), want) {
		t.Fatalf("mode=%v view:\n%s", m.mode, m.View())
	}
	m, cmd := press(t, m, "y")
	drain(cmd)

	if got := lastExecArgs(r); !reflect.DeepEqual(got, []string{"/bin/claude-fake", "work", "--resume", "sess-9"}) {
		t.Fatalf("exec=%q", got)
	}
	if !reflect.DeepEqual(r.launched, []string{"main", "work"}) || m.mode != modeList {
		t.Fatalf("launched=%v mode=%v", r.launched, m.mode)
	}
}

func TestAfterExitContinuePromptNoDoesNothing(t *testing.T) {
	m, r := newLoaded(t)
	m.deps.LatestSession = func() (string, error) { return "sess-9", nil }
	m = exitClaude(t, m, r, limitedMainSnapshot())

	m, cmd := press(t, m, "n")

	if cmd != nil || len(r.launched) != 1 || m.mode != modeList {
		t.Fatalf("launched=%v mode=%v", r.launched, m.mode)
	}
}

func TestAfterExitNotLimitedNoPrompt(t *testing.T) {
	m, r := newLoaded(t)

	m = exitClaude(t, m, r, twoAccountSnapshot())

	if m.mode != modeList || m.handoffFrom != "" {
		t.Fatalf("mode=%v from=%q", m.mode, m.handoffFrom)
	}
}

func TestAfterExitContinueWithoutSessionToasts(t *testing.T) {
	m, r := newLoaded(t)
	m.deps.LatestSession = func() (string, error) { return "", errors.New("no conversation found") }
	m = exitClaude(t, m, r, limitedMainSnapshot())

	m, cmd := press(t, m, "y")

	if cmd != nil || len(r.launched) != 1 || !strings.Contains(m.toast, "no conversation found") {
		t.Fatalf("launched=%v toast=%q", r.launched, m.toast)
	}
}

func TestSnapshotStartedBeforeExitIsNotUsedForHandoff(t *testing.T) {
	m, r := newLoaded(t)
	m = update(t, m, execDoneMsg{label: "claude (main)", account: "main"})
	r.snap = limitedMainSnapshot()

	next, cmd := m.Update(snapshotMsg{snap: r.snap, at: testNow, started: testNow.Add(-time.Second)})
	m = next.(Model)

	if m.mode != modeList || m.handoffFrom != "main" {
		t.Fatalf("stale snapshot decided the handoff: mode=%v from=%q", m.mode, m.handoffFrom)
	}
	if _, ok := hasMsg[snapshotMsg](drain(cmd)); !ok {
		t.Fatal("stale snapshot should trigger a fresh collect")
	}
}

func TestConfirmFooterShowsChoices(t *testing.T) {
	m, _ := newLimited(t)
	m, _ = press(t, m, "enter")
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	v := m.View()

	if !strings.Contains(v, "y yes") || !strings.Contains(v, "esc cancel") {
		t.Fatalf("footer:\n%s", v)
	}
	assertFits(t, v, 80, 24)
}

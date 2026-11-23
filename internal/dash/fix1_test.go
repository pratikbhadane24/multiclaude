package dash

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pratikbhadane24/multiclaude/internal/status"
	"github.com/pratikbhadane24/multiclaude/internal/tokens"
	"github.com/pratikbhadane24/multiclaude/internal/usage"
)

func TestRefreshBeforeFirstSnapshotDoesNotCollectAgain(t *testing.T) {
	r := &recorder{snap: twoAccountSnapshot()}
	m := New(context.Background(), newTestDeps(r))

	m, cmd := press(t, m, "r")
	drain(cmd)
	_, cmd = m.Update(tickMsg{})
	drain(cmd)

	if n := r.count(&r.collects); n != 0 {
		t.Fatalf("extra collects while the first one runs: %d", n)
	}
}

func TestAccountKeysWithNoAccountsToastAdd(t *testing.T) {
	for _, k := range []string{"enter", "n", "l"} {
		m, r := newLoaded(t)
		m = update(t, m, snapshotMsg{snap: Snapshot{}, at: testNow})

		m, cmd := press(t, m, k)
		drain(cmd)

		if !strings.Contains(m.toast, "no account yet — press a to add one") || len(r.execs)+len(r.runs) != 0 {
			t.Errorf("%s: toast=%q", k, m.toast)
		}
	}
}

func TestSnapshotErrorKeepsPreviousRows(t *testing.T) {
	m, _ := newLoaded(t)

	m = update(t, m, snapshotMsg{snap: Snapshot{Err: errors.New("registry unreadable")}, at: testNow})

	if len(m.snap.Rows) != 2 || !strings.Contains(m.toast, "registry unreadable") || m.loading {
		t.Fatalf("rows=%d toast=%q loading=%v", len(m.snap.Rows), m.toast, m.loading)
	}
}

func TestRepeatedJobKeyIsIgnoredWhileRunning(t *testing.T) {
	for _, k := range []string{"d", "s", "t"} {
		m, _ := newLoaded(t)
		m, first := press(t, m, k)

		m, second := press(t, m, k)

		if first == nil || second != nil || !strings.Contains(m.toast, "already running") {
			t.Errorf("%s: second=%v toast=%q", k, second != nil, m.toast)
		}
		m = feed(t, m, drain(first))
		if m.mode == modeDoctor {
			m, _ = press(t, m, "esc")
		}
		if _, again := press(t, m, k); again == nil {
			t.Errorf("%s: not runnable again after it finished", k)
		}
	}
}

func TestTodayFitsAt80Columns(t *testing.T) {
	m, r := newLoaded(t)
	snap := r.snap
	snap.Today = tokens.Tokens{Input: 1200, Output: 3400, CacheRead: 2_000_000}
	m = update(t, m, snapshotMsg{snap: snap, at: testNow})
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	v := m.View()

	if !strings.Contains(v, "1.2k/3.4k") {
		t.Fatalf("today missing at 80 cols:\n%s", v)
	}
	assertFits(t, v, 80, 24)
}

func TestNoPerAccountTodayColumns(t *testing.T) {
	m, _ := newLoaded(t)
	m = update(t, m, tea.WindowSizeMsg{Width: 200, Height: 40})

	head := m.tableLines()[0]

	if strings.Contains(head, "TODAY") || strings.Contains(head, "CACHE") {
		t.Fatalf("per-account today columns must be gone: %q", head)
	}
}

func TestDetailsShowCombinedToday(t *testing.T) {
	m, r := newLoaded(t)
	snap := r.snap
	snap.Today = tokens.Tokens{Input: 5000, Output: 6000, CacheRead: 70_000, CacheWrite: 1000}
	m = update(t, m, snapshotMsg{snap: snap, at: testNow})

	d := strings.Join(m.detailLines(), "\n")

	if !strings.Contains(d, "today (all accounts) in 5.0k") || !strings.Contains(d, "cache read 70.0k") {
		t.Fatalf("details=%q", d)
	}
	snap.TodayErr = "boom"
	m = update(t, m, snapshotMsg{snap: snap, at: testNow})
	if d := strings.Join(m.detailLines(), "\n"); !strings.Contains(d, "error: boom") {
		t.Fatalf("details=%q", d)
	}
}

func TestHeaderShowsCombinedToday(t *testing.T) {
	m, r := newLoaded(t)
	snap := r.snap
	snap.Today = tokens.Tokens{Input: 5000, Output: 6000, CacheRead: 70_000}
	m = update(t, m, snapshotMsg{snap: snap, at: testNow})

	if h := strings.Join(m.headerLines(), "\n"); !strings.Contains(h, "today") || !strings.Contains(h, "5.0k/6.0k cache 70.0k") {
		t.Fatalf("header=%q", h)
	}
	m = update(t, m, snapshotMsg{snap: r.snap, at: testNow})
	if strings.Contains(strings.Join(m.headerLines(), "\n"), "today") {
		t.Fatal("zero combined today should be hidden")
	}
}

func TestHelpExplainsNumbersAndStates(t *testing.T) {
	m, _ := newLoaded(t)
	m, _ = press(t, m, "?")
	m = update(t, m, tea.WindowSizeMsg{Width: 120, Height: 50})

	v := m.View()

	for _, want := range []string{"accounts' worth", "★", "ok", "low", "limited", "idle", "logged out"} {
		if !strings.Contains(v, want) {
			t.Errorf("help missing %q:\n%s", want, v)
		}
	}
}

func TestHelpScrollsAndFits80x24(t *testing.T) {
	m, _ := newLoaded(t)
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = press(t, m, "?")
	assertFits(t, m.View(), 80, 24)

	for range 30 {
		m, _ = press(t, m, "down")
	}

	if !strings.Contains(m.View(), "Press ? or esc to close.") {
		t.Fatalf("end of help not reachable:\n%s", m.View())
	}
	assertFits(t, m.View(), 80, 24)
}

func TestStateLabelLowAndIdle(t *testing.T) {
	low := status.Row{AccountUsage: usage.AccountUsage{State: usage.StateOK, Usage: &usage.Usage{
		FiveHour: usage.Window{UsedPct: pct(95), ResetsAt: at(time.Hour)},
		SevenDay: usage.Window{UsedPct: pct(10), ResetsAt: at(time.Hour)},
	}}}
	idle := status.Row{AccountUsage: usage.AccountUsage{State: usage.StateOK, Usage: &usage.Usage{
		FiveHour: usage.Window{UsedPct: pct(0)},
		SevenDay: usage.Window{UsedPct: pct(10), ResetsAt: at(time.Hour)},
	}}}

	if s, _ := stateText(low); s != "low (5h)" {
		t.Errorf("low: %q", s)
	}
	if s, _ := stateText(idle); s != "idle" {
		t.Errorf("idle: %q", s)
	}
}

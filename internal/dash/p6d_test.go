package dash

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"multiclaude/internal/registry"
	"multiclaude/internal/status"
	"multiclaude/internal/usage"
)

func okRow(name, email string, five, seven *float64) status.Row {
	u := &usage.Usage{
		FiveHour: usage.Window{UsedPct: five},
		SevenDay: usage.Window{UsedPct: seven},
	}
	if five != nil {
		u.FiveHour.ResetsAt = at(time.Hour)
	}
	if seven != nil {
		u.SevenDay.ResetsAt = at(48 * time.Hour)
	}
	return status.Row{AccountUsage: usage.AccountUsage{Account: name, Email: email, State: usage.StateOK, Usage: u}}
}

func snapOf(rows ...status.Row) Snapshot {
	return Snapshot{Rows: rows, Accounts: []registry.Account{{Name: "main"}}}
}

func TestBarIdleIsEmptyWithIdleText(t *testing.T) {
	r := okRow("a", "a@x", nil, pct(10))

	got := bar(status.Window5h(r, testNow), 8)

	if got != "──────── idle" {
		t.Fatalf("bar=%q", got)
	}
}

func TestWindowStyleLowYellowLimitedRed(t *testing.T) {
	low := status.Window5h(okRow("a", "a", pct(96), pct(1)), testNow)
	lim := status.Window5h(okRow("a", "a", pct(100), pct(1)), testNow)
	ok := status.Window5h(okRow("a", "a", pct(94), pct(1)), testNow)

	if windowStyle(low).GetForeground() != styleWarn.GetForeground() {
		t.Error("low should be yellow")
	}
	if windowStyle(lim).GetForeground() != styleBad.GetForeground() {
		t.Error("limited should be red")
	}
	if windowStyle(ok).GetForeground() != styleOK.GetForeground() {
		t.Error("94% should stay green")
	}
}

func TestNinetyTwoPercentIsNotLow(t *testing.T) {
	m, r := newLoaded(t)
	r.snap = snapOf(okRow("main", "m@x", pct(92), pct(1)))
	m = update(t, m, snapshotMsg{snap: r.snap, at: testNow})

	if got := strings.Join(m.tableLines(), "\n"); strings.Contains(got, "low") {
		t.Fatalf("92%% must not be low:\n%s", got)
	}
}

func dupSnapshot(show bool) Snapshot {
	main := okRow("main", "m@x", pct(50), pct(50))
	work := okRow("work", "w@x", pct(60), pct(60))
	if !show {
		main.Aliases = []string{"admin"}
		return snapOf(main, work)
	}
	admin := okRow("admin", "m@x", pct(50), pct(50))
	admin.SameAs = "main"
	return snapOf(main, admin, work)
}

func TestCollapsedRowShowsAliases(t *testing.T) {
	m, r := newLoaded(t)
	r.snap = dupSnapshot(false)
	m = update(t, m, snapshotMsg{snap: r.snap, at: testNow})

	got := strings.Join(m.tableLines(), "\n")

	if !strings.Contains(got, "main (= admin)") {
		t.Fatalf("table:\n%s", got)
	}
}

func TestShownDuplicateRowIsLabelledAndNeverBest(t *testing.T) {
	m, r := newLoaded(t)
	m.deps.ShowDuplicates = true
	r.snap = dupSnapshot(true)
	m = update(t, m, snapshotMsg{snap: r.snap, at: testNow, showDuplicates: true})

	got := strings.Join(m.tableLines(), "\n")

	if !strings.Contains(got, "admin (= main)") || m.bestName() == "admin" {
		t.Fatalf("best=%q table:\n%s", m.bestName(), got)
	}
}

func TestKeyATogglesDuplicatesAndRecollects(t *testing.T) {
	m, r := newLoaded(t)

	m, cmd := press(t, m, "A")
	drain(cmd)

	if !m.deps.ShowDuplicates || len(r.shows) == 0 || !r.shows[len(r.shows)-1] {
		t.Fatalf("show=%v shows=%v", m.deps.ShowDuplicates, r.shows)
	}
	m = update(t, m, snapshotMsg{snap: r.snap, at: testNow, showDuplicates: true})
	m, _ = press(t, m, "A")
	if m.deps.ShowDuplicates {
		t.Fatal("second A should hide duplicates again")
	}
}

func TestSnapshotCollectedBeforeToggleIsIgnored(t *testing.T) {
	m, _ := newLoaded(t)
	m, _ = press(t, m, "A")
	before := len(m.snap.Rows)

	m = update(t, m, snapshotMsg{snap: dupSnapshot(false), at: testNow, showDuplicates: false})

	if len(m.snap.Rows) != before || !m.loading {
		t.Fatalf("stale snapshot applied (rows=%d loading=%v)", len(m.snap.Rows), m.loading)
	}
}

func TestFooterAndHelpListDuplicatesKey(t *testing.T) {
	m, _ := newLoaded(t)
	foot := strings.Join(m.footerLines(), "\n")
	help := strings.Join(m.helpLines(), "\n")

	if !strings.Contains(foot, "A duplicates") || !strings.Contains(help, "same login") {
		t.Fatalf("foot=%q", foot)
	}
}

func TestHelpStatesInPlainWords(t *testing.T) {
	m, _ := newLoaded(t)
	help := strings.Join(m.helpLines(), "\n")

	for _, want := range []string{"95%", "= name", "idle", "logged out", "limited", "low"} {
		if !strings.Contains(help, want) {
			t.Errorf("help missing %q", want)
		}
	}
	if strings.Contains(help, "90%") {
		t.Error("help still mentions the old 90% threshold")
	}
}

func TestLaunchConfigWarningToastsButStillLaunches(t *testing.T) {
	m, r := newLoaded(t)
	r.launchWarn = "bad json"

	m, cmd := press(t, m, "enter")

	if cmd == nil || !strings.Contains(m.toast, "ignoring config: bad json (default args not applied)") {
		t.Fatalf("cmd=%v toast=%q", cmd, m.toast)
	}
}

func TestDetailsShowConfigErrorForArgs(t *testing.T) {
	r := &recorder{snap: twoAccountSnapshot()}
	deps := newTestDeps(r)
	deps.ArgsFor = func(string) ([]string, error) { return nil, errors.New("boom") }
	m := update(t, New(context.Background(), deps), snapshotMsg{snap: r.snap, at: testNow})

	got := strings.Join(m.detailLines(), "\n")

	if !strings.Contains(got, "launch args: config error — run ccm config show") {
		t.Fatalf("details:\n%s", got)
	}
}

func TestLimitedResetToleratesMissingUsage(t *testing.T) {
	m, _ := newLoaded(t)

	if got := m.limitedReset(status.Row{}, "5h"); got != "?" {
		t.Fatalf("got %q", got)
	}
}

func TestHandoffClearedWhenPostExitSnapshotErrors(t *testing.T) {
	m, _ := newLoaded(t)
	m = update(t, m, execDoneMsg{label: "claude (main)", account: "main"})

	m = update(t, m, snapshotMsg{snap: Snapshot{Err: errors.New("net")}, at: testNow, started: testNow})

	if m.handoffFrom != "" {
		t.Fatalf("handoffFrom=%q", m.handoffFrom)
	}
	m = update(t, m, snapshotMsg{snap: limitedMainSnapshot(), at: testNow, started: testNow})
	if m.mode != modeList {
		t.Fatalf("re-prompted on a later tick: mode=%v", m.mode)
	}
}

func TestHandoffWaitsForListMode(t *testing.T) {
	m, _ := newLoaded(t)
	m = update(t, m, execDoneMsg{label: "claude (main)", account: "main"})
	m, _ = press(t, m, "?")

	m = update(t, m, snapshotMsg{snap: limitedMainSnapshot(), at: testNow, started: testNow})
	if m.mode != modeHelp || m.handoffFrom != "main" {
		t.Fatalf("offer should stay pending: mode=%v from=%q", m.mode, m.handoffFrom)
	}
	m, _ = press(t, m, "esc")

	if m.mode != modeConfirm || !strings.Contains(m.prompt.text, "Continue this conversation on work") {
		t.Fatalf("mode=%v prompt=%q", m.mode, m.prompt.text)
	}
}

func TestHandoffExpiresAfterTwoMinutes(t *testing.T) {
	m, _ := newLoaded(t)
	now := testNow
	m.deps.Now = func() time.Time { return now }
	m = update(t, m, execDoneMsg{label: "claude (main)", account: "main"})
	m, _ = press(t, m, "?")
	m = update(t, m, snapshotMsg{snap: limitedMainSnapshot(), at: now, started: now})

	now = now.Add(3 * time.Minute)
	m, _ = press(t, m, "esc")

	if m.mode != modeList || m.handoffFrom != "" {
		t.Fatalf("mode=%v from=%q", m.mode, m.handoffFrom)
	}
}

func TestDemoHasIdleLowAndDuplicateRows(t *testing.T) {
	d := Demo(newTestDeps(&recorder{}))
	m := New(context.Background(), d)
	m = update(t, m, snapshotMsg{snap: d.Collect(context.Background(), CollectOptions{}), at: testNow})

	v := strings.Join(m.tableLines(), "\n")

	for _, want := range []string{"idle", "low", "(= "} {
		if !strings.Contains(v, want) {
			t.Errorf("demo table missing %q:\n%s", want, v)
		}
	}
}

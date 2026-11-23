package dash

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// newMoving returns a model with animations on that has seen a window size
// but no snapshot yet.
func newMoving(t *testing.T) (Model, *recorder) {
	t.Helper()
	r := &recorder{snap: twoAccountSnapshot()}
	d := newTestDeps(r)
	d.Motion = true
	m := New(context.Background(), d)
	return update(t, m, tea.WindowSizeMsg{Width: 100, Height: 30}), r
}

// settle feeds frames until the model stops asking for more.
func settle(t *testing.T, m Model) Model {
	t.Helper()
	for range tweenFrames + maxStagger*staggerFrames + 1 {
		m = update(t, m, frameMsg{})
	}
	return m
}

func TestBarsGrowFromEmptyOnFirstSnapshot(t *testing.T) {
	m, r := newMoving(t)

	m = update(t, m, snapshotMsg{snap: r.snap, at: testNow})

	if rows := strings.Join(m.tableLines(), "\n"); strings.Contains(rows, barFull) || !strings.Contains(rows, "25%") {
		t.Fatalf("bars should start empty with the real figure beside them:\n%s", rows)
	}
	if rows := strings.Join(settle(t, m).tableLines(), "\n"); !strings.Contains(rows, "━━────── 25%") {
		t.Fatalf("bars should rest on their value:\n%s", rows)
	}
}

func TestBarsEaseFromShownValueToNewValue(t *testing.T) {
	m, r := newMoving(t)
	m = settle(t, update(t, m, snapshotMsg{snap: r.snap, at: testNow}))
	next := twoAccountSnapshot()
	next.Rows[0].Usage.FiveHour.UsedPct = pct(75)
	g := gauge{id: gauge5h + "main", target: 75}

	m = update(t, m, snapshotMsg{snap: next, at: testNow})
	start := m.fx.value(g)
	m = update(t, m, frameMsg{})
	mid := m.fx.value(g)

	if start != 25 || mid <= 25 || mid >= 75 || settle(t, m).fx.value(g) != 75 {
		t.Fatalf("start=%v mid=%v", start, mid)
	}
}

func TestFramesRunOnlyWhileBusy(t *testing.T) {
	m, r := newMoving(t)
	if !m.fx.ticking {
		t.Fatal("first load should animate")
	}
	before := r.count(&r.ticks)

	m = update(t, m, tea.WindowSizeMsg{Width: 90, Height: 30})

	if r.count(&r.ticks) != before {
		t.Fatal("a second frame timer was scheduled while one is pending")
	}
	m = settle(t, update(t, m, snapshotMsg{snap: r.snap, at: testNow}))
	if m.fx.ticking || m.busy() {
		t.Fatalf("resting dashboard still animating: %+v", m.fx)
	}
	rest := r.count(&r.ticks)
	if m, _ = press(t, m, "j"); r.count(&r.ticks) != rest {
		t.Fatal("moving the selection must not start frames")
	}
}

func TestMotionOffSchedulesNoFrames(t *testing.T) {
	r := &recorder{snap: twoAccountSnapshot()}
	m := New(context.Background(), newTestDeps(r))

	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = update(t, m, snapshotMsg{snap: r.snap, at: testNow})

	if m.fx.ticking || r.count(&r.ticks) != 0 || !strings.Contains(m.View(), "━━────── 25%") {
		t.Fatalf("ticks=%d fx=%+v", r.ticks, m.fx)
	}
}

func TestSpinnerAdvancesWithFrames(t *testing.T) {
	m, _ := newMoving(t)
	first := m.fx.spinner()

	for range spinnerEvery {
		m = update(t, m, frameMsg{})
	}

	if m.fx.spinner() == first || !strings.Contains(m.View(), m.fx.spinner()) {
		t.Fatalf("spinner stuck on %q:\n%s", first, m.View())
	}
}

func TestMeterDrawsHalfCells(t *testing.T) {
	for pctUsed, want := range map[float64]string{0: "────", 12.5: "╸───", 50: "━━──", 62.5: "━━╸─", 100: "━━━━", 140: "━━━━"} {
		if got := meter(pctUsed, 4, rampColor, plain); got != want {
			t.Errorf("meter(%v)=%q want %q", pctUsed, got, want)
		}
	}
}

func TestPanelIsExactlyAsWideAsAsked(t *testing.T) {
	lines := panel("Accounts", []string{"short", strings.Repeat("x", 80)}, 40)

	if len(lines) != 4 || !strings.Contains(lines[0], "Accounts") || !strings.Contains(lines[2], ellipsis) {
		t.Fatalf("lines=%q", lines)
	}
	for _, l := range lines {
		if lipgloss.Width(l) != 40 {
			t.Errorf("width %d: %q", lipgloss.Width(l), l)
		}
	}
}

func TestWindowKeepsSelectionVisible(t *testing.T) {
	for _, c := range []struct{ n, sel, size, lo, hi int }{
		{3, 2, 5, 0, 3}, {10, 0, 4, 0, 4}, {10, 6, 4, 3, 7}, {10, 9, 4, 6, 10},
	} {
		if lo, hi := window(c.n, c.sel, c.size); lo != c.lo || hi != c.hi {
			t.Errorf("window(%d,%d,%d)=%d,%d want %d,%d", c.n, c.sel, c.size, lo, hi, c.lo, c.hi)
		}
	}
}

func TestManyAccountsScrollInsteadOfOverflowing(t *testing.T) {
	m, r := newLoaded(t)
	snap := r.snap
	for _, name := range []string{"a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8", "a9", "b1", "b2", "b3", "b4", "b5", "b6", "b7", "b8", "b9"} {
		snap.Rows = append(snap.Rows, okRow(name, name+"@x", pct(5), pct(5)))
	}
	m = update(t, m, snapshotMsg{snap: snap, at: testNow})
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	for range len(snap.Rows) - 1 {
		m, _ = press(t, m, "j")
	}

	v := m.View()

	assertFits(t, v, 80, 24)
	if !strings.Contains(v, "5h left") || !strings.Contains(v, "7d left") || !strings.Contains(v, "next reset in") {
		t.Fatalf("combined capacity must stay visible on a short window:\n%s", v)
	}
	if !strings.Contains(v, "b9") || !strings.Contains(v, "of 20") || !strings.Contains(v, "╰") {
		t.Fatalf("selected last row not shown in a closed panel:\n%s", v)
	}
}

package dash

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pratikbhadane24/multiclaude/internal/usage"
)

func newWide(t *testing.T, snap Snapshot) Model {
	t.Helper()
	r := &recorder{snap: snap}
	m := New(context.Background(), newTestDeps(r))
	m = update(t, m, tea.WindowSizeMsg{Width: 180, Height: 30})
	return update(t, m, snapshotMsg{snap: snap, at: testNow})
}

func TestDashShowsWeeklyResetColumn(t *testing.T) {
	m := newWide(t, twoAccountSnapshot())
	out := m.View()
	for _, want := range []string{"7D RESET", "3d0h", "4d0h"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
}

func TestDashMaxWithoutFiveHourIsNotIdle(t *testing.T) {
	snap := twoAccountSnapshot()
	snap.Rows[0].Usage = &usage.Usage{SevenDay: usage.Window{UsedPct: pct(30), ResetsAt: at(48 * time.Hour)}}
	m := newWide(t, snap)
	line := lineWith(m.View(), "m@x.com")
	if !strings.Contains(line, "no limit") || strings.Contains(line, "idle") {
		t.Fatalf("max row: %s", line)
	}
}

func TestDashExplainsBestPick(t *testing.T) {
	snap := twoAccountSnapshot()
	// main's weekly quota expires in 6h with half left: use it first.
	snap.Rows[0].Usage.SevenDay = usage.Window{UsedPct: pct(50), ResetsAt: at(6 * time.Hour)}
	m := newWide(t, snap)
	if got := m.bestName(); got != "main" {
		t.Fatalf("best %q, want main", got)
	}
	if out := m.View(); !strings.Contains(out, "50% weekly left, resets in 6h0m") {
		t.Fatalf("no reason in details:\n%s", out)
	}
}

func lineWith(out, s string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, s) {
			return l
		}
	}
	return ""
}

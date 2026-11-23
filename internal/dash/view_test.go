package dash

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"multiclaude/internal/usage"
)

func TestViewSnapshotTwoAccounts(t *testing.T) {
	m, _ := newLoaded(t)

	v := m.View()

	for _, want := range []string{
		"ccm · 2 accounts", "5h left", "7d left",
		"main", "work", "m@x.com", "w@x.com",
		"━━", "25%", "10%",
		"⚠ skip-permissions",
		"today (all accounts)",
		"⏎ launch", "b best", "n new window", "l login", "a add", "d doctor",
		"s sync", "t t3-sync", "r refresh", "? help", "q quit",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q", want)
		}
	}
	assertFits(t, v, 100, 30)
}

func TestViewFitsSmallTerminal(t *testing.T) {
	m, _ := newLoaded(t)
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	v := m.View()

	assertFits(t, v, 80, 24)
	for _, want := range []string{"main", "work", "q quit"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
}

func TestBadgeFollowsSelectedAccount(t *testing.T) {
	m, _ := newLoaded(t)
	m, _ = press(t, m, "j")

	if strings.Contains(m.View(), "skip-permissions") {
		t.Fatal("work has no skip-permissions arg")
	}
}

func TestDetailsPaneShowsSelectedAccount(t *testing.T) {
	m, _ := newLoaded(t)
	m, _ = press(t, m, "j")

	v := m.View()

	for _, want := range []string{"/tmp/accts/work", "refreshed 0s ago", "press d"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
}

func TestStateLabels(t *testing.T) {
	m, _ := newLoaded(t)
	rows := m.snap.Rows
	rows[1].State = usage.StateLoggedOut
	rows[0].Usage.FiveHour.UsedPct = pct(100)

	v := m.View()

	if !strings.Contains(v, "limited (5h)") || !strings.Contains(v, "logged out") {
		t.Fatalf("view:\n%s", v)
	}
}

func TestLoadingViewBeforeFirstSnapshot(t *testing.T) {
	r := &recorder{}
	m := New(t.Context(), newTestDeps(r))

	if !strings.Contains(m.View(), "loading") {
		t.Fatalf("view: %q", m.View())
	}
}

func TestNewWindowArgvQuoting(t *testing.T) {
	got := NewWindowArgv(`/Users/a b/it's/ccm`, "work")

	script := `tell application "Terminal" to do script "'/Users/a b/it'\\''s/ccm' 'work'"`
	want := []string{"/usr/bin/osascript", "-e", script}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestNewWindowArgvEscapesAppleScriptQuotes(t *testing.T) {
	got := NewWindowArgv(`/x/"q"\ccm`, "w")

	want := `tell application "Terminal" to do script "'/x/\"q\"\\ccm' 'w'"`
	if got[2] != want {
		t.Fatalf("got  %s\nwant %s", got[2], want)
	}
}

func TestZeroWindowSizeKeepsDefaults(t *testing.T) {
	m, _ := newLoaded(t)

	m = update(t, m, tea.WindowSizeMsg{Width: 0, Height: 0})

	if m.width != 100 || m.height != 30 || !strings.Contains(m.View(), "main") {
		t.Fatalf("w=%d h=%d", m.width, m.height)
	}
}

func TestFitColumnsStopsAtFirstMissInPriorityOrder(t *testing.T) {
	cols := []column{{title: "a", width: 10, prio: 0}, {title: "b", width: 30, prio: 1}, {title: "c", width: 5, prio: 2}}

	got := fitColumns(cols, 30)

	if len(got) != 1 || got[0].title != "a" {
		t.Fatalf("got %+v", got)
	}
}

func assertFits(t *testing.T, v string, w, h int) {
	t.Helper()
	lines := strings.Split(v, "\n")
	if len(lines) > h {
		t.Errorf("%d lines > %d:\n%s", len(lines), h, v)
	}
	for i, l := range lines {
		if lipgloss.Width(l) > w {
			t.Errorf("line %d width %d > %d: %q", i, lipgloss.Width(l), w, l)
		}
	}
}

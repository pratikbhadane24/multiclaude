package dash

import (
	"context"
	"os/exec"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"multiclaude/internal/doctor"
	"multiclaude/internal/registry"
	"multiclaude/internal/status"
	"multiclaude/internal/usage"
)

var testNow = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

// recorder captures every side effect the model asks for.
type recorder struct {
	mu       sync.Mutex
	collects int
	launched []string
	execs    []*exec.Cmd
	runs     [][]string
	doctors  int
	syncs    int
	t3syncs  int
	ticks    int
	snap     Snapshot
	// shows records the showDuplicates flag of every collect.
	shows []bool
	// launchWarn is returned as Launch's config warning.
	launchWarn string
}

func (r *recorder) count(p *int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return *p
}

type tickFired struct{}

func pct(v float64) *float64 { return &v }

func at(d time.Duration) *time.Time {
	t := testNow.Add(d)
	return &t
}

func twoAccountSnapshot() Snapshot {
	main := registry.Account{Name: "main"}
	work := registry.Account{Name: "work", Dir: "/tmp/accts/work", Email: "w@x.com"}
	rows := []status.Row{
		{AccountUsage: usage.AccountUsage{Account: "main", Email: "m@x.com", Plan: "max", State: usage.StateOK,
			Usage: &usage.Usage{
				FiveHour: usage.Window{UsedPct: pct(25), ResetsAt: at(time.Hour)},
				SevenDay: usage.Window{UsedPct: pct(50), ResetsAt: at(72 * time.Hour)},
			}}},
		{AccountUsage: usage.AccountUsage{Account: "work", Email: "w@x.com", Plan: "pro", State: usage.StateOK,
			Usage: &usage.Usage{
				FiveHour: usage.Window{UsedPct: pct(10), ResetsAt: at(2 * time.Hour)},
				SevenDay: usage.Window{UsedPct: pct(20), ResetsAt: at(96 * time.Hour)},
			}}},
	}
	accts := []registry.Account{main, work}
	return Snapshot{Rows: rows, Combined: usage.Aggregate([]usage.AccountUsage{rows[0].AccountUsage, rows[1].AccountUsage}), Accounts: accts}
}

func newTestDeps(r *recorder) Deps {
	return Deps{
		Collect: func(_ context.Context, showDuplicates bool) Snapshot {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.collects++
			r.shows = append(r.shows, showDuplicates)
			return r.snap
		},
		Launch: func(name string, extra []string) (*exec.Cmd, string, error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.launched = append(r.launched, name)
			return exec.Command("/bin/claude-fake", append([]string{name}, extra...)...), r.launchWarn, nil
		},
		Self:     func(args ...string) *exec.Cmd { return exec.Command("/opt/ccm", args...) },
		SelfPath: "/opt/ccm",
		Run: func(_ context.Context, name string, args []string) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.runs = append(r.runs, append([]string{name}, args...))
			return nil
		},
		Doctor: func(context.Context) []doctor.Check {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.doctors++
			return []doctor.Check{{Account: "work", Name: "keychain", Status: doctor.StatusWarn, Detail: "logged out → ccm login work"}}
		},
		Sync: func(context.Context) (string, error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.syncs++
			return "work: up to date\n", nil
		},
		T3Sync: func(context.Context) (string, error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.t3syncs++
			return "T3 Code already up to date.\n", nil
		},
		ArgsFor: func(name string) ([]string, error) {
			if name == "main" {
				return []string{"--dangerously-skip-permissions"}, nil
			}
			return nil, nil
		},
		Now: func() time.Time { return testNow },
		Exec: func(c *exec.Cmd, fn tea.ExecCallback) tea.Cmd {
			r.mu.Lock()
			r.execs = append(r.execs, c)
			r.mu.Unlock()
			return func() tea.Msg { return fn(nil) }
		},
		Tick: func(time.Duration, func(time.Time) tea.Msg) tea.Cmd {
			r.mu.Lock()
			r.ticks++
			r.mu.Unlock()
			return func() tea.Msg { return tickFired{} }
		},
	}
}

// newLoaded returns a model that has already received the two-account snapshot.
func newLoaded(t *testing.T) (Model, *recorder) {
	t.Helper()
	r := &recorder{snap: twoAccountSnapshot()}
	m := New(context.Background(), newTestDeps(r))
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = update(t, m, snapshotMsg{snap: r.snap, at: testNow})
	return m, r
}

func update(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, _ := m.Update(msg)
	return next.(Model)
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func press(t *testing.T, m Model, s string) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(key(s))
	return next.(Model), cmd
}

// drain runs cmd and returns every message it produces, expanding batches.
func drain(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, drain(c)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

// feed applies msgs to m, ignoring the commands they return.
func feed(t *testing.T, m Model, msgs []tea.Msg) Model {
	t.Helper()
	for _, msg := range msgs {
		m = update(t, m, msg)
	}
	return m
}

func hasMsg[T any](msgs []tea.Msg) (T, bool) {
	for _, msg := range msgs {
		if v, ok := msg.(T); ok {
			return v, true
		}
	}
	var zero T
	return zero, false
}

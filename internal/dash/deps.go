// Package dash implements the full-screen ccm dashboard (Bubble Tea). Every
// side effect is injected through Deps so the model is testable without a TTY.
package dash

import (
	"context"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"multiclaude/internal/doctor"
	"multiclaude/internal/registry"
	"multiclaude/internal/status"
	"multiclaude/internal/usage"
)

// DefaultInterval is the auto-refresh period.
const DefaultInterval = 30 * time.Second

// Snapshot is one collection of every account's usage.
type Snapshot struct {
	Rows     []status.Row
	Combined usage.Combined
	Accounts []registry.Account
	Err      error
}

// Deps are the dashboard's injected collaborators.
type Deps struct {
	// Collect gathers usage for every registered account.
	Collect func(ctx context.Context) Snapshot
	// Launch builds the command `ccm <name>` would run (claude as that account).
	Launch func(name string) (*exec.Cmd, error)
	// Self builds a command that runs this ccm binary with args.
	Self func(args ...string) *exec.Cmd
	// SelfPath is the absolute ccm path used for new Terminal windows.
	SelfPath string
	// Run runs a short non-interactive command (osascript).
	Run func(ctx context.Context, name string, args []string) error
	// Doctor runs every doctor check (no fixes).
	Doctor func(ctx context.Context) []doctor.Check
	// Sync and T3Sync run the commands in-process and return their output.
	Sync   func(ctx context.Context) (string, error)
	T3Sync func(ctx context.Context) (string, error)
	// ArgsFor returns the merged launch args for an account.
	ArgsFor func(name string) []string
	Now     func() time.Time
	// Exec hands the terminal to a child (tea.ExecProcess in production).
	Exec func(c *exec.Cmd, fn tea.ExecCallback) tea.Cmd
	// Tick schedules a timer message (tea.Tick in production).
	Tick     func(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd
	Interval time.Duration
}

// withDefaults returns a copy of d with production defaults for unset hooks.
func withDefaults(d Deps) Deps {
	if d.Exec == nil {
		d.Exec = tea.ExecProcess
	}
	if d.Tick == nil {
		d.Tick = tea.Tick
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.ArgsFor == nil {
		d.ArgsFor = func(string) []string { return nil }
	}
	if d.Interval <= 0 {
		d.Interval = DefaultInterval
	}
	return d
}

// Messages.
type (
	snapshotMsg struct {
		snap Snapshot
		at   time.Time
	}
	tickMsg     struct{}
	execDoneMsg struct {
		label string
		err   error
	}
	doctorMsg struct{ checks []doctor.Check }
	toastMsg  struct{ text string }
)

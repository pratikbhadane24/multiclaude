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
	"multiclaude/internal/tokens"
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
	// Today is the combined token use today across accounts (shared history);
	// zero hides it from the header.
	Today    tokens.Tokens
	TodayErr string
	// CacheErr is a failed write of the status line's usage cache.
	CacheErr error
}

// CollectOptions tune one collection.
type CollectOptions struct {
	// ShowDuplicates keeps accounts that share a login as separate rows.
	ShowDuplicates bool
	// Force refetches usage even when the cached result is recent (r key);
	// an account backing off after errors is still not fetched.
	Force bool
}

// Deps are the dashboard's injected collaborators.
type Deps struct {
	// Collect gathers usage for every registered account.
	Collect func(ctx context.Context, opts CollectOptions) Snapshot
	// Launch builds the command `ccm <name> [extra...]` would run (claude as
	// that account, config args first).
	// warning is a non-fatal config problem (the launch still happens).
	Launch func(name string, extra []string) (cmd *exec.Cmd, warning string, err error)
	// LatestSession returns the newest conversation id for the current folder
	// (shared history), as `ccm continue` finds it.
	LatestSession func() (string, error)
	// NewSessionID returns a fresh session id (uuid v4) for a new
	// conversation; nil launches without --session-id.
	NewSessionID func() string
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
	// An error means the config could not be read.
	ArgsFor func(name string) ([]string, error)
	Now     func() time.Time
	// Exec hands the terminal to a child (tea.ExecProcess in production).
	Exec func(c *exec.Cmd, fn tea.ExecCallback) tea.Cmd
	// Tick schedules a timer message (tea.Tick in production).
	Tick     func(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd
	Interval time.Duration
	// ShowDuplicates lists accounts that share a login as their own rows
	// (toggled with A).
	ShowDuplicates bool
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
		d.ArgsFor = func(string) ([]string, error) { return nil, nil }
	}
	if d.Interval <= 0 {
		d.Interval = DefaultInterval
	}
	return d
}

// Background job names (one of each may run at a time).
const (
	jobDoctor = "doctor"
	jobSync   = "sync"
	jobT3Sync = "t3-sync"
)

// Messages.
type (
	snapshotMsg struct {
		snap Snapshot
		// started is when the collection began (it may predate a claude exit).
		started time.Time
		// showDuplicates is the setting the collection ran with.
		showDuplicates bool
		at             time.Time
	}
	tickMsg     struct{}
	execDoneMsg struct {
		label string
		// account is set for a claude launch: the account that just ran.
		account string
		// session is the id the launch started its conversation with ("" if unknown).
		session string
		err     error
	}
	doctorMsg struct{ checks []doctor.Check }
	toastMsg  struct{ job, text string }
)

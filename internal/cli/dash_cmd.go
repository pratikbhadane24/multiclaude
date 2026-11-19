package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"

	dashboard "multiclaude/internal/dash"
	"multiclaude/internal/doctor"
	"multiclaude/internal/registry"
	"multiclaude/internal/status"
)

// demoEnv, when "1", makes the dashboard show canned data (screenshots, smoke tests).
const demoEnv = "CCM_DASH_DEMO"

// isTerminal reports whether v is an *os.File attached to a terminal.
func isTerminal(v any) bool {
	f, ok := v.(*os.File)
	if !ok || f == nil {
		return false
	}
	return term.IsTerminal(f.Fd())
}

func (a *App) hasTTY() bool { return isTerminal(a.In) && isTerminal(a.Out) }

// cmdDash handles `ccm dash` (and bare `ccm` on a terminal).
func (a *App) cmdDash(ctx context.Context, args []string) int {
	if _, code, ok := a.parseArgs(a.newFlags("dash"), args, 0, "dash"); !ok {
		return code
	}
	if !a.hasTTY() {
		return a.errorf("dash needs an interactive terminal; try: ccm status")
	}
	// SIGINT must not end the dashboard while a child (claude, login) owns the
	// terminal: Bubble Tea handles signals itself, so detach from ctx's cancel.
	ctx = context.WithoutCancel(ctx)
	deps := a.dashDeps()
	if os.Getenv(demoEnv) == "1" {
		deps = dashboard.Demo(deps)
	}
	p := tea.NewProgram(dashboard.New(ctx, deps), tea.WithAltScreen(), tea.WithInput(a.In), tea.WithOutput(a.Out))
	if _, err := p.Run(); err != nil && !errors.Is(err, tea.ErrProgramKilled) && !errors.Is(err, tea.ErrInterrupted) {
		return a.errorf("dash: %v", err)
	}
	return ExitOK
}

func (a *App) dashDeps() dashboard.Deps {
	return dashboard.Deps{
		Collect: a.dashCollect,
		Launch:  a.LaunchCmd,

		LatestSession: a.dashLatestSession,
		NewSessionID:  newSessionID,
		Self:          a.selfCmd,
		SelfPath:      a.selfPath(),
		Run:           a.runQuiet,
		Doctor:        a.dashDoctor,
		Sync:          a.captured((*App).cmdSync),
		T3Sync:        a.captured((*App).cmdT3Sync),
		ArgsFor:       a.dashArgsFor,
		Now:           a.Now,
	}
}

// dashCollect re-reads the registry (accounts may have been added) and
// collects usage the same way `ccm status` does, including writing the
// status line's usage cache (a failed write is reported in the snapshot).
func (a *App) dashCollect(ctx context.Context, showDuplicates bool) dashboard.Snapshot {
	accts, err := registry.Load(a.Paths)
	if err != nil {
		return dashboard.Snapshot{Err: err}
	}
	deps := a.statusDeps(accts)
	deps.ShowDuplicates = showDuplicates
	rows, combined := status.Collect(ctx, deps)
	cacheErr := status.WriteCache(a.Paths, rows, combined, a.Now())
	return dashboard.Snapshot{Rows: rows, Combined: combined, Accounts: accts,
		Today: combined.Today, TodayErr: combined.TodayErr, CacheErr: cacheErr}
}

func (a *App) dashDoctor(ctx context.Context) []doctor.Check {
	accts, err := registry.Load(a.Paths)
	if err != nil {
		return []doctor.Check{{Name: "registry", Status: doctor.StatusFail, Detail: err.Error()}}
	}
	return doctor.Run(ctx, a.doctorDeps(accts), false)
}

// selfPath is the absolute ccm binary path.
func (a *App) selfPath() string {
	if a.SelfExe != "" {
		return a.SelfExe
	}
	exe, err := os.Executable()
	if err != nil {
		return "ccm"
	}
	return exe
}

func (a *App) selfCmd(args ...string) *exec.Cmd {
	return exec.Command(a.selfPath(), args...)
}

// runQuiet runs a short command without the terminal and fails on non-zero exit.
func (a *App) runQuiet(ctx context.Context, name string, args []string) error {
	res, err := a.Runner.Run(ctx, name, args, nil, false)
	if err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(name), err)
	}
	if res.Code != 0 {
		return fmt.Errorf("%s exited %d: %s", filepath.Base(name), res.Code, strings.TrimSpace(res.Stderr))
	}
	return nil
}

// captured runs a command handler in-process with its output in a buffer.
func (a *App) captured(h handler) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		var buf bytes.Buffer
		sub := *a
		sub.Out, sub.Err, sub.In = &buf, &buf, strings.NewReader("")
		if code := h(&sub, ctx, nil); code != ExitOK {
			return buf.String(), fmt.Errorf("exit %d", code)
		}
		return buf.String(), nil
	}
}

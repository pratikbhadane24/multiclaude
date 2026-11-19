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

	"multiclaude/internal/auth"
	dashboard "multiclaude/internal/dash"
	"multiclaude/internal/doctor"
	"multiclaude/internal/registry"
	"multiclaude/internal/status"
)

// demoEnv, when "1", makes the dashboard show canned data (screenshots, smoke tests).
const demoEnv = "CCM_DASH_DEMO"

// dashArgsFor returns the merged launch args (config defaults + account args)
// for an interactive launch from the dashboard.
// TODO(P1): return the merged config args here once internal/config lands.
var dashArgsFor = func(_ *App, _ string) []string { return nil }

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

// LaunchCmd builds the command `ccm <name>` runs: claude as that account with
// its merged launch args. Stdio is left unset for tea.ExecProcess to wire.
func (a *App) LaunchCmd(name string) (*exec.Cmd, error) {
	accts, err := registry.Load(a.Paths)
	if err != nil {
		return nil, err
	}
	acct, ok := registry.Find(accts, name)
	if !ok {
		return nil, fmt.Errorf("unknown account %q", name)
	}
	// P1 wires config args here via ArgsFor (dashArgsFor).
	c := exec.Command(auth.Binary(a.Paths), dashArgsFor(a, name)...)
	c.Env = auth.Env(acct, os.Environ())
	return c, nil
}

func (a *App) dashDeps() dashboard.Deps {
	return dashboard.Deps{
		Collect:  a.dashCollect,
		Launch:   a.LaunchCmd,
		Self:     a.selfCmd,
		SelfPath: a.selfPath(),
		Run:      a.runQuiet,
		Doctor:   a.dashDoctor,
		Sync:     a.captured((*App).cmdSync),
		T3Sync:   a.captured((*App).cmdT3Sync),
		ArgsFor:  func(name string) []string { return dashArgsFor(a, name) },
		Now:      a.Now,
	}
}

// dashCollect re-reads the registry (accounts may have been added) and
// collects usage the same way `ccm status` does.
// TODO(P2): write the status cache here once status.WriteCache exists.
func (a *App) dashCollect(ctx context.Context) dashboard.Snapshot {
	accts, err := registry.Load(a.Paths)
	if err != nil {
		return dashboard.Snapshot{Err: err}
	}
	rows, combined := status.Collect(ctx, a.statusDeps(accts))
	return dashboard.Snapshot{Rows: rows, Combined: combined, Accounts: accts,
		Today: combined.Today, TodayErr: combined.TodayErr}
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

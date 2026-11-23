package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/pratikbhadane24/multiclaude/internal/registry"
	"github.com/pratikbhadane24/multiclaude/internal/worker"
)

const workerUsage = "worker add <host> --accounts a,b [--name n] [--yes] | worker sync [<name>|--all] | worker status [--json] | worker list"

// cmdWorker dispatches `ccm worker ...`.
func (a *App) cmdWorker(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return a.usageErr(workerUsage)
	}
	switch args[0] {
	case "add":
		return a.workerAdd(ctx, args[1:])
	case "sync":
		return a.workerSync(ctx, args[1:])
	case "status":
		return a.workerStatus(ctx, args[1:])
	case "list":
		return a.workerList()
	}
	return a.usageErr(workerUsage)
}

func (a *App) workerAdd(ctx context.Context, args []string) int {
	fs := a.newFlags("worker add")
	accounts := fs.String("accounts", "", "comma-separated hub accounts to log in on the worker")
	name := fs.String("name", "", "name for the worker (default: the host)")
	yes := fs.Bool("yes", false, "do not ask before editing the worker's shell files")
	pos, code, ok := a.parseArgs(fs, args, 1, "worker add <host> --accounts a,b [--name n] [--yes]")
	if !ok {
		return code
	}
	names, emails, code, ok := a.workerAccounts(*accounts)
	if !ok {
		return code
	}
	w := worker.Worker{Name: *name, SSHHost: pos[0], Accounts: names, AddedAt: a.Now()}
	if w.Name == "" {
		w.Name = w.SSHHost
	}
	s := a.newSetup(w.SSHHost, *yes)
	steps := a.setupSteps(s, true)
	steps = append(steps,
		worker.Step{Name: "log in accounts", Run: func(ctx context.Context) error { return s.EnsureAccounts(ctx, names, emails) }},
		worker.Step{Name: "save worker", Run: func(context.Context) error { return a.saveWorker(w) }},
		worker.Step{Name: "doctor", Run: s.Doctor},
	)
	if err := worker.RunSteps(ctx, a.Out, steps); err != nil {
		return a.errorf("worker %s: %v", w.Name, err)
	}
	a.printf("worker %s is ready. Add it in Orca (Settings → SSH) and pick it under Run on.\n", w.Name)
	return ExitOK
}

// workerAccounts validates --accounts against the hub registry.
func (a *App) workerAccounts(list string) ([]string, map[string]string, int, bool) {
	if strings.TrimSpace(list) == "" {
		return nil, nil, a.usageErr("worker add <host> --accounts a,b"), false
	}
	accts, err := registry.Load(a.Paths)
	if err != nil {
		return nil, nil, a.errorf("%v", err), false
	}
	var names []string
	emails := map[string]string{}
	for _, n := range strings.Split(list, ",") {
		n = strings.TrimSpace(n)
		if _, dup := emails[n]; dup {
			fmt.Fprintf(a.Err, "ccm: %q is listed twice in --accounts\n", n)
			return nil, nil, ExitUsage, false
		}
		acct, ok := registry.Find(accts, n)
		if !ok || acct.IsMain() || acct.Email == "" {
			fmt.Fprintf(a.Err, "ccm: %q is not a named hub account with an email (see ccm list)\n", n)
			return nil, nil, ExitUsage, false
		}
		names = append(names, n)
		emails[n] = acct.Email
	}
	return names, emails, ExitOK, true
}

func (a *App) workerSync(ctx context.Context, args []string) int {
	fs := a.newFlags("worker sync")
	all := fs.Bool("all", false, "sync every worker")
	yes := fs.Bool("yes", false, "do not ask before editing the worker's shell files")
	pos, err := parseInterspersed(fs, args)
	if err != nil || (len(pos) == 1) == *all || len(pos) > 1 {
		return a.usageErr("worker sync <name> | worker sync --all")
	}
	ws, err := worker.Load(a.Paths)
	if err != nil {
		return a.errorf("%v", err)
	}
	targets := ws
	if !*all {
		w, ok := worker.Find(ws, pos[0])
		if !ok {
			return a.errorf("no worker named %q (see ccm worker list)", pos[0])
		}
		targets = []worker.Worker{w}
	}
	failed := 0
	for _, w := range targets {
		a.printf("== %s\n", w.Name)
		s := a.newSetup(w.SSHHost, *yes)
		steps := append(a.setupSteps(s, false), worker.Step{Name: "doctor", Run: s.Doctor})
		if err := worker.RunSteps(ctx, a.Out, steps); err != nil {
			failed++
			continue
		}
		w.LastSyncAt = a.Now()
		if err := a.saveWorker(w); err != nil {
			a.warnf("%v", err)
		}
	}
	if failed > 0 {
		return a.errorf("%d of %d workers failed to sync", failed, len(targets))
	}
	return ExitOK
}

// setupSteps are shared by add and sync; add also installs Claude Code.
func (a *App) setupSteps(s *worker.Setup, installClaude bool) []worker.Step {
	steps := []worker.Step{{Name: "check worker", Run: func(ctx context.Context) error { _, err := s.Probe(ctx); return err }}}
	if installClaude {
		steps = append(steps, worker.Step{Name: "Claude Code", Run: s.InstallClaude})
	}
	return append(steps,
		worker.Step{Name: "install ccm", Run: func(ctx context.Context) error {
			exe, err := a.realExe()
			if err != nil {
				return err
			}
			return s.InstallCCM(ctx, exe)
		}},
		worker.Step{Name: "shell setup", Run: s.ShellBlocks},
		worker.Step{Name: "push shared setup", Run: s.PushSetup},
	)
}

func (a *App) newSetup(host string, yes bool) *worker.Setup {
	return &worker.Setup{
		P: a.Paths, R: worker.SSHRemote{Host: host, R: a.Runner}, Out: a.Out, Yes: yes,
		Confirm: func(q string) bool {
			ans, err := a.promptLine(context.Background(), q+" [y/N] ")
			return err == nil && strings.EqualFold(ans, "y")
		},
	}
}

// realExe is this ccm binary's real path (its ccm-shim sits beside it).
func (a *App) realExe() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate ccm: %w", err)
	}
	return filepath.EvalSymlinks(exe)
}

func (a *App) saveWorker(w worker.Worker) error {
	ws, err := worker.Load(a.Paths)
	if err != nil {
		return err
	}
	return worker.Save(a.Paths, worker.With(ws, w))
}

func (a *App) workerStatus(ctx context.Context, args []string) int {
	fs := a.newFlags("worker status")
	asJSON := fs.Bool("json", false, "print JSON")
	if _, code, ok := a.parseArgs(fs, args, 0, "worker status [--json]"); !ok {
		return code
	}
	ws, err := worker.Load(a.Paths)
	if err != nil {
		return a.errorf("%v", err)
	}
	hs := worker.Collect(ctx, ws, func(h string) worker.Remote { return worker.SSHRemote{Host: h, R: a.Runner} },
		worker.DefaultStatusTimeout)
	if *asJSON {
		if hs == nil {
			hs = []worker.Health{}
		}
		enc := json.NewEncoder(a.Out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(hs); err != nil {
			return a.errorf("write json: %v", err)
		}
		return ExitOK
	}
	return a.printHealth(ws, hs)
}

func (a *App) printHealth(ws []worker.Worker, hs []worker.Health) int {
	if len(hs) == 0 {
		a.printf("no workers yet: ccm worker add <host> --accounts a,b\n")
		return ExitOK
	}
	tw := tabwriter.NewWriter(a.Out, 0, 0, listPad, ' ', 0)
	fmt.Fprintln(tw, "NAME\tUP\tLOAD\tMEM-FREE\tCLAUDE\tCCM\tACCOUNTS")
	for i, h := range hs {
		if !h.Reachable {
			fmt.Fprintf(tw, "%s\tno\t-\t-\t-\t-\t%s\n", h.Name, h.Err)
			continue
		}
		fmt.Fprintf(tw, "%s\tyes\t%.1f/%d\t%.1f GB\t%d\t%s\t%s\n", h.Name, h.Load1, h.Cores, h.FreeMemGB,
			h.ClaudeProcs, dash(h.CCMVersion), accountsText(ws[i].Accounts, h.Accounts))
	}
	if err := tw.Flush(); err != nil {
		return a.errorf("write status: %v", err)
	}
	return ExitOK
}

// accountsText lists the worker's accounts, marking logged-out ones.
func accountsText(want []string, loggedIn map[string]bool) string {
	parts := make([]string, 0, len(want))
	for _, n := range want {
		if loggedIn[n] {
			parts = append(parts, n)
		} else {
			parts = append(parts, n+" (logged out)")
		}
	}
	return strings.Join(parts, ", ")
}

func (a *App) workerList() int {
	ws, err := worker.Load(a.Paths)
	if err != nil {
		return a.errorf("%v", err)
	}
	if len(ws) == 0 {
		a.printf("no workers yet: ccm worker add <host> --accounts a,b\n")
		return ExitOK
	}
	tw := tabwriter.NewWriter(a.Out, 0, 0, listPad, ' ', 0)
	fmt.Fprintln(tw, "NAME\tHOST\tACCOUNTS\tLAST SYNC")
	for _, w := range ws {
		last := "-"
		if !w.LastSyncAt.IsZero() {
			last = w.LastSyncAt.Local().Format("2006-01-02 15:04")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", w.Name, w.SSHHost, strings.Join(w.Accounts, ", "), last)
	}
	if err := tw.Flush(); err != nil {
		return a.errorf("write list: %v", err)
	}
	return ExitOK
}

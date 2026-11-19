package cli

import (
	"context"
	"flag"
	"path/filepath"
	"time"

	"multiclaude/internal/auth"
	"multiclaude/internal/links"
	"multiclaude/internal/paths"
	"multiclaude/internal/registry"
	"multiclaude/internal/status"
	"multiclaude/internal/tokens"
	"multiclaude/internal/usage"
)

const (
	defaultWatch = 30 * time.Second
	// clearScreen moves the cursor home and clears the terminal.
	clearScreen = "\x1b[H\x1b[2J"
)

// fetcher builds the usage fetcher; refresh runs `claude auth status`.
func (a *App) fetcher() usage.Fetcher {
	return usage.Fetcher{
		Runner: a.Runner,
		HTTP:   a.HTTP,
		Now:    a.Now,
		Refresh: func(ctx context.Context, acct registry.Account) error {
			return auth.Refresh(ctx, a.Runner, a.Paths, acct)
		},
	}
}

// inspectShared checks shared links; main owns the shared copies, so it has none.
func inspectShared(p paths.Paths, dir string) ([]links.Item, error) {
	if filepath.Clean(dir) == filepath.Clean(p.ClaudeDir) {
		return nil, nil
	}
	return links.Inspect(p, dir)
}

func (a *App) statusDeps(accts []registry.Account) status.Deps {
	return status.Deps{
		Accounts: accts,
		Fetch:    a.fetcher().Fetch,
		Today:    tokens.Today,
		Inspect:  inspectShared,
		Paths:    a.Paths,
		Now:      a.Now,

		EmailFallback: a.authEmail,
	}
}

// authEmail asks `claude auth status` for the account's email; "" on any failure.
func (a *App) authEmail(ctx context.Context, acct registry.Account) string {
	st, err := auth.GetStatus(ctx, a.Runner, a.Paths, acct)
	if err != nil || !st.LoggedIn {
		return ""
	}
	return st.Email
}

const statusUsage = "status [--json] [--watch d] [--refresh-cache]"

// cmdStatus handles `ccm status [--json] [--watch d]`.
func (a *App) cmdStatus(ctx context.Context, args []string) int {
	fs := a.newFlags("status")
	jsonOut := fs.Bool("json", false, "print JSON")
	watch := fs.Duration("watch", 0, "refresh every interval (e.g. 30s)")
	refreshCache := fs.Bool("refresh-cache", false, "update the status line cache quietly")
	if _, code, ok := a.parseArgs(fs, withWatchDefault(args), 0, statusUsage); !ok {
		return code
	}
	watchSet := false
	fs.Visit(func(f *flag.Flag) { watchSet = watchSet || f.Name == "watch" })
	if *jsonOut && watchSet {
		return a.usageErr("status: --json and --watch cannot be combined")
	}
	if *refreshCache && watchSet {
		return a.usageErr(statusUsage)
	}
	accts, err := registry.Load(a.Paths)
	if err != nil {
		return a.errorf("%v", err)
	}
	d := a.statusDeps(accts)
	if watchSet {
		return a.watchStatus(ctx, d, *watch)
	}
	rows, combined := status.Collect(ctx, d)
	if code, done := a.finishStatusCache(rows, combined, *refreshCache); done {
		return code
	}
	if *jsonOut {
		b, err := status.RenderJSON(rows, combined)
		if err != nil {
			return a.errorf("%v", err)
		}
		a.printf("%s\n", b)
		return ExitOK
	}
	a.printf("%s", status.Render(rows, combined, a.Now()))
	return ExitOK
}

// withWatchDefault lets a bare --watch mean --watch=30s.
func withWatchDefault(args []string) []string {
	out := make([]string, 0, len(args))
	for i, arg := range args {
		if arg != "--watch" && arg != "-watch" {
			out = append(out, arg)
			continue
		}
		if i+1 < len(args) {
			if _, err := time.ParseDuration(args[i+1]); err == nil {
				out = append(out, arg)
				continue
			}
		}
		out = append(out, "--watch="+defaultWatch.String())
	}
	return out
}

// watchStatus re-renders every interval until ctx is cancelled.
func (a *App) watchStatus(ctx context.Context, d status.Deps, interval time.Duration) int {
	if interval <= 0 {
		interval = defaultWatch
	}
	w := status.NewWatcher(a.Now)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		rows, combined := w.Step(ctx, d)
		if ctx.Err() != nil {
			return ExitOK
		}
		a.printf("%s%s", clearScreen, status.Render(rows, combined, a.Now()))
		select {
		case <-ctx.Done():
			return ExitOK
		case <-ticker.C:
		}
	}
}

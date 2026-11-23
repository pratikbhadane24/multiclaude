package cli

import (
	"context"
	"flag"
	"path/filepath"
	"sync"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/auth"
	"github.com/pratikbhadane24/multiclaude/internal/links"
	"github.com/pratikbhadane24/multiclaude/internal/paths"
	"github.com/pratikbhadane24/multiclaude/internal/registry"
	"github.com/pratikbhadane24/multiclaude/internal/status"
	"github.com/pratikbhadane24/multiclaude/internal/tokens"
	"github.com/pratikbhadane24/multiclaude/internal/usage"
)

const (
	defaultWatch = 30 * time.Second
	// clearScreen moves the cursor home and clears the terminal.
	clearScreen = "\x1b[H\x1b[2J"
)

// fetcher builds the usage fetcher; refresh runs `claude auth status`.
// Accounts signed in as the same login share one request.
func (a *App) fetcher() usage.Fetcher {
	return usage.Fetcher{
		Runner: a.Runner,
		HTTP:   a.HTTP,
		Now:    a.Now,
		Shared: usage.NewSharedFetches(),
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

// cachedFetch is the only way usage is fetched: through the persistent
// fetch cache (min interval, backoff, last good usage). force skips the
// min interval but never the backoff.
func (a *App) cachedFetch(force bool) *status.CachedFetch {
	return status.NewCachedFetch(a.Paths, a.fetcher().Fetch, a.Now, force)
}

// warnCacheErr reports a failure to record fetch state.
func (a *App) warnCacheErr(cf *status.CachedFetch) {
	if err := cf.Err(); err != nil {
		a.warnf("usage cache: %v", err)
	}
}

func (a *App) statusDeps(accts []registry.Account, cf *status.CachedFetch) status.Deps {
	return status.Deps{
		Accounts: accts,
		Fetch:    cf.Fetch,
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

// cheapDeps is d without what the status line cache never uses: the token
// scan of today and the `claude auth status` email lookups.
func cheapDeps(d status.Deps) status.Deps {
	d.Today, d.EmailFallback = nil, nil
	return d
}

// memoEmail wraps an email lookup so each account is asked once.
func memoEmail(lookup func(context.Context, registry.Account) string) func(context.Context, registry.Account) string {
	var mu sync.Mutex
	seen := map[string]string{}
	return func(ctx context.Context, acct registry.Account) string {
		mu.Lock()
		email, ok := seen[acct.Name]
		mu.Unlock()
		if ok {
			return email
		}
		email = lookup(ctx, acct)
		mu.Lock()
		seen[acct.Name] = email
		mu.Unlock()
		return email
	}
}

const statusUsage = "status [--json] [--all] [--refresh] [--watch d] [--refresh-cache]"

// cmdStatus handles `ccm status [--json] [--watch d]`.
func (a *App) cmdStatus(ctx context.Context, args []string) int {
	fs := a.newFlags("status")
	jsonOut := fs.Bool("json", false, "print JSON")
	watch := fs.Duration("watch", 0, "refresh every interval (e.g. 30s)")
	showAll := fs.Bool("all", false, "also list accounts that share a login as separate rows")
	refreshCache := fs.Bool("refresh-cache", false, "update the status line cache quietly")
	force := fs.Bool("refresh", false, "fetch now even if the cached usage is recent (never during a rate-limit backoff)")
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
	cf := a.cachedFetch(*force)
	d := a.statusDeps(accts, cf)
	d.ShowDuplicates = *showAll
	if *refreshCache {
		d = cheapDeps(d)
	}
	if watchSet {
		return a.watchStatus(ctx, d, *watch)
	}
	rows, combined := status.Collect(ctx, d)
	a.warnCacheErr(cf)
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

// watchStatus re-renders every interval until ctx is cancelled. Each step
// fetches through a fresh cache wrapper, so recent results are reused and
// failing accounts back off.
func (a *App) watchStatus(ctx context.Context, d status.Deps, interval time.Duration) int {
	if interval <= 0 {
		interval = defaultWatch
	}
	if d.EmailFallback != nil {
		d.EmailFallback = memoEmail(d.EmailFallback)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		cf := a.cachedFetch(false)
		d.Fetch = cf.Fetch
		rows, combined := status.Collect(ctx, d)
		if ctx.Err() != nil {
			return ExitOK
		}
		a.printf("%s%s", clearScreen, status.Render(rows, combined, a.Now()))
		a.warnCacheErr(cf)
		select {
		case <-ctx.Done():
			return ExitOK
		case <-ticker.C:
		}
	}
}

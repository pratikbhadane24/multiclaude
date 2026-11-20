package cli

import (
	"context"
	"fmt"
	"strings"

	"multiclaude/internal/registry"
	"multiclaude/internal/status"
	"multiclaude/internal/usage"
)

// exitAllLimited is returned by `best` when no account has headroom.
const exitAllLimited = 2

// cmdRun handles `ccm run <name> [claude args...]`; args pass through verbatim.
func (a *App) cmdRun(ctx context.Context, args []string) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return a.usageErr("run <name> [claude args...]")
	}
	acct, code, ok := a.findAccount(args[0])
	if !ok {
		return code
	}
	return a.runAccount(ctx, acct, args[1:])
}

// runAccount runs claude as acct with inherited stdio and returns its exit
// code. The child is not tied to ctx: an interrupt is delivered to it by the
// terminal and it decides how to exit.
//
// The configured default and per-account args are prepended to args; a broken
// config is reported and the launch continues without it. With auto-switch
// on, the auto-switch supervisor runs claude instead.
func (a *App) runAccount(ctx context.Context, acct registry.Account, args []string) int {
	if code, ok := a.runSupervised(ctx, acct, args); ok {
		return code
	}
	l, err := a.buildLaunch(acct, args)
	if err != nil {
		a.warnf("ignoring config: %v", err)
	}
	res, err := a.Runner.Run(context.WithoutCancel(ctx), l.Bin, l.Args, l.Env, true)
	if err != nil {
		return a.errorf("run claude as %s: %v", acct.Name, err)
	}
	return res.Code
}

// cmdBest runs claude as the account with the most usage headroom.
func (a *App) cmdBest(ctx context.Context, args []string) int {
	accts, err := registry.Load(a.Paths)
	if err != nil {
		return a.errorf("%v", err)
	}
	cf := a.cachedFetch(false)
	d := status.Deps{Accounts: accts, Fetch: cf.Fetch, Paths: a.Paths, Now: a.Now}
	rows, combined := status.Collect(ctx, d)
	a.warnCacheErr(cf)
	list := make([]usage.AccountUsage, len(rows))
	for i, r := range rows {
		list[i] = r.AccountUsage
	}
	best, ok := status.PickBest(list)
	if !ok {
		return a.noBest(combined)
	}
	acct, _ := registry.Find(accts, best.Account)
	fmt.Fprintf(a.Err, "ccm: using %s (5h %s used, 7d %s used)\n", acct.Name,
		status.FormatPct(best.Usage.FiveHour.UsedPct), status.FormatPct(best.Usage.SevenDay.UsedPct))
	return a.runAccount(ctx, acct, args)
}

func (a *App) noBest(c usage.Combined) int {
	if c.NextResetAt == nil {
		fmt.Fprintln(a.Err, "ccm: no usable accounts")
		return exitAllLimited
	}
	fmt.Fprintf(a.Err, "ccm: all accounts limited; next reset in %s\n", status.FormatDuration(max(0, c.NextResetAt.Sub(a.Now()))))
	return exitAllLimited
}

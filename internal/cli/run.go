package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"multiclaude/internal/auth"
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
func (a *App) runAccount(ctx context.Context, acct registry.Account, args []string) int {
	res, err := a.Runner.Run(context.WithoutCancel(ctx), auth.Binary(a.Paths), args, auth.Env(acct, os.Environ()), true)
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
	d := status.Deps{Accounts: accts, Fetch: a.fetcher().Fetch, Paths: a.Paths, Now: a.Now}
	rows, combined := status.Collect(ctx, d)
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

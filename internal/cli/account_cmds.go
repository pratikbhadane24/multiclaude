package cli

import "context"

// cmdAdd handles `ccm add <name> [--email e]`.
func (a *App) cmdAdd(ctx context.Context, args []string) int {
	fs := a.newFlags("add")
	email := fs.String("email", "", "email of the Claude account")
	pos, code, ok := a.parseArgs(fs, args, 1, "add <name> [--email e]")
	if !ok {
		return code
	}
	return a.Add(ctx, pos[0], *email)
}

// cmdLogin handles `ccm login <name>` and `ccm login --all`.
func (a *App) cmdLogin(ctx context.Context, args []string) int {
	const usage = "login <name> | --all"
	fs := a.newFlags("login")
	all := fs.Bool("all", false, "log in every logged-out account")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return flagExit(err)
	}
	switch {
	case *all && len(pos) == 0:
		return a.Login(ctx, allFlag)
	case !*all && len(pos) == 1:
		return a.Login(ctx, pos[0])
	}
	return a.usageErr(usage)
}

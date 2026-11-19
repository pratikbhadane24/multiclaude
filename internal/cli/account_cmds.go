package cli

import "context"

const openUsage = "how the login link is shown: copy, incognito or chrome:<Profile dir>"

// cmdAdd handles `ccm add <name> [--email e]`.
func (a *App) cmdAdd(ctx context.Context, args []string) int {
	fs := a.newFlags("add")
	email := fs.String("email", "", "email of the Claude account")
	open := fs.String("open", "", openUsage)
	pos, code, ok := a.parseArgs(fs, args, 1, "add <name> [--email e] [--open mode]")
	if !ok {
		return code
	}
	return a.AddWith(ctx, pos[0], *email, *open)
}

// cmdLogin handles `ccm login <name>` and `ccm login --all`.
func (a *App) cmdLogin(ctx context.Context, args []string) int {
	const usage = "login <name> [--open mode] | --all"
	fs := a.newFlags("login")
	all := fs.Bool("all", false, "log in every logged-out account")
	open := fs.String("open", "", openUsage)
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return flagExit(err)
	}
	switch {
	case *all && len(pos) == 0 && *open == "":
		return a.Login(ctx, allFlag)
	case !*all && len(pos) == 1:
		if *open != "" {
			if code := a.SetBrowser(pos[0], *open); code != ExitOK {
				return code
			}
		}
		return a.Login(ctx, pos[0])
	}
	return a.usageErr(usage)
}

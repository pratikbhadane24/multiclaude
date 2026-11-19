package cli

import (
	"context"
	"fmt"
	"strings"

	"multiclaude/internal/config"
	"multiclaude/internal/mcp"
	"multiclaude/internal/registry"
	"multiclaude/internal/t3"
)

// cmdT3Sync adds or updates a T3 Code instance for every account and keeps
// each instance's launchArgs in line with the ccm config.
func (a *App) cmdT3Sync(_ context.Context, args []string) int {
	if _, code, ok := a.parseArgs(a.newFlags("t3-sync"), args, 0, "t3-sync"); !ok {
		return code
	}
	accts, err := registry.Load(a.Paths)
	if err != nil {
		return a.errorf("%v", err)
	}
	cfg, err := config.Load(a.Paths)
	if err != nil {
		return a.errorf("%v", err)
	}
	var added, updated []string
	var backup string
	var warnings []string
	err = mcp.UpdateT3LaunchArgs(a.Paths, func(written map[string]string) (map[string]string, error) {
		var res t3.LaunchResult
		var syncErr error
		added, updated, backup, res, syncErr = t3.SyncLaunch(a.Paths, accts, a.Now(), t3.LaunchArgs{Desired: desiredLaunchArgs(cfg, accts), Written: written})
		warnings = res.Warnings
		return res.Written, syncErr
	})
	for _, w := range warnings {
		fmt.Fprintln(a.Err, w)
	}
	if err != nil {
		return a.errorf("t3-sync: %v", err)
	}
	return a.reportT3Sync(added, updated, backup)
}

// desiredLaunchArgs maps account name to the shell-joined merged args.
func desiredLaunchArgs(cfg config.Config, accts []registry.Account) map[string]string {
	out := make(map[string]string, len(accts))
	for _, acct := range accts {
		out[acct.Name] = config.ShellJoin(config.MergedArgs(cfg, acct.Name, nil))
	}
	return out
}

func (a *App) reportT3Sync(added, updated []string, backup string) int {
	if len(added)+len(updated) == 0 {
		a.printf("T3 Code already up to date.\n")
		return ExitOK
	}
	if len(added) > 0 {
		a.printf("Added: %s\n", strings.Join(added, ", "))
	}
	if len(updated) > 0 {
		a.printf("Updated: %s\n", strings.Join(updated, ", "))
	}
	if backup != "" {
		a.printf("Backup: %s\n", backup)
	}
	a.printf("Restart T3 Code to apply.\n")
	return ExitOK
}

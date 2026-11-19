package cli

import (
	"context"
	"strings"

	"multiclaude/internal/registry"
	"multiclaude/internal/t3"
)

// cmdT3Sync adds or updates a T3 Code instance for every account.
func (a *App) cmdT3Sync(_ context.Context, args []string) int {
	if _, code, ok := a.parseArgs(a.newFlags("t3-sync"), args, 0, "t3-sync"); !ok {
		return code
	}
	accts, err := registry.Load(a.Paths)
	if err != nil {
		return a.errorf("%v", err)
	}
	added, updated, backup, err := t3.Sync(a.Paths, accts, a.Now())
	if err != nil {
		return a.errorf("t3-sync: %v", err)
	}
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

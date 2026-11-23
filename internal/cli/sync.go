package cli

import (
	"context"
	"strings"

	"github.com/pratikbhadane24/multiclaude/internal/links"
	"github.com/pratikbhadane24/multiclaude/internal/mcp"
	"github.com/pratikbhadane24/multiclaude/internal/registry"
)

// cmdSync links shared config and syncs MCP servers into every account.
func (a *App) cmdSync(_ context.Context, args []string) int {
	if _, code, ok := a.parseArgs(a.newFlags("sync"), args, 0, "sync"); !ok {
		return code
	}
	accts, err := registry.Load(a.Paths)
	if err != nil {
		return a.errorf("%v", err)
	}
	code := ExitOK
	for _, acct := range accts {
		if acct.IsMain() {
			continue
		}
		if !a.syncOne(acct) {
			code = ExitError
		}
	}
	return code
}

// syncOne syncs one account, prints a summary line and reports success.
func (a *App) syncOne(acct registry.Account) bool {
	ok := true
	var parts []string
	if !a.migrateHistory(acct) {
		ok = false
	}
	linked, err := links.LinkMissing(a.Paths, acct.Dir)
	if err != nil {
		a.warnf("%s: link shared config: %v", acct.Name, err)
		ok = false
	}
	if len(linked) > 0 {
		parts = append(parts, "linked "+strings.Join(linked, ", "))
	}
	changed, err := mcp.Sync(a.Paths, acct.Dir)
	if err != nil {
		a.warnf("%s: mcp sync: %v", acct.Name, err)
		ok = false
	}
	if changed {
		parts = append(parts, "MCP servers synced")
	}
	if len(parts) == 0 && ok {
		parts = append(parts, "up to date")
	}
	if len(parts) > 0 {
		a.printf("%s: %s\n", acct.Name, strings.Join(parts, "; "))
	}
	return ok
}

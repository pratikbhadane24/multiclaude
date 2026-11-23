package cli

import (
	"context"

	"github.com/pratikbhadane24/multiclaude/internal/shellenv"
)

// cmdUse prints the shell snippet that activates an account.
func (a *App) cmdUse(_ context.Context, args []string) int {
	pos, code, ok := a.parseArgs(a.newFlags("use"), args, 1, "use <name>")
	if !ok {
		return code
	}
	acct, code, ok := a.findAccount(pos[0])
	if !ok {
		return code
	}
	a.printf("%s\n", shellenv.UseScript(acct))
	return ExitOK
}

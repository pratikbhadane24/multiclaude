package cli

import (
	"context"

	"multiclaude/internal/shellenv"
)

// cmdShellInit prints the shell integration snippet.
func (a *App) cmdShellInit(_ context.Context, args []string) int {
	pos, code, ok := a.parseArgs(a.newFlags("shell-init"), args, 1, "shell-init zsh")
	if !ok {
		return code
	}
	script, err := shellenv.ShellInit(pos[0], a.Paths.AccountsRoot)
	if err != nil {
		return a.errorf("%v", err)
	}
	a.printf("%s", script)
	return ExitOK
}

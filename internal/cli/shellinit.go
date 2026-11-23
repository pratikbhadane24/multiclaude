package cli

import (
	"context"

	"github.com/pratikbhadane24/multiclaude/internal/shellenv"
)

// cmdShellInit prints the shell integration snippet.
func (a *App) cmdShellInit(_ context.Context, args []string) int {
	fs := a.newFlags("shell-init")
	worker := fs.Bool("worker", false, "also define claude() to run the ccm shim (workers)")
	pos, code, ok := a.parseArgs(fs, args, 1, "shell-init zsh [--worker]")
	if !ok {
		return code
	}
	build := shellenv.ShellInit
	if *worker {
		build = shellenv.ShellInitWorker
	}
	script, err := build(pos[0], a.Paths.AccountsRoot)
	if err != nil {
		return a.errorf("%v", err)
	}
	a.printf("%s", script)
	return ExitOK
}

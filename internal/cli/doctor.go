package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"

	"multiclaude/internal/doctor"
	"multiclaude/internal/keychain"
	"multiclaude/internal/links"
	"multiclaude/internal/mcp"
	"multiclaude/internal/registry"
	"multiclaude/internal/t3"
)

// cmdDoctor handles `ccm doctor [--fix]`.
func (a *App) cmdDoctor(ctx context.Context, args []string) int {
	fs := a.newFlags("doctor")
	fix := fs.Bool("fix", false, "repair shared links and MCP drift")
	if _, code, ok := a.parseArgs(fs, args, 0, "doctor [--fix]"); !ok {
		return code
	}
	accts, err := registry.Load(a.Paths)
	if err != nil {
		return a.errorf("%v", err)
	}
	code, text := doctor.Summary(doctor.Run(ctx, a.doctorDeps(accts), *fix))
	a.printf("%s", text)
	return code
}

func (a *App) doctorDeps(accts []registry.Account) doctor.Deps {
	return doctor.Deps{
		Paths:        a.Paths,
		Accounts:     accts,
		Now:          a.Now,
		Stat:         os.Stat,
		EvalSymlinks: filepath.EvalSymlinks,
		LookPath:     exec.LookPath,
		Inspect:      links.Inspect,
		LinkMissing:  links.LinkMissing,
		FixReplaced:  links.FixReplaced,
		MCPDrift:     mcp.Drift,
		MCPSync:      mcp.Sync,
		KeychainPresent: func(ctx context.Context, acct registry.Account) (bool, error) {
			return keychain.Present(ctx, a.Runner, acct)
		},
		T3HasInstance: func(acct registry.Account) (bool, error) {
			return t3.HasInstance(a.Paths, acct, a.Paths.ShimPath)
		},
	}
}

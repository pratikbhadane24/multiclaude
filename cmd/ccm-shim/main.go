// Command ccm-shim is installed as ~/.claude-accounts/bin/claude. It syncs
// MCP servers into the active account and then execs the real claude.
package main

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"multiclaude/internal/links"
	"multiclaude/internal/paths"
	"multiclaude/internal/shim"
)

// exitCannotStart is the shell's "command not found" status.
const exitCannotStart = 127

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ccm: cannot start claude: home dir: %v\n", err)
		os.Exit(exitCannotStart)
	}
	deps := shim.Deps{
		Paths:   paths.Resolve(os.Getenv, home),
		Exec:    syscall.Exec,
		Stderr:  os.Stderr,
		Inspect: links.Inspect,
		Self:    os.Executable,
	}
	err = shim.Main(os.Args[1:], os.Environ(), deps)
	if err == nil {
		return
	}
	if errors.Is(err, shim.ErrSelfExec) {
		fmt.Fprintln(os.Stderr, err)
	} else {
		fmt.Fprintf(os.Stderr, "ccm: cannot start claude: %v\n", err)
	}
	os.Exit(exitCannotStart)
}

package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"multiclaude/internal/registry"
)

// ExitUsage is returned for unknown commands and bad arguments (EX_USAGE).
const ExitUsage = 64

const usageText = `Usage: ccm <command> [args]

Run ccm with no arguments in a terminal to open the dashboard.

Commands:
  dash                          live dashboard: every action on a key
  add <name> [--email e] [--open mode]   create an account and log it in
  login <name> [--open mode] | --all    log in one account, or every logged-out one
  list                          list accounts and their login state
  run <name> [claude args...]   run claude as an account
  <name> [claude args...]       same as run <name>
  best [claude args...]         run claude as the account with most headroom
  use <name>                    print shell code that switches account
  continue [--from a] [--to b] [--session id]   resume this folder's latest chat on another account
  status [--json] [--watch d] [--refresh-cache]
                                show plan usage per account (watch: default 30s)
  statusline [install|uninstall] Claude status line segment (install wraps your current one)
  sync                          link shared config and sync MCP servers
  t3-sync                       add every account to T3 Code (and sync launch args)
  config show                   show default and per-account claude args
  config set default-args <args...>      args added to every launch (e.g. --dangerously-skip-permissions)
  config set args <account> <args...>    extra args for one account
  config clear default-args | args <account>
  doctor [--fix]                check (and repair) the setup
  remove <name> [--purge]       unregister an account (--purge deletes its data)
  shell-init zsh                print shell integration for ~/.zshrc
  install                       install the shim and the ccm link
  help                          show this help

Login link (--open copy|incognito|chrome:<Profile dir>):
  copy (default)   the link is copied to the clipboard; the default browser is never auto-opened
  incognito        open the link in a Google Chrome incognito window
  chrome:<dir>     open the link in that Chrome profile directory (e.g. "Profile 2")
  The chosen mode is remembered for the account.
`

type handler func(a *App, ctx context.Context, args []string) int

var commands = map[string]handler{
	"add":        (*App).cmdAdd,
	"login":      (*App).cmdLogin,
	"list":       (*App).cmdList,
	"run":        (*App).cmdRun,
	"best":       (*App).cmdBest,
	"use":        (*App).cmdUse,
	"continue":   (*App).cmdContinue,
	"status":     (*App).cmdStatus,
	"statusline": (*App).cmdStatusline,
	"sync":       (*App).cmdSync,
	"t3-sync":    (*App).cmdT3Sync,
	"config":     (*App).cmdConfig,
	"doctor":     (*App).cmdDoctor,
	"remove":     (*App).cmdRemove,
	"shell-init": (*App).cmdShellInit,
	"install":    (*App).cmdInstall,
	"dash":       (*App).cmdDash,
}

// Main dispatches args (without the program name) and returns the exit code.
func (a *App) Main(ctx context.Context, args []string) int {
	if len(args) == 0 {
		if a.hasTTY() {
			return a.cmdDash(ctx, nil)
		}
		fmt.Fprint(a.Err, usageText)
		return ExitUsage
	}
	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprint(a.Out, usageText)
		return ExitOK
	}
	if h, ok := commands[args[0]]; ok {
		return h(a, ctx, args[1:])
	}
	return a.runRegistered(ctx, args[0], args[1:])
}

// runRegistered runs `ccm <name> args...` when name is a registered account.
func (a *App) runRegistered(ctx context.Context, name string, args []string) int {
	accts, err := registry.Load(a.Paths)
	if err != nil {
		// Not a known command and accounts are unreadable: report usage, not a registry failure.
		fmt.Fprintf(a.Err, "ccm: unknown command %q (account registry unreadable: %v)\n\n%s", name, err, usageText)
		return ExitUsage
	}
	if acct, ok := registry.Find(accts, name); ok {
		return a.runAccount(ctx, acct, args)
	}
	fmt.Fprintf(a.Err, "ccm: unknown command %q\n\n%s", name, usageText)
	return ExitUsage
}

// usageErr prints a one-line usage message and returns ExitUsage.
func (a *App) usageErr(line string) int {
	fmt.Fprintf(a.Err, "usage: ccm %s\n", line)
	return ExitUsage
}

// newFlags returns a flag set that reports errors to a.Err.
func (a *App) newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet("ccm "+name, flag.ContinueOnError)
	fs.SetOutput(a.Err)
	return fs
}

// parseInterspersed parses flags anywhere among args and returns the
// positional arguments. A "--" ends flag parsing.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return pos, nil
		}
		consumed := len(args) - len(rest)
		if consumed > 0 && args[consumed-1] == "--" {
			return append(pos, rest...), nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

// flagExit maps a flag parse error to an exit code (-h is not an error).
func flagExit(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return ExitOK
	}
	return ExitUsage
}

// parseArgs parses fs over args and checks the positional count.
func (a *App) parseArgs(fs *flag.FlagSet, args []string, want int, usage string) ([]string, int, bool) {
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return nil, flagExit(err), false
	}
	if len(pos) != want {
		return nil, a.usageErr(usage), false
	}
	return pos, ExitOK, true
}

// findAccount loads the registry and looks up name.
func (a *App) findAccount(name string) (registry.Account, int, bool) {
	accts, err := registry.Load(a.Paths)
	if err != nil {
		return registry.Account{}, a.errorf("%v", err), false
	}
	acct, ok := registry.Find(accts, name)
	if !ok {
		return registry.Account{}, a.errorf("unknown account %q", name), false
	}
	return acct, ExitOK, true
}

// interruptFree lists commands that never block on input or a child, so the
// default SIGINT behaviour (terminate) is right for them.
var interruptFree = map[string]bool{
	"list": true, "use": true, "sync": true, "t3-sync": true, "doctor": true,
	"shell-init": true, "install": true, "config": true, "help": true, "-h": true, "--help": true,
}

// HandlesInterrupt reports whether the command wants SIGINT delivered as a
// cancelled context (prompts, watch loops, foreground children) instead of
// terminating the process.
func HandlesInterrupt(args []string) bool {
	return len(args) > 0 && !interruptFree[args[0]]
}

package cli

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"multiclaude/internal/config"
)

const configUsage = "config show | set default-args <args...> | set args <account> <args...> | clear default-args | clear args <account>"

// cmdConfig handles `ccm config ...`. Args after the key are taken verbatim,
// including ones that look like flags.
func (a *App) cmdConfig(_ context.Context, args []string) int {
	if len(args) == 0 {
		return a.usageErr(configUsage)
	}
	cfg, err := config.Load(a.Paths)
	if err != nil {
		return a.errorf("%v", err)
	}
	switch args[0] {
	case "show":
		return a.configShow(cfg, args[1:])
	case "set":
		return a.configSet(cfg, args[1:])
	case "clear":
		return a.configClear(cfg, args[1:])
	}
	return a.usageErr(configUsage)
}

func (a *App) configShow(cfg config.Config, rest []string) int {
	if len(rest) != 0 {
		return a.usageErr("config show")
	}
	a.printf("default-args: %s\n", orNone(config.ShellJoin(cfg.DefaultArgs)))
	for _, name := range slices.Sorted(maps.Keys(cfg.Accounts)) {
		a.printf("args %s: %s\n", name, orNone(config.ShellJoin(cfg.Accounts[name].Args)))
	}
	return ExitOK
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

func (a *App) configSet(cfg config.Config, rest []string) int {
	switch {
	case len(rest) >= 2 && rest[0] == "default-args":
		next := cfg
		next.DefaultArgs = slices.Clone(rest[1:])
		return a.saveConfig(next)
	case len(rest) >= 3 && rest[0] == "args":
		if _, code, ok := a.findAccount(rest[1]); !ok {
			return code
		}
		return a.saveConfig(withAccountArgs(cfg, rest[1], slices.Clone(rest[2:])))
	}
	return a.usageErr("config set default-args <args...> | config set args <account> <args...>")
}

func (a *App) configClear(cfg config.Config, rest []string) int {
	switch {
	case len(rest) == 1 && rest[0] == "default-args":
		next := cfg
		next.DefaultArgs = nil
		return a.saveConfig(next)
	case len(rest) == 2 && rest[0] == "args":
		return a.saveConfig(withAccountArgs(cfg, rest[1], nil))
	}
	return a.usageErr("config clear default-args | config clear args <account>")
}

// withAccountArgs returns cfg with the account's args replaced; empty removes the entry.
func withAccountArgs(cfg config.Config, name string, args []string) config.Config {
	accounts := maps.Clone(cfg.Accounts)
	if accounts == nil {
		accounts = map[string]config.AccountConfig{}
	}
	if len(args) == 0 {
		delete(accounts, name)
	} else {
		accounts[name] = config.AccountConfig{Args: args}
	}
	next := cfg
	next.Accounts = accounts
	return next
}

func (a *App) saveConfig(cfg config.Config) int {
	if err := config.Save(a.Paths, cfg); err != nil {
		return a.errorf("%v", err)
	}
	fmt.Fprintln(a.Out, "ok")
	return ExitOK
}

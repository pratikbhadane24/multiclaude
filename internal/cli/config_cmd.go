package cli

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/pratikbhadane24/multiclaude/internal/config"
)

const configUsage = "config show | set default-args <args...> | set args <account> <args...> | set auto-switch on|off | " +
	"set auto-switch-prompt <text> | clear default-args | clear args <account> | clear auto-switch-prompt"

// Config keys for auto-switch.
const (
	keyAutoSwitch       = "auto-switch"
	keyDefaultAccount   = "default-account"
	keyAutoSwitchPrompt = "auto-switch-prompt"
)

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
	a.printf("auto-switch: %s\n", onOff(cfg.AutoSwitch))
	a.printf("auto-switch-prompt: %s\n", orDefault(cfg.AutoSwitchPrompt))
	return ExitOK
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func orDefault(s string) string {
	if s == "" {
		return "(default)"
	}
	return s
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
	case len(rest) == 2 && rest[0] == keyAutoSwitch:
		return a.setAutoSwitch(cfg, rest[1])
	case len(rest) == 2 && rest[0] == keyDefaultAccount:
		return a.setDefaultAccount(cfg, rest[1])
	case len(rest) >= 2 && rest[0] == keyAutoSwitchPrompt:
		next := cfg
		next.AutoSwitchPrompt = strings.Join(rest[1:], " ")
		return a.saveConfig(next)
	}
	return a.usageErr("config set default-args <args...> | config set args <account> <args...> | " +
		"config set auto-switch on|off | config set auto-switch-prompt <text> | config set default-account best|off")
}

// setDefaultAccount turns shim auto-pick on ("best") or off.
func (a *App) setDefaultAccount(cfg config.Config, value string) int {
	next := cfg
	switch value {
	case config.DefaultAccountBest:
		next.DefaultAccount = config.DefaultAccountBest
	case "off":
		next.DefaultAccount = ""
	default:
		return a.usageErr("config set default-account best|off")
	}
	return a.saveConfig(next)
}

// setAutoSwitch stores the auto-switch setting ("on" or "off").
func (a *App) setAutoSwitch(cfg config.Config, value string) int {
	next := cfg
	switch value {
	case "on":
		next.AutoSwitch = true
	case "off":
		next.AutoSwitch = false
	default:
		return a.usageErr("config set auto-switch on|off")
	}
	return a.saveConfig(next)
}

func (a *App) configClear(cfg config.Config, rest []string) int {
	switch {
	case len(rest) == 1 && rest[0] == "default-args":
		next := cfg
		next.DefaultArgs = nil
		return a.saveConfig(next)
	case len(rest) == 2 && rest[0] == "args":
		return a.saveConfig(withAccountArgs(cfg, rest[1], nil))
	case len(rest) == 1 && rest[0] == keyAutoSwitchPrompt:
		next := cfg
		next.AutoSwitchPrompt = ""
		return a.saveConfig(next)
	}
	return a.usageErr("config clear default-args | config clear args <account> | config clear auto-switch-prompt")
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

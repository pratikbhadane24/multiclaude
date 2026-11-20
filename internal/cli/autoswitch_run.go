package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"multiclaude/internal/auth"
	"multiclaude/internal/autoswitch"
	"multiclaude/internal/config"
	"multiclaude/internal/handoff"
	"multiclaude/internal/registry"
)

// startProc starts claude for the auto-switch supervisor (a fake in tests).
func (a *App) startProc(bin string, args, env []string) (autoswitch.Proc, error) {
	if a.StartProc != nil {
		return a.StartProc(bin, args, env)
	}
	return autoswitch.StartProcess(autoswitch.OSStdio(), bin, args, env)
}

// runSupervised runs claude as acct under the auto-switch supervisor when
// the setting is on; ok is false when it is off or this launch cannot be
// supervised (then the caller launches claude plainly).
func (a *App) runSupervised(ctx context.Context, acct registry.Account, typed []string) (code int, ok bool) {
	cfg, err := config.Load(a.Paths)
	if err != nil || !cfg.AutoSwitch {
		return 0, false
	}
	cwd, err := realWd()
	if err != nil {
		a.warnf("auto-switch off for this launch: %v", err)
		return 0, false
	}
	latest := func() (string, error) { return latestIn(a, acct, cwd) }
	args, id, err := autoswitch.ResolveSession(config.MergedArgs(cfg, acct.Name, typed), newSessionID, latest)
	if err != nil {
		a.warnf("auto-switch off for this launch: %v", err)
		return 0, false
	}
	env, removed := autoswitch.StripCredentialEnv(os.Environ())
	if len(removed) > 0 {
		a.warnf("auto-switch: not passing %s to claude, so each account uses its own login", strings.Join(removed, ", "))
	}
	return autoswitch.Run(ctx, a.autoSwitchDeps(cfg, cwd, env), acct, args, id), true
}

// realWd is the working directory with symlinks resolved, as claude's own
// getcwd() reports it (and so names its project directory).
func realWd() (string, error) {
	cwd, err := getwd()
	if err != nil {
		return "", fmt.Errorf("current directory: %w", err)
	}
	if real, err := filepath.EvalSymlinks(cwd); err == nil {
		return real, nil
	}
	return cwd, nil
}

// latestIn is the newest conversation for cwd (what --continue would pick).
func latestIn(a *App, acct registry.Account, cwd string) (string, error) {
	s, err := handoff.FindLatest(a.Paths, acct, cwd)
	if err != nil {
		return "", err
	}
	return s.ID, nil
}

// autoSwitchDeps wires the supervisor to the usage cache, the config's args
// and the real claude binary, launched with env (credential variables
// already removed).
func (a *App) autoSwitchDeps(cfg config.Config, cwd string, env []string) autoswitch.Deps {
	d := autoswitch.NewDeps(a.Paths)
	d.Now, d.Err, d.PromptOverride = a.Now, a.Err, cfg.AutoSwitchPrompt
	d.ProjectDir = func(acct registry.Account) string {
		return autoswitch.ProjectDir(handoff.ConfigDir(a.Paths, acct), cwd)
	}
	d.RelaunchArgs = func(acct registry.Account, id, prompt string) []string {
		return autoswitch.RelaunchArgs(config.MergedArgs(cfg, acct.Name, nil), id, prompt)
	}
	d.Start = func(acct registry.Account, args []string) (autoswitch.Proc, error) {
		return a.startProc(auth.Binary(a.Paths), args, auth.Env(acct, env))
	}
	return d
}

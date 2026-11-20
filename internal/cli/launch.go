package cli

import (
	"fmt"
	"os"
	"os/exec"

	"multiclaude/internal/auth"
	"multiclaude/internal/config"
	"multiclaude/internal/registry"
)

// launch is one interactive claude launch: binary, args (without argv[0]) and env.
type launch struct {
	Bin  string
	Args []string
	Env  []string
}

// launchArgs merges the configured default and per-account args before
// extra. A broken config is returned as an error alongside extra alone, so
// the caller can still launch.
func (a *App) launchArgs(name string, extra []string) ([]string, error) {
	cfg, err := config.Load(a.Paths)
	if err != nil {
		return config.MergedArgs(config.Config{}, name, extra), err
	}
	return config.MergedArgs(cfg, name, extra), nil
}

// buildLaunch is the single launch builder used by `ccm <name>`, run, best,
// continue and the dashboard, so they always produce the same argv and env.
func (a *App) buildLaunch(acct registry.Account, extra []string) (launch, error) {
	args, err := a.launchArgs(acct.Name, extra)
	return launch{Bin: auth.Binary(a.Paths), Args: args, Env: auth.Env(acct, os.Environ())}, err
}

// LaunchCmd builds the command `ccm <name> [extra...]` runs, for the
// dashboard. Stdio is left unset for tea.ExecProcess to wire. A broken config
// cannot be printed under the dashboard, so it launches without config args
// (as `ccm <name>` does after its warning) and returns the config error text
// as warning for the dashboard to show.
func (a *App) LaunchCmd(name string, extra []string) (c *exec.Cmd, warning string, err error) {
	accts, err := registry.Load(a.Paths)
	if err != nil {
		return nil, "", err
	}
	acct, ok := registry.Find(accts, name)
	if !ok {
		return nil, "", fmt.Errorf("unknown account %q", name)
	}
	if c, ok := a.supervisedLaunchCmd(name, extra); ok {
		return c, "", nil
	}
	l, cfgErr := a.buildLaunch(acct, extra)
	if cfgErr != nil {
		warning = cfgErr.Error()
	}
	c = exec.Command(l.Bin, l.Args...)
	c.Env = l.Env
	return c, warning, nil
}

// supervisedLaunchCmd is `ccm run <name> extra...` when auto-switch is on and
// the ccm binary is known, so dashboard launches get the same supervisor.
func (a *App) supervisedLaunchCmd(name string, extra []string) (*exec.Cmd, bool) {
	cfg, err := config.Load(a.Paths)
	if err != nil || !cfg.AutoSwitch || a.SelfExe == "" {
		return nil, false
	}
	c := exec.Command(a.SelfExe, append([]string{"run", name}, extra...)...)
	c.Env = os.Environ()
	return c, true
}

// dashArgsFor is the merged launch args shown by the dashboard (details pane
// and the skip-permissions badge); the error means the config cannot be read.
func (a *App) dashArgsFor(name string) ([]string, error) {
	args, err := a.launchArgs(name, nil)
	if err != nil {
		return nil, err
	}
	return args, nil
}

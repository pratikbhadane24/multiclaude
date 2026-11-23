package sdkproxy

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/pratikbhadane24/multiclaude/internal/config"
	"github.com/pratikbhadane24/multiclaude/internal/paths"
	"github.com/pratikbhadane24/multiclaude/internal/registry"
	"github.com/pratikbhadane24/multiclaude/internal/status"
)

// Supervisor returns the shim's hook: for an SDK stream-json invocation with
// auto-switch on (and CCM_NO_PROXY unset) it runs the proxy on the process's
// stdio and returns its exit code with ok true; otherwise ok is false and
// the shim execs claude as usual. prepare readies a non-main account
// directory before a child starts there.
func Supervisor(p paths.Paths, stderr io.Writer) func(args, env []string, prepare func(dir string)) (int, bool) {
	return func(args, env []string, prepare func(dir string)) (int, bool) {
		if !IsStreamJSON(args) {
			return 0, false // cheap check first: no config read for normal runs
		}
		cfg, err := config.Load(p)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "ccm: warning: auto-switch off: %v\n", err)
			return 0, false
		}
		if !Enabled(args, env, cfg.AutoSwitch) {
			return 0, false
		}
		return Run(args, env, liveDeps(p, stderr, cfg.AutoSwitchPrompt, prepare)), true
	}
}

// liveDeps wires the proxy to the real process, registry and usage cache.
func liveDeps(p paths.Paths, stderr io.Writer, prompt string, prepare func(string)) Deps {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	return Deps{
		Paths: p, Start: ExecStarter(stderr), Clock: RealClock{},
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: stderr, Signals: sigs,
		Accounts:  func() ([]registry.Account, error) { return registry.Load(p) },
		ReadCache: func() (status.Cache, error) { return status.ReadCache(p) },
		MarkLimited: func(name string, l Limit) error {
			return status.MarkLimitedType(p, name, l.Type, l.ResetsAt)
		},
		ResumePrompt: prompt,
		Prepare:      prepare,
	}
}

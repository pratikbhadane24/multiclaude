// Package shim is the launcher installed as ~/.claude-accounts/bin/claude.
// It syncs MCP servers into the active account, warns about broken shared
// links, then execs the real claude. Nothing here may stop Claude starting
// except being unable to find a real claude that is not the shim itself.
package shim

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/links"
	"github.com/pratikbhadane24/multiclaude/internal/mcp"
	"github.com/pratikbhadane24/multiclaude/internal/paths"
)

const (
	configDirEnv = "CLAUDE_CONFIG_DIR"
	claudeArgv0  = "claude"
	warnPrefix   = "ccm: warning: "
)

// ErrSelfExec means the real claude path resolves to the shim itself.
var ErrSelfExec = errors.New("ccm: real claude resolves to the shim; set CCM_CLAUDE_BIN")

// Deps are the side effects Main needs, injectable for tests.
type Deps struct {
	Paths   paths.Paths
	Exec    func(argv0 string, argv []string, env []string) error // syscall.Exec in prod
	Stderr  io.Writer
	Inspect func(p paths.Paths, dir string) ([]links.Item, error)
	Self    func() (string, error) // os.Executable in prod; nil skips that check
	// Supervise, when set, may run claude as a supervised child instead of
	// exec'ing it (SDK auto-switch). ok false means "not applicable: exec".
	// prepare readies another account's directory before a child starts.
	Supervise func(args, env []string, prepare func(dir string)) (code int, ok bool)
	// Exit ends the process with a supervised run's exit code.
	Exit func(code int)
}

// Main runs the shim. args excludes the program name; env is passed to the
// real claude unchanged. It returns only if exec fails or is refused.
func Main(args []string, env []string, deps Deps) error {
	p := deps.Paths
	env = safeAutoPick(p, env, deps.Stderr)
	if dir, ok := accountDir(env, p); ok {
		prepareAccount(p, dir, deps)
	}
	if err := checkNotSelf(p, deps.Self); err != nil {
		return err
	}
	if deps.Supervise != nil && deps.Exit != nil {
		prepare := func(dir string) { prepareAccount(p, dir, deps) }
		if code, ok := deps.Supervise(args, env, prepare); ok {
			deps.Exit(code)
			return nil
		}
	}
	argv := append([]string{claudeArgv0}, args...)
	if err := deps.Exec(p.RealClaude, argv, env); err != nil {
		return fmt.Errorf("exec %s: %w", p.RealClaude, err)
	}
	return nil
}

// safeAutoPick applies AutoPick; a panic keeps env unchanged so launching
// is never blocked.
func safeAutoPick(p paths.Paths, env []string, stderr io.Writer) (out []string) {
	out = env
	defer func() {
		if r := recover(); r != nil {
			warn(stderr, "account auto-pick skipped: %v", r)
			out = env
		}
	}()
	return AutoPick(p, env, time.Now())
}

// accountDir returns the normalized CLAUDE_CONFIG_DIR when it names a
// non-main account.
func accountDir(env []string, p paths.Paths) (string, bool) {
	raw := lookupEnv(env, configDirEnv)
	if raw == "" {
		return "", false
	}
	dir := paths.Normalize(raw, p.Home)
	if dir == paths.Normalize(p.ClaudeDir, p.Home) {
		return "", false
	}
	return dir, true
}

// lookupEnv returns the last value of key in env (as getenv would).
func lookupEnv(env []string, key string) string {
	prefix := key + "="
	val := ""
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			val = kv[len(prefix):]
		}
	}
	return val
}

// prepareAccount syncs MCP servers and reports replaced links. Failures are
// only warnings: they must never block launching Claude.
func prepareAccount(p paths.Paths, dir string, deps Deps) {
	defer func() {
		if r := recover(); r != nil {
			warn(deps.Stderr, "internal error during sync: %v", r)
		}
	}()
	if _, err := mcp.Sync(p, dir); err != nil {
		warn(deps.Stderr, "MCP sync skipped: %v", err)
	}
	if deps.Inspect == nil {
		return
	}
	items, err := deps.Inspect(p, dir)
	if err != nil {
		warn(deps.Stderr, "cannot check shared links: %v", err)
		return
	}
	for _, it := range items {
		if it.State == links.StateReplaced {
			warn(deps.Stderr, "%s is no longer shared — run ccm doctor", it.Name)
		}
	}
}

func warn(w io.Writer, format string, a ...any) {
	if w == nil {
		return
	}
	_, _ = fmt.Fprintf(w, warnPrefix+format+"\n", a...)
}

// checkNotSelf refuses to exec when RealClaude resolves to the installed shim
// or to the running executable, which would loop forever. Candidates that
// cannot be resolved (e.g. no shim installed) cannot be the target, so pass.
func checkNotSelf(p paths.Paths, self func() (string, error)) error {
	real, err := filepath.EvalSymlinks(p.RealClaude)
	if err != nil {
		return fmt.Errorf("resolve real claude %s: %w", p.RealClaude, err)
	}
	candidates := []string{p.ShimPath}
	if self != nil {
		if exe, err := self(); err == nil {
			candidates = append(candidates, exe)
		}
	}
	for _, c := range candidates {
		if resolved, err := filepath.EvalSymlinks(c); err == nil && resolved == real {
			return ErrSelfExec
		}
	}
	return nil
}

// Package statusline renders the Claude Code status line segment for ccm.
// The render path is cache-only: it never reads the Keychain or the network.
package statusline

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"multiclaude/internal/consts"
	"multiclaude/internal/paths"
	"multiclaude/internal/registry"
	"multiclaude/internal/status"
)

const (
	configDirEnv   = "CLAUDE_CONFIG_DIR"
	separator      = " │ "
	staleAfter     = 60 * time.Second
	lockFreshFor   = 60 * time.Second
	wrapTimeout    = time.Second
	maxStdin       = 1 << 20
	fullPct        = 100.0
	limitedMark    = "⛔"
	lockFileName   = "refresh.lock"
	cacheDirMode   = 0o700
	lockFileMode   = 0o600
	waitDelay      = 50 * time.Millisecond
	loggedOutState = "logged-out"
)

// Deps are the injected collaborators of Render.
type Deps struct {
	Paths     paths.Paths
	Now       func() time.Time
	ReadCache func() (status.Cache, error)
	// Wrap is the previous status line command; empty means none.
	Wrap string
	// RunWrap runs Wrap with stdin and returns its stdout.
	RunWrap func(cmd string, stdin []byte) (string, error)
	// SpawnRefresh starts a detached cache refresh (already lock-guarded).
	SpawnRefresh func() error
}

// Account maps CLAUDE_CONFIG_DIR to a registry account name: unset or the
// default dir is main, a registered dir its name, anything else its basename.
func Account(p paths.Paths, configDir string) string {
	if configDir == "" {
		return consts.MainName
	}
	dir := paths.Normalize(configDir, p.Home)
	if dir == paths.Normalize(p.ClaudeDir, p.Home) {
		return consts.MainName
	}
	if accts, err := registry.Load(p); err == nil {
		for _, a := range accts {
			if !a.IsMain() && a.Dir != "" && paths.Normalize(a.Dir, p.Home) == dir {
				return a.Name
			}
		}
	}
	return filepath.Base(dir)
}

func segment(label string, used *float64) string {
	if used == nil {
		return ""
	}
	s := fmt.Sprintf("%s %.0f%%", label, *used)
	if *used >= fullPct {
		s += " " + limitedMark
	}
	return s
}

func ccmPart(name string, c *status.CacheAccount) string {
	out := "ccm: " + name
	if c == nil {
		return out
	}
	if c.State == loggedOutState {
		return out + " (logged out)"
	}
	var segs []string
	for _, s := range []string{segment("5h", c.FiveHourUsed), segment("7d", c.SevenDayUsed)} {
		if s != "" {
			segs = append(segs, s)
		}
	}
	if len(segs) == 0 {
		return out
	}
	return out + " " + strings.Join(segs, " · ")
}

func findAccount(c status.Cache, name string) *status.CacheAccount {
	for i := range c.Accounts {
		if c.Accounts[i].Name == name {
			return &c.Accounts[i]
		}
	}
	return nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimLeft(s, "\r\n"), "\n")
	return strings.TrimSpace(line)
}

func wrapped(in io.Reader, d Deps) string {
	if d.Wrap == "" || d.RunWrap == nil {
		return ""
	}
	stdin, _ := io.ReadAll(io.LimitReader(in, maxStdin))
	out, err := d.RunWrap(d.Wrap, stdin)
	if err != nil {
		return ""
	}
	return firstLine(out)
}

// Render writes the single status line and returns the exit code (always 0:
// a status line must never break Claude's UI).
func Render(in io.Reader, env func(string) string, d Deps, out io.Writer) int {
	name := Account(d.Paths, env(configDirEnv))
	prefix := wrapped(in, d)
	cache, err := d.ReadCache()
	var acct *status.CacheAccount
	if err == nil {
		acct = findAccount(cache, name)
	}
	if (err != nil || d.Now().Sub(cache.WrittenAt) > staleAfter) && d.SpawnRefresh != nil {
		_ = d.SpawnRefresh()
	}
	line := ccmPart(name, acct)
	if prefix != "" {
		line = prefix + separator + line
	}
	fmt.Fprintln(out, line)
	return 0
}

// RunShell runs `/bin/sh -c cmd` with stdin, returning stdout; it is killed
// after timeout.
func RunShell(cmd string, stdin []byte, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	c := exec.CommandContext(ctx, "/bin/sh", "-c", cmd)
	c.Stdin = bytes.NewReader(stdin)
	var buf bytes.Buffer
	c.Stdout = &buf
	c.WaitDelay = waitDelay
	if err := c.Run(); err != nil {
		return buf.String(), err
	}
	return buf.String(), nil
}

// DefaultRunWrap runs a wrapped command with the standard 1s timeout.
func DefaultRunWrap(cmd string, stdin []byte) (string, error) {
	return RunShell(cmd, stdin, wrapTimeout)
}

// NewSpawner guards start with <AccountsRoot>/cache/refresh.lock: a lock whose
// mtime is under 60s old means a refresh is already running.
func NewSpawner(p paths.Paths, now func() time.Time, start func() error) func() error {
	lock := filepath.Join(p.AccountsRoot, "cache", lockFileName)
	return func() error {
		if st, err := os.Stat(lock); err == nil && now().Sub(st.ModTime()) < lockFreshFor {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(lock), cacheDirMode); err != nil {
			return err
		}
		if err := os.WriteFile(lock, nil, lockFileMode); err != nil {
			return err
		}
		stamp := now()
		_ = os.Chtimes(lock, stamp, stamp)
		return start()
	}
}

// StartDetached launches `exe status --refresh-cache` in its own session
// without CLAUDE_CONFIG_DIR, so the child never blocks the caller.
func StartDetached(exe string) error {
	c := exec.Command(exe, "status", "--refresh-cache")
	c.Env = envWithout(os.Environ(), configDirEnv)
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := c.Start(); err != nil {
		return err
	}
	return c.Process.Release()
}

func envWithout(env []string, key string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(kv, key+"=") {
			out = append(out, kv)
		}
	}
	return out
}

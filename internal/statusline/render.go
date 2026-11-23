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

	"github.com/pratikbhadane24/multiclaude/internal/consts"
	"github.com/pratikbhadane24/multiclaude/internal/paths"
	"github.com/pratikbhadane24/multiclaude/internal/registry"
	"github.com/pratikbhadane24/multiclaude/internal/status"
)

const (
	configDirEnv = "CLAUDE_CONFIG_DIR"
	separator    = " │ "
	// staleAfter matches the fetch cache's min interval: a younger cache
	// would only be served back from itself.
	staleAfter     = status.MinFetchInterval
	lockFreshFor   = 60 * time.Second
	wrapTimeout    = time.Second
	maxStdin       = 1 << 20
	fullPct        = 100.0
	limitedMark    = "⛔"
	lowMark        = "⚠"
	lockFileName   = "refresh.lock"
	cacheDirMode   = 0o700
	lockFileMode   = 0o600
	waitDelay      = 50 * time.Millisecond
	loggedOutState = "logged-out"
	// staleMark follows usage kept from an earlier fetch (the latest failed).
	staleMark = "~"
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

// segment renders one window. A nil window (or 0% with no reset) on a usable
// account is idle, and so is a window that already reset since the cache was
// written. live reports whether the text shows a cached percentage.
func segment(label string, used *float64, resets *time.Time, usable bool, now time.Time) (text string, live bool) {
	elapsed := resets != nil && !now.Before(*resets)
	if used == nil || (*used == 0 && resets == nil) || elapsed {
		if usable {
			return label + " idle", false
		}
		return "", false
	}
	s := fmt.Sprintf("%s %.0f%%", label, *used)
	switch {
	case *used >= fullPct:
		s += " " + limitedMark
	case *used >= status.LowPct:
		s += " " + lowMark
	}
	return s, true
}

// isUsable reports whether a cached state carries real usage numbers.
func isUsable(state string) bool {
	switch state {
	case "ok", "expired-refreshed", status.StateLimited:
		return true
	}
	return false
}

func ccmPart(name string, c *status.CacheAccount, now time.Time) string {
	out := "ccm: " + name
	if c == nil {
		return out
	}
	if c.State == loggedOutState {
		return out + " (logged out)"
	}
	usable := isUsable(c.State)
	five, live5 := segment("5h", c.FiveHourUsed, c.FiveHourResetsAt, usable, now)
	seven, live7 := segment("7d", c.SevenDayUsed, c.SevenDayResetsAt, usable, now)
	var segs []string
	for _, s := range []string{five, seven} {
		if s != "" {
			segs = append(segs, s)
		}
	}
	if len(segs) == 0 {
		return out
	}
	out += " " + strings.Join(segs, " · ")
	// Only cached numbers can be out of date; idle windows carry none.
	if c.Stale && (live5 || live7) {
		out += " " + staleMark
	}
	return out
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
	line := ccmPart(name, acct, d.Now())
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
	// Own process group so a timeout kills the shell and everything it spawned.
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
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

func lockPath(p paths.Paths) string { return filepath.Join(p.AccountsRoot, "cache", lockFileName) }

// ReleaseLock removes the refresh lock (called by a finished refresher).
func ReleaseLock(p paths.Paths) { _ = os.Remove(lockPath(p)) }

// takeLock creates the lock exclusively; a lock older than lockFreshFor is
// stale and is removed and retaken once. It reports whether the lock is ours.
func takeLock(lock string, now time.Time) bool {
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, lockFileMode)
		if err == nil {
			_ = f.Close()
			_ = os.Chtimes(lock, now, now)
			return true
		}
		st, serr := os.Stat(lock)
		if !os.IsExist(err) || (serr == nil && now.Sub(st.ModTime()) < lockFreshFor) {
			return false
		}
		_ = os.Remove(lock)
	}
	return false
}

// NewSpawner guards start with <AccountsRoot>/cache/refresh.lock: a lock
// younger than 60s means a refresh is already running.
func NewSpawner(p paths.Paths, now func() time.Time, start func() error) func() error {
	lock := lockPath(p)
	return func() error {
		if err := os.MkdirAll(filepath.Dir(lock), cacheDirMode); err != nil {
			return err
		}
		if !takeLock(lock, now()) {
			return nil
		}
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

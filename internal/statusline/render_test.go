package statusline

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"multiclaude/internal/paths"
	"multiclaude/internal/registry"
	"multiclaude/internal/status"
)

var now = time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)

func pct(v float64) *float64 { return &v }

type harness struct {
	deps   Deps
	spawns int
	wraps  []string
	home   string
}

func newHarness(t *testing.T, cache *status.Cache) *harness {
	t.Helper()
	home := t.TempDir()
	h := &harness{home: home}
	p := paths.Paths{Home: home, ClaudeDir: filepath.Join(home, ".claude"), AccountsRoot: filepath.Join(home, "accts")}
	if err := registry.Save(p, []registry.Account{{Name: "main"}, {Name: "work", Dir: filepath.Join(home, "accts", "work")}}); err != nil {
		t.Fatal(err)
	}
	h.deps = Deps{
		Paths: p,
		Now:   func() time.Time { return now },
		ReadCache: func() (status.Cache, error) {
			if cache == nil {
				return status.Cache{}, os.ErrNotExist
			}
			return *cache, nil
		},
		SpawnRefresh: func() error { h.spawns++; return nil },
		RunWrap: func(cmd string, stdin []byte) (string, error) {
			h.wraps = append(h.wraps, cmd+"|"+string(stdin))
			return "CAVEMAN\nsecond line\n", nil
		},
	}
	return h
}

func freshCache() *status.Cache {
	return &status.Cache{WrittenAt: now.Add(-10 * time.Second), Accounts: []status.CacheAccount{
		{Name: "main", State: "ok", FiveHourUsed: pct(76), SevenDayUsed: pct(12)},
		{Name: "work", State: "limited", FiveHourUsed: pct(100), SevenDayUsed: pct(30)},
		{Name: "gone", State: "logged-out"},
	}}
}

func (h *harness) render(t *testing.T, stdin string, cfg string) string {
	t.Helper()
	var out bytes.Buffer
	env := func(k string) string {
		if k == "CLAUDE_CONFIG_DIR" {
			return cfg
		}
		return ""
	}
	if code := Render(strings.NewReader(stdin), env, h.deps, &out); code != 0 {
		t.Fatalf("code=%d", code)
	}
	return out.String()
}

func TestRenderNoWrap(t *testing.T) {
	h := newHarness(t, freshCache())
	got := h.render(t, "{}", "")
	if got != "ccm: main 5h 76% · 7d 12%\n" {
		t.Fatalf("got %q", got)
	}
	if h.spawns != 0 {
		t.Fatal("fresh cache must not spawn")
	}
}

func TestRenderWrapUsesFirstLineAndPassesStdin(t *testing.T) {
	h := newHarness(t, freshCache())
	h.deps.Wrap = "bash caveman.sh"

	got := h.render(t, `{"model":"x"}`, "")

	if got != "CAVEMAN │ ccm: main 5h 76% · 7d 12%\n" {
		t.Fatalf("got %q", got)
	}
	if len(h.wraps) != 1 || h.wraps[0] != `bash caveman.sh|{"model":"x"}` {
		t.Fatalf("wraps=%v", h.wraps)
	}
}

func TestRenderWrapFailureOrEmptyPrintsOnlyCcm(t *testing.T) {
	for name, fn := range map[string]func(string, []byte) (string, error){
		"timeout": func(string, []byte) (string, error) { return "partial", errors.New("timeout") },
		"empty":   func(string, []byte) (string, error) { return "  \n", nil },
	} {
		h := newHarness(t, freshCache())
		h.deps.Wrap, h.deps.RunWrap = "x", fn
		if got := h.render(t, "{}", ""); got != "ccm: main 5h 76% · 7d 12%\n" {
			t.Errorf("%s: got %q", name, got)
		}
	}
}

func TestRenderLimitedAndStates(t *testing.T) {
	h := newHarness(t, freshCache())
	work := filepath.Join(h.home, "accts", "work")
	if got := h.render(t, "{}", work); got != "ccm: work 5h 100% ⛔ · 7d 30%\n" {
		t.Fatalf("limited: %q", got)
	}
	if got := h.render(t, "{}", "/elsewhere/gone"); got != "ccm: gone (logged out)\n" {
		t.Fatalf("unknown basename found in cache: %q", got)
	}
}

func TestRenderLoggedOutAccount(t *testing.T) {
	c := freshCache()
	c.Accounts[0].State = "logged-out"
	c.Accounts[0].FiveHourUsed, c.Accounts[0].SevenDayUsed = nil, nil
	h := newHarness(t, c)
	if got := h.render(t, "{}", ""); got != "ccm: main (logged out)\n" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderAccountMissingFromCache(t *testing.T) {
	h := newHarness(t, freshCache())
	if got := h.render(t, "{}", "/x/unlisted"); got != "ccm: unlisted\n" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderMissingCacheSpawnsAndPrintsAccount(t *testing.T) {
	h := newHarness(t, nil)
	got := h.render(t, "{}", "")
	if got != "ccm: main\n" {
		t.Fatalf("got %q", got)
	}
	if h.spawns != 1 {
		t.Fatalf("spawns=%d want 1", h.spawns)
	}
}

func TestRenderStaleCacheSpawnsOnceAndUsesCache(t *testing.T) {
	c := freshCache()
	c.WrittenAt = now.Add(-5 * time.Minute)
	h := newHarness(t, c)

	got := h.render(t, "{}", "")

	if got != "ccm: main 5h 76% · 7d 12%\n" || h.spawns != 1 {
		t.Fatalf("got %q spawns=%d", got, h.spawns)
	}
}

func TestRenderSpawnErrorIsIgnored(t *testing.T) {
	h := newHarness(t, nil)
	h.deps.SpawnRefresh = func() error { return errors.New("boom") }
	if got := h.render(t, "{}", ""); got != "ccm: main\n" {
		t.Fatalf("got %q", got)
	}
}

func TestAccountResolution(t *testing.T) {
	h := newHarness(t, freshCache())
	work := filepath.Join(h.home, "accts", "work")
	cases := map[string]string{
		"":                           "main",
		h.deps.Paths.ClaudeDir:       "main",
		h.deps.Paths.ClaudeDir + "/": "main",
		work:                         "work",
		work + "/":                   "work",
		"~/accts/work":               "work",
		"/somewhere/else/other":      "other",
	}
	for env, want := range cases {
		if got := Account(h.deps.Paths, env); got != want {
			t.Errorf("env %q: got %q want %q", env, got, want)
		}
	}
}

func TestSpawnerLockGuard(t *testing.T) {
	p := paths.Paths{AccountsRoot: t.TempDir()}
	clock := now
	starts := 0
	spawn := NewSpawner(p, func() time.Time { return clock }, func() error { starts++; return nil })

	_ = spawn()
	_ = spawn()
	if starts != 1 {
		t.Fatalf("starts=%d want 1 while lock fresh", starts)
	}
	clock = clock.Add(2 * time.Minute)
	_ = spawn()
	if starts != 2 {
		t.Fatalf("starts=%d want 2 after lock went stale", starts)
	}
}

func TestShellRunnerTimesOutAndCaptures(t *testing.T) {
	out, err := RunShell("cat; echo hi", []byte("in\n"), time.Second)
	if err != nil || out != "in\nhi\n" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	start := time.Now()
	_, err = RunShell("sleep 5", nil, 100*time.Millisecond)
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("want fast timeout, err=%v after %v", err, time.Since(start))
	}
}

func TestRenderIsCacheOnlyAndFast(t *testing.T) {
	// Deps has no Keychain/HTTP collaborators at all; the render path is
	// cache-only. The time bound is deliberately generous to avoid flakiness.
	h := newHarness(t, freshCache())
	start := time.Now()
	h.render(t, "{}", "")
	if d := time.Since(start); d > time.Second {
		t.Fatalf("render took %v", d)
	}
	if h.spawns != 0 || len(h.wraps) != 0 {
		t.Fatalf("unexpected side effects: spawns=%d wraps=%v", h.spawns, h.wraps)
	}
}

func TestRenderWindowPastResetShowsQuestionMark(t *testing.T) {
	past, future := now.Add(-time.Minute), now.Add(time.Hour)
	c := freshCache()
	c.Accounts[1].FiveHourResetsAt, c.Accounts[1].SevenDayResetsAt = &past, &future
	h := newHarness(t, c)
	work := filepath.Join(h.home, "accts", "work")

	got := h.render(t, "{}", work)

	if got != "ccm: work 5h ? · 7d 30%\n" {
		t.Fatalf("got %q", got)
	}
}

func TestSpawnerLockReleaseAllowsImmediateRespawn(t *testing.T) {
	p := paths.Paths{AccountsRoot: t.TempDir()}
	starts := 0
	spawn := NewSpawner(p, func() time.Time { return now }, func() error { starts++; return nil })

	_ = spawn()
	ReleaseLock(p)
	_ = spawn()

	if starts != 2 {
		t.Fatalf("starts=%d want 2", starts)
	}
}

func TestRunShellTimeoutKillsWholeProcessGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")

	_, err := RunShell("sleep 30 & echo $! > "+pidFile+"; wait", nil, 300*time.Millisecond)

	if err == nil {
		t.Fatal("want timeout error")
	}
	b, rerr := os.ReadFile(pidFile)
	if rerr != nil {
		t.Fatal(rerr)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	deadline := time.Now().Add(2 * time.Second)
	for syscall.Kill(pid, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if syscall.Kill(pid, 0) == nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("grandchild %d survived the timeout", pid)
	}
}

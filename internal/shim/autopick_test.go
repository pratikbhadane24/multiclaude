package shim

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/config"
	"github.com/pratikbhadane24/multiclaude/internal/paths"
	"github.com/pratikbhadane24/multiclaude/internal/registry"
	"github.com/pratikbhadane24/multiclaude/internal/run"
	"github.com/pratikbhadane24/multiclaude/internal/status"
)

var pickNow = time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

// pickFixture builds a ccm root with main, inbox2 and inbox3 and turns
// auto-pick on unless off is true.
func pickFixture(t *testing.T, off bool) paths.Paths {
	t.Helper()
	home := t.TempDir()
	p := paths.Resolve(func(string) string { return "" }, home)
	accts := []registry.Account{
		{Name: "main"},
		{Name: "inbox2", Dir: filepath.Join(p.AccountsRoot, "inbox2"), Email: "i2@x"},
		{Name: "inbox3", Dir: filepath.Join(p.AccountsRoot, "inbox3"), Email: "i3@x"},
	}
	if err := registry.Save(p, accts); err != nil {
		t.Fatal(err)
	}
	if !off {
		if err := config.Save(p, config.Config{DefaultAccount: "best"}); err != nil {
			t.Fatal(err)
		}
	}
	stubPresent(t, func(registry.Account) bool { return true })
	return p
}

func stubPresent(t *testing.T, f func(registry.Account) bool) {
	t.Helper()
	old := presentFn
	presentFn = f
	t.Cleanup(func() { presentFn = old })
}

func writeCache(t *testing.T, p paths.Paths, used map[string]float64) {
	t.Helper()
	fetched := pickNow.Add(-time.Minute)
	reset := pickNow.Add(3 * time.Hour)
	var c status.Cache
	for _, name := range []string{"main", "inbox2", "inbox3"} {
		u, ok := used[name]
		e := status.CacheAccount{Name: name, State: "ok"}
		if ok {
			five, seven := u, 0.0
			e.FiveHourUsed, e.SevenDayUsed = &five, &seven
			e.FiveHourResetsAt, e.SevenDayResetsAt = &reset, &reset
			e.FetchedAt = &fetched
		}
		c.Accounts = append(c.Accounts, e)
	}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(status.CachePath(p)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(status.CachePath(p), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func envValue(env []string, key string) string { return lookupEnv(env, key) }

func TestAutoPickRespectsExplicitConfigDir(t *testing.T) {
	p := pickFixture(t, false)
	env := []string{"PATH=/bin", "CLAUDE_CONFIG_DIR=/x"}

	got := AutoPick(p, env, pickNow)

	if !slices.Equal(got, env) {
		t.Fatalf("env changed: %v", got)
	}
}

func TestAutoPickOffByDefault(t *testing.T) {
	p := pickFixture(t, true)
	writeCache(t, p, map[string]float64{"inbox2": 10, "inbox3": 90})
	env := []string{"PATH=/bin"}

	got := AutoPick(p, env, pickNow)

	if !slices.Equal(got, env) {
		t.Fatalf("env changed: %v", got)
	}
}

func TestAutoPickChoosesMostHeadroom(t *testing.T) {
	p := pickFixture(t, false)
	writeCache(t, p, map[string]float64{"main": 0, "inbox2": 90, "inbox3": 10})

	got := AutoPick(p, []string{"PATH=/bin"}, pickNow)

	if envValue(got, "CCM_ACCOUNT") != "inbox3" {
		t.Fatalf("CCM_ACCOUNT = %q in %v", envValue(got, "CCM_ACCOUNT"), got)
	}
	if envValue(got, "CLAUDE_CONFIG_DIR") != filepath.Join(p.AccountsRoot, "inbox3") {
		t.Fatalf("CLAUDE_CONFIG_DIR = %q", envValue(got, "CLAUDE_CONFIG_DIR"))
	}
}

func TestAutoPickNeverMain(t *testing.T) {
	p := pickFixture(t, false)
	stubPresent(t, func(a registry.Account) bool { return a.IsMain() })
	if err := os.Remove(filepath.Join(p.AccountsRoot, "accounts.json")); err != nil {
		t.Fatal(err)
	}
	env := []string{"PATH=/bin"}

	got := AutoPick(p, env, pickNow)

	if !slices.Equal(got, env) {
		t.Fatalf("env changed: %v", got)
	}
}

func TestAutoPickCorruptCacheFallsBackToFirstLoggedIn(t *testing.T) {
	p := pickFixture(t, false)
	if err := os.MkdirAll(filepath.Dir(status.CachePath(p)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(status.CachePath(p), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := AutoPick(p, []string{"PATH=/bin"}, pickNow)

	if envValue(got, "CCM_ACCOUNT") != "inbox2" {
		t.Fatalf("CCM_ACCOUNT = %q", envValue(got, "CCM_ACCOUNT"))
	}
}

func TestAutoPickDoesNotMutateInput(t *testing.T) {
	p := pickFixture(t, false)
	writeCache(t, p, map[string]float64{"inbox2": 90, "inbox3": 10})
	env := make([]string, 1, 8)
	env[0] = "PATH=/bin"
	before := slices.Clone(env)

	got := AutoPick(p, env, pickNow)

	if !slices.Equal(env, before) || !slices.Equal(env[:cap(env)][1:3], make([]string, 2)) {
		t.Fatalf("input mutated: %v", env[:cap(env)])
	}
	if len(got) != 3 {
		t.Fatalf("want 3 entries, got %v", got)
	}
}

func TestMainAppliesAutoPickBeforeLaunch(t *testing.T) {
	f := newFixture(t)
	accts := []registry.Account{{Name: "main"}, {Name: "inbox2", Dir: filepath.Join(f.p.AccountsRoot, "inbox2")}}
	if err := registry.Save(f.p, accts); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(f.p, config.Config{DefaultAccount: "best"}); err != nil {
		t.Fatal(err)
	}
	stubPresent(t, func(a registry.Account) bool { return !a.IsMain() })

	if err := Main(nil, []string{"PATH=/bin"}, f.deps()); err != nil {
		t.Fatal(err)
	}

	if len(f.calls) != 1 || envValue(f.calls[0].env, "CCM_ACCOUNT") != "inbox2" {
		t.Fatalf("exec env = %+v", f.calls)
	}
}

func TestAutoPickSkipsWhenAccountChosen(t *testing.T) {
	p := pickFixture(t, false)
	writeCache(t, p, map[string]float64{"inbox2": 90, "inbox3": 10})
	env := []string{"PATH=/bin", "CCM_ACCOUNT=main"}

	got := AutoPick(p, env, pickNow)

	if !slices.Equal(got, env) {
		t.Fatalf("env changed: %v", got)
	}
}

func TestAutoPickReplacesEmptyAccountVar(t *testing.T) {
	p := pickFixture(t, false)
	writeCache(t, p, map[string]float64{"inbox2": 90, "inbox3": 10})

	got := AutoPick(p, []string{"CCM_ACCOUNT=", "PATH=/bin"}, pickNow)

	n := 0
	for _, e := range got {
		if strings.HasPrefix(e, "CCM_ACCOUNT=") {
			n++
		}
	}
	if n != 1 || envValue(got, "CCM_ACCOUNT") != "inbox3" {
		t.Fatalf("env = %v", got)
	}
}

// blockingRunner never answers until its context ends.
type blockingRunner struct{}

func (blockingRunner) Run(ctx context.Context, _ string, _ []string, _ []string, _ bool) (run.Result, error) {
	<-ctx.Done()
	return run.Result{}, ctx.Err()
}

func TestPresentWithTimesOut(t *testing.T) {
	start := time.Now()

	ok := presentWith(blockingRunner{}, registry.Account{Name: "inbox2", Dir: "/nowhere"}, 50*time.Millisecond)

	if ok || time.Since(start) > time.Second {
		t.Fatalf("ok=%v took %v", ok, time.Since(start))
	}
}

func TestMainAutoPickAppliesDefaultArgs(t *testing.T) {
	f := newFixture(t)
	accts := []registry.Account{{Name: "main"}, {Name: "inbox2", Dir: filepath.Join(f.p.AccountsRoot, "inbox2")}}
	if err := registry.Save(f.p, accts); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{DefaultAccount: "best", DefaultArgs: []string{"--dangerously-skip-permissions"}}
	if err := config.Save(f.p, cfg); err != nil {
		t.Fatal(err)
	}
	stubPresent(t, func(a registry.Account) bool { return !a.IsMain() })

	if err := Main([]string{"-p", "hi"}, []string{"PATH=/bin"}, f.deps()); err != nil {
		t.Fatal(err)
	}

	want := []string{"claude", "--dangerously-skip-permissions", "-p", "hi"}
	if len(f.calls) != 1 || !slices.Equal(f.calls[0].argv, want) {
		t.Fatalf("argv = %+v", f.calls)
	}
}

func TestMainNoAutoPickKeepsArgs(t *testing.T) {
	f := newFixture(t)
	if err := config.Save(f.p, config.Config{DefaultArgs: []string{"--x"}}); err != nil {
		t.Fatal(err)
	}

	if err := Main([]string{"-p"}, []string{"PATH=/bin"}, f.deps()); err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(f.calls[0].argv, []string{"claude", "-p"}) {
		t.Fatalf("argv = %v", f.calls[0].argv)
	}
}

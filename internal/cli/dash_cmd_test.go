package cli

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"multiclaude/internal/config"
	"multiclaude/internal/doctor"
	"multiclaude/internal/run"
	"multiclaude/internal/status"
)

func envValue(env []string, key string) (string, bool) {
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, key+"="); ok {
			return v, true
		}
	}
	return "", false
}

func TestLaunchCmdSetsConfigDirOnlyForNonMain(t *testing.T) {
	e := newCLI(t, "")
	t.Setenv("CLAUDE_CONFIG_DIR", "/stale")

	work, err := e.app.LaunchCmd("work", nil)
	if err != nil {
		t.Fatal(err)
	}
	main, err := e.app.LaunchCmd("main", nil)
	if err != nil {
		t.Fatal(err)
	}

	if v, ok := envValue(work.Env, "CLAUDE_CONFIG_DIR"); !ok || v != e.workDir {
		t.Fatalf("work CLAUDE_CONFIG_DIR=%q ok=%v", v, ok)
	}
	if v, ok := envValue(main.Env, "CLAUDE_CONFIG_DIR"); ok {
		t.Fatalf("main has CLAUDE_CONFIG_DIR=%q", v)
	}
	if work.Path != e.p.RealClaude || !reflect.DeepEqual(work.Args, []string{e.p.RealClaude}) {
		t.Fatalf("path=%q args=%q", work.Path, work.Args)
	}
}

func TestLaunchCmdUsesConfigArgsThenExtra(t *testing.T) {
	e := newCLI(t, "")
	e.setConfig(t, config.Config{DefaultArgs: []string{"--model", "opus"},
		Accounts: map[string]config.AccountConfig{"work": {Args: []string{"--verbose"}}}})

	c, err := e.app.LaunchCmd("work", []string{"--resume", "s1"})

	want := []string{"--model", "opus", "--verbose", "--resume", "s1"}
	if err != nil || !reflect.DeepEqual(c.Args[1:], want) {
		t.Fatalf("args=%q err=%v", c.Args, err)
	}
}

func TestCcmNameAndDashboardEnterLaunchIdentically(t *testing.T) {
	e := newCLI(t, "")
	e.setConfig(t, config.Config{DefaultArgs: []string{"--dangerously-skip-permissions", "--x", "a b"},
		Accounts: map[string]config.AccountConfig{"work": {Args: []string{"--model", "it's"}}}})

	if code := e.main("work"); code != ExitOK {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
	c, err := e.app.LaunchCmd("work", nil)

	calls := e.callsTo(e.p.RealClaude)
	if err != nil || len(calls) != 1 {
		t.Fatalf("err=%v calls=%v", err, e.fake.Calls)
	}
	if c.Path != calls[0].Name || !reflect.DeepEqual(c.Args[1:], calls[0].Args) || !reflect.DeepEqual(c.Env, calls[0].Env) {
		t.Fatalf("dash=%q %q\nccm=%q %q", c.Path, c.Args, calls[0].Name, calls[0].Args)
	}
}

func TestDashArgsForUsesConfig(t *testing.T) {
	e := newCLI(t, "")
	e.setConfig(t, config.Config{DefaultArgs: []string{"--dangerously-skip-permissions"}})

	got := e.app.dashDeps().ArgsFor("work")

	if !reflect.DeepEqual(got, []string{"--dangerously-skip-permissions"}) {
		t.Fatalf("args=%q", got)
	}
}

func TestDashCollectWritesUsageCache(t *testing.T) {
	e := newCLI(t, "")
	e.loginWithUsage(e.workDir, usageBody("30", "10"))

	e.app.dashCollect(context.Background())

	c, err := status.ReadCache(e.p)
	if err != nil || len(c.Accounts) != 2 || c.Accounts[1].Name != "work" || !c.WrittenAt.Equal(testNow) {
		t.Fatalf("cache=%+v err=%v", c, err)
	}
}

func TestLaunchCmdUnknownAccount(t *testing.T) {
	e := newCLI(t, "")

	if _, err := e.app.LaunchCmd("nope", nil); err == nil || !strings.Contains(err.Error(), `unknown account "nope"`) {
		t.Fatalf("err=%v", err)
	}
}

func TestBareCcmWithoutTTYPrintsUsage(t *testing.T) {
	e := newCLI(t, "")

	if code := e.main(); code != ExitUsage || !strings.Contains(e.err.String(), "dash") {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
}

func TestBareCcmWithDevNullIsNotATerminal(t *testing.T) {
	e := newCLI(t, "")
	in, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	e.app.In, e.app.Out = in, out

	if code := e.main(); code != ExitUsage || !strings.Contains(e.err.String(), "Usage:") {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
}

func TestDashWithoutTTYFails(t *testing.T) {
	e := newCLI(t, "")

	if code := e.main("dash"); code != ExitError || !strings.Contains(e.err.String(), "terminal") {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
}

func TestDashDepsCollectReturnsRegistryAccounts(t *testing.T) {
	e := newCLI(t, "")

	snap := e.app.dashDeps().Collect(context.Background())

	if snap.Err != nil || len(snap.Rows) != 2 || len(snap.Accounts) != 2 || snap.Rows[1].Account != "work" {
		t.Fatalf("snap=%+v", snap)
	}
}

func TestDashCollectFeedsCombinedToday(t *testing.T) {
	e := newCLI(t, "")
	file := filepath.Join(e.p.ClaudeDir, "projects", "p", "s.jsonl")
	mustWrite(t, file, `{"type":"assistant","timestamp":"`+testNow.Format(time.RFC3339)+
		`","message":{"id":"m1","usage":{"input_tokens":7,"output_tokens":9}}}`+"\n")
	if err := os.Chtimes(file, testNow, testNow); err != nil {
		t.Fatal(err)
	}

	snap := e.app.dashCollect(context.Background())

	if snap.Today != snap.Combined.Today || snap.Today.Input != 7 || snap.Today.Output != 9 {
		t.Fatalf("today=%+v combined=%+v err=%q", snap.Today, snap.Combined.Today, snap.TodayErr)
	}
}

func TestDashDepsSyncCapturesOutput(t *testing.T) {
	e := newCLI(t, "")

	out, err := e.app.dashDeps().Sync(context.Background())

	if err != nil || !strings.Contains(out, "work:") || e.out.Len() != 0 {
		t.Fatalf("out=%q err=%v stdout=%q", out, err, e.out)
	}
}

func TestDashDepsT3SyncCapturesOutput(t *testing.T) {
	e := newCLI(t, "")

	out, err := e.app.dashDeps().T3Sync(context.Background())

	if err != nil || !strings.Contains(out, "Restart T3 Code") {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestDashDepsDoctorReturnsChecks(t *testing.T) {
	e := newCLI(t, "")

	checks := e.app.dashDeps().Doctor(context.Background())

	if !slices.ContainsFunc(checks, func(c doctor.Check) bool { return c.Account == "work" && c.Name == "keychain" }) {
		t.Fatalf("checks=%+v", checks)
	}
}

func TestDashDepsRunReportsNonZeroExit(t *testing.T) {
	e := newCLI(t, "")
	e.fake.Respond = func(run.Call) (run.Result, error) { return run.Result{Code: 1, Stderr: "denied"}, nil }

	err := e.app.dashDeps().Run(context.Background(), "/usr/bin/osascript", []string{"-e", "x"})

	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("err=%v", err)
	}
	if c := e.fake.Calls[0]; c.Name != "/usr/bin/osascript" || c.Inherit {
		t.Fatalf("call=%+v", c)
	}
}

func TestDashDepsSelfRunsSelfExe(t *testing.T) {
	e := newCLI(t, "")

	c := e.app.dashDeps().Self("login", "work")

	if !reflect.DeepEqual(c.Args, []string{"/bin/ccm", "login", "work"}) {
		t.Fatalf("args=%q", c.Args)
	}
}

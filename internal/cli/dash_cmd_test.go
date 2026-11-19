package cli

import (
	"context"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"multiclaude/internal/doctor"
	"multiclaude/internal/run"
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

	work, err := e.app.LaunchCmd("work")
	if err != nil {
		t.Fatal(err)
	}
	main, err := e.app.LaunchCmd("main")
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

func TestLaunchCmdAppendsArgsFor(t *testing.T) {
	e := newCLI(t, "")
	orig := dashArgsFor
	t.Cleanup(func() { dashArgsFor = orig })
	dashArgsFor = func(_ *App, name string) []string { return []string{"--model", name} }

	c, err := e.app.LaunchCmd("work")

	if err != nil || !reflect.DeepEqual(c.Args[1:], []string{"--model", "work"}) {
		t.Fatalf("args=%q err=%v", c.Args, err)
	}
}

func TestLaunchCmdUnknownAccount(t *testing.T) {
	e := newCLI(t, "")

	if _, err := e.app.LaunchCmd("nope"); err == nil || !strings.Contains(err.Error(), `unknown account "nope"`) {
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

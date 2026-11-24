package cli

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func envHas(env []string, kv string) bool { return slices.Contains(env, kv) }

func envHasKey(env []string, key string) bool {
	for _, e := range env {
		if strings.HasPrefix(e, key+"=") {
			return true
		}
	}
	return false
}

func TestRunPassesArgsVerbatimWithAccountEnv(t *testing.T) {
	e := newCLI(t, "")

	code := e.main("run", "work", "--", "x", "--model", "opus")

	calls := e.callsTo(e.p.RealClaude)
	if code != ExitOK || len(calls) != 1 {
		t.Fatalf("code=%d calls=%v err=%s", code, e.fake.Calls, e.err)
	}
	c := calls[0]
	if !reflect.DeepEqual(c.Args, []string{"--", "x", "--model", "opus"}) {
		t.Fatalf("args=%q", c.Args)
	}
	if !c.Inherit || !envHas(c.Env, "CLAUDE_CONFIG_DIR="+e.workDir) {
		t.Fatalf("inherit=%v env has dir=%v", c.Inherit, envHas(c.Env, "CLAUDE_CONFIG_DIR="+e.workDir))
	}
}

func TestRunPropagatesExitCode(t *testing.T) {
	e := newCLI(t, "")
	e.exitCode = 7
	if code := e.main("work", "-p", "hi"); code != 7 {
		t.Fatalf("code=%d", code)
	}
}

func TestRegisteredMainRunsWithoutConfigDir(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/stale/dir")
	e := newCLI(t, "")

	e.main("main", "--resume")

	calls := e.callsTo(e.p.RealClaude)
	if len(calls) != 1 || envHasKey(calls[0].Env, "CLAUDE_CONFIG_DIR") {
		t.Fatalf("calls=%v", calls)
	}
	if !reflect.DeepEqual(calls[0].Args, []string{"--resume"}) {
		t.Fatalf("args=%q", calls[0].Args)
	}
}

func TestRunUsesShimWhenInstalled(t *testing.T) {
	e := newCLI(t, "")
	mustWrite(t, e.p.ShimPath, "#!/bin/sh\n")
	e.main("run", "work")
	if len(e.callsTo(e.p.ShimPath)) != 1 {
		t.Fatalf("calls=%v", e.fake.Calls)
	}
}

func TestBestAllLimitedExits2WithNextReset(t *testing.T) {
	e := newCLI(t, "")
	e.loginWithUsage("", usageBody("100", "20"))
	e.loginWithUsage(e.workDir, usageBody("100", "50"))

	code := e.main("best")

	if code != exitAllLimited || !strings.Contains(e.err.String(), "all accounts limited; next reset in 1h12m") {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
	if len(e.callsTo(e.p.RealClaude)) != 0 {
		t.Fatal("claude must not run")
	}
}

func TestBestPicksMostHeadroomAndRunsIt(t *testing.T) {
	e := newCLI(t, "")
	e.exitCode = 3
	e.loginWithUsage("", usageBody("90", "20"))
	e.loginWithUsage(e.workDir, usageBody("30", "10"))

	code := e.main("best", "-p", "hello")

	if code != 3 {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
	if !strings.Contains(e.err.String(), "ccm: using work (5h 30% used, 7d 10% used; 90% weekly left, 70% 5h left)") {
		t.Fatalf("err=%s", e.err)
	}
	calls := e.callsTo(e.p.RealClaude)
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, []string{"-p", "hello"}) || !envHas(calls[0].Env, "CLAUDE_CONFIG_DIR="+e.workDir) {
		t.Fatalf("calls=%v", calls)
	}
}

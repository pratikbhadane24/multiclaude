package cli

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"multiclaude/internal/config"
	"multiclaude/internal/handoff"
)

const continueCwd = "/Users/x/Work/proj"

// continueEnv fakes the working directory and seeds a shared-history session.
func continueEnv(t *testing.T) *cliEnv {
	t.Helper()
	t.Setenv("CCM_ACCOUNT", "")
	prev := getwd
	getwd = func() (string, error) { return continueCwd, nil }
	t.Cleanup(func() { getwd = prev })
	e := newCLI(t, "")
	mustWrite(t, filepath.Join(e.p.ClaudeDir, "projects", handoff.Slug(continueCwd), "sess-1.jsonl"), "{}\n")
	return e
}

func TestContinueDefaultsPickMostLimitedFromAndBestTo(t *testing.T) {
	e := continueEnv(t)
	e.loginWithUsage("", usageBody("100", "20"))
	e.loginWithUsage(e.workDir, usageBody("30", "10"))
	e.setConfig(t, config.Config{DefaultArgs: []string{"--model", "opus"}})

	code := e.main("continue")

	if code != ExitOK {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
	if got := e.out.String(); got != "Continuing sess-1 on work (from main)\n" {
		t.Fatalf("out=%q", got)
	}
	calls := e.callsTo(e.p.RealClaude)
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, []string{"--model", "opus", "--resume", "sess-1"}) {
		t.Fatalf("calls=%v", calls)
	}
	if !envHas(calls[0].Env, "CLAUDE_CONFIG_DIR="+e.workDir) {
		t.Fatalf("env=%v", calls[0].Env)
	}
}

func TestContinueFromDefaultsToCCMAccount(t *testing.T) {
	e := continueEnv(t)
	t.Setenv("CCM_ACCOUNT", "work")
	e.loginWithUsage("", usageBody("10", "10"))
	e.loginWithUsage(e.workDir, usageBody("5", "5"))

	code := e.main("continue")

	if code != ExitOK || e.out.String() != "Continuing sess-1 on main (from work)\n" {
		t.Fatalf("code=%d out=%q err=%s", code, e.out, e.err)
	}
}

func TestContinueExplicitFlagsNeedNoUsage(t *testing.T) {
	e := continueEnv(t)

	code := e.main("continue", "--from", "work", "--to", "main", "--session", "abc-123")

	calls := e.callsTo(e.p.RealClaude)
	if code != ExitOK || len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, []string{"--resume", "abc-123"}) {
		t.Fatalf("code=%d calls=%v err=%s", code, calls, e.err)
	}
	if envHasKey(calls[0].Env, "CLAUDE_CONFIG_DIR") {
		t.Fatal("main must run without CLAUDE_CONFIG_DIR")
	}
	if e.out.String() != "Continuing abc-123 on main (from work)\n" {
		t.Fatalf("out=%q", e.out)
	}
}

func TestContinueRefusesSameAccount(t *testing.T) {
	e := continueEnv(t)
	code := e.main("continue", "--from", "work", "--to", "work")
	if code != ExitError || !strings.Contains(e.err.String(), "same account") || len(e.callsTo(e.p.RealClaude)) != 0 {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
}

func TestContinueNoFreeAccountExits2(t *testing.T) {
	e := continueEnv(t)
	e.loginWithUsage("", usageBody("100", "20"))
	e.loginWithUsage(e.workDir, usageBody("100", "50"))

	code := e.main("continue")

	if code != exitAllLimited || !strings.Contains(e.err.String(), "no free account to continue on") || len(e.callsTo(e.p.RealClaude)) != 0 {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
}

func TestContinueNoSessionExits1(t *testing.T) {
	e := continueEnv(t)
	getwd = func() (string, error) { return "/somewhere/else", nil }

	code := e.main("continue", "--from", "work", "--to", "main")

	if code != ExitError || !strings.Contains(e.err.String(), "no conversation found for this folder in work") || len(e.callsTo(e.p.RealClaude)) != 0 {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
}

func TestContinueUnknownAccountAndBadSession(t *testing.T) {
	e := continueEnv(t)
	if code := e.main("continue", "--from", "nope"); code != ExitError || !strings.Contains(e.err.String(), `unknown account "nope"`) {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
	if code := e.main("continue", "--from", "work", "--to", "main", "--session", "../x"); code != ExitUsage {
		t.Fatalf("code=%d", code)
	}
}

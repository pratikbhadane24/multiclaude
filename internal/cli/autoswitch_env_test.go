package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"multiclaude/internal/config"
	"multiclaude/internal/handoff"
	"multiclaude/internal/status"
)

var credentialKeys = []string{"CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"}

func TestAutoSwitchStripsCredentialEnvWithOneWarning(t *testing.T) {
	// Arrange
	e := newCLI(t, "")
	useCwd(t)
	for _, k := range credentialKeys {
		t.Setenv(k, "secret")
	}
	rec := enableAutoSwitch(t, e, config.Config{})

	// Act
	e.main("work")

	// Assert
	if len(rec.calls) != 1 {
		t.Fatalf("calls=%d err=%s", len(rec.calls), e.err)
	}
	for _, k := range credentialKeys {
		if envHasKey(rec.calls[0].env, k) {
			t.Fatalf("%s passed to claude", k)
		}
	}
	errOut := e.err.String()
	if strings.Count(errOut, "ccm: warning:") != 1 || !strings.Contains(errOut, "ANTHROPIC_API_KEY") {
		t.Fatalf("err=%q", errOut)
	}
}

func TestAutoSwitchNoCredentialEnvNoWarning(t *testing.T) {
	e := newCLI(t, "")
	useCwd(t)
	for _, k := range credentialKeys {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	enableAutoSwitch(t, e, config.Config{})

	e.main("work")

	if strings.Contains(e.err.String(), "warning") {
		t.Fatalf("err=%q", e.err.String())
	}
}

func TestAutoSwitchFollowsClearedSession(t *testing.T) {
	// Arrange: /clear inside claude starts a new transcript in the project
	// directory and claude records it in sessions/<pid>.json; the limit
	// lands there.
	e := newCLI(t, "")
	cwd := useCwd(t)
	rec := enableAutoSwitch(t, e, config.Config{})
	fetched, low := testNow.Add(-time.Minute), 10.0
	writeUsageCache(t, e,
		status.CacheAccount{Name: "main", FiveHourUsed: &low, FetchedAt: &fetched, State: "ok"},
		status.CacheAccount{Name: "work", FiveHourUsed: &low, FetchedAt: &fetched, State: "ok"},
	)
	const cleared = "99999999-2222-4333-8444-555555555555"
	limitLine := `{"type":"assistant","isApiErrorMessage":true,"error":"rate_limit","message":{"content":"You've hit your 5-hour limit"}}` + "\n"
	rec.onStart = func(i int, c procCall, p *cliProc) {
		if i > 0 {
			p.exit(0)
			return
		}
		dir := filepath.Join(e.workDir, "projects", handoff.Slug(cwd))
		mustWrite(t, filepath.Join(dir, flagValue(c.args, "--session-id")+".jsonl"), `{"type":"user"}`+"\n")
		mustWrite(t, filepath.Join(dir, cleared+".jsonl"), `{"type":"user"}`+"\n"+limitLine)
		pidFile := filepath.Join(e.workDir, "sessions", strconv.Itoa(fakePid(i))+".json")
		mustWrite(t, pidFile, `{"pid":`+strconv.Itoa(fakePid(i))+`,"sessionId":"`+cleared+`"}`)
	}

	// Act
	code := e.main("work")

	// Assert
	if code != ExitOK || len(rec.calls) != 2 || flagValue(rec.calls[1].args, "--resume") != cleared {
		t.Fatalf("code=%d calls=%+v err=%s", code, rec.calls, e.err)
	}
}

func TestAutoSwitchIgnoresAnotherClaudesLimitInSharedProjectDir(t *testing.T) {
	// Arrange: another terminal's claude, in the same folder, hits a limit
	// in its own conversation; ours keeps running until it exits.
	e := newCLI(t, "")
	cwd := useCwd(t)
	rec := enableAutoSwitch(t, e, config.Config{})
	const other = "99999999-2222-4333-8444-555555555555"
	limitLine := `{"type":"assistant","isApiErrorMessage":true,"error":"rate_limit","message":{"content":"You've hit your 5-hour limit"}}` + "\n"
	rec.onStart = func(i int, c procCall, p *cliProc) {
		dir := filepath.Join(e.workDir, "projects", handoff.Slug(cwd))
		mustWrite(t, filepath.Join(dir, flagValue(c.args, "--session-id")+".jsonl"), `{"type":"user"}`+"\n")
		mustWrite(t, filepath.Join(dir, other+".jsonl"), `{"type":"user"}`+"\n"+limitLine)
		go func() { time.Sleep(2 * time.Second); p.exit(0) }()
	}

	// Act
	code := e.main("work")

	// Assert: no switch, one launch.
	if code != ExitOK || len(rec.calls) != 1 {
		t.Fatalf("code=%d calls=%d err=%s", code, len(rec.calls), e.err)
	}
}

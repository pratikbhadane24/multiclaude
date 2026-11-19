package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"multiclaude/internal/handoff"
)

// dupContinueEnv has main and work signed in as the same login, with a
// shared-history session for the faked working directory.
func dupContinueEnv(t *testing.T) *cliEnv {
	t.Helper()
	t.Setenv("CCM_ACCOUNT", "")
	prev := getwd
	getwd = func() (string, error) { return continueCwd, nil }
	t.Cleanup(func() { getwd = prev })
	e := sameLoginEnv(t)
	mustWrite(t, filepath.Join(e.p.ClaudeDir, "projects", handoff.Slug(continueCwd), "sess-1.jsonl"), "{}\n")
	return e
}

func TestContinueAutoTargetSkipsDuplicateLoginOfFrom(t *testing.T) {
	e := dupContinueEnv(t)

	code := e.main("continue", "--from", "main")

	if code != exitAllLimited || len(e.resumeRuns()) != 0 {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
}

func TestContinueExplicitToRefusesDuplicateLoginOfFrom(t *testing.T) {
	e := dupContinueEnv(t)

	code := e.main("continue", "--to", "work")

	if code != ExitError || !strings.Contains(e.err.String(), "same login") || len(e.resumeRuns()) != 0 {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
}

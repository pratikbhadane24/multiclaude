package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"multiclaude/internal/handoff"
	"multiclaude/internal/run"
)

// markLive records sessionID as running in the test process (always alive).
func markLive(t *testing.T, dir, sessionID string) {
	t.Helper()
	pid := os.Getpid()
	body := fmt.Sprintf(`{"pid":%d,"sessionId":%q,"cwd":%q}`, pid, sessionID, continueCwd)
	mustWrite(t, filepath.Join(dir, "sessions", fmt.Sprintf("%d.json", pid)), body)
}

// resumeRuns are the real-claude calls that are not `auth status` lookups.
func (e *cliEnv) resumeRuns() []run.Call {
	var out []run.Call
	for _, c := range e.callsTo(e.p.RealClaude) {
		if len(c.Args) == 0 || c.Args[0] != "auth" {
			out = append(out, c)
		}
	}
	return out
}

func TestContinueRefusesWhenOnlySessionIsLive(t *testing.T) {
	e := continueEnv(t)
	markLive(t, e.workDir, "sess-1")

	code := e.main("continue", "--from", "main", "--to", "work")

	want := "the latest chat here is still open in another window (session sess-1); close it first or pass --session"
	if code != ExitError || !strings.Contains(e.err.String(), want) || len(e.resumeRuns()) != 0 {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
}

func TestContinuePicksNewestIdleSessionSkippingLiveOne(t *testing.T) {
	e := continueEnv(t)
	mustWrite(t, filepath.Join(e.p.ClaudeDir, "projects", handoff.Slug(continueCwd), "sess-live.jsonl"), "{}\n")
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(e.p.ClaudeDir, "projects", handoff.Slug(continueCwd), "sess-live.jsonl"), later, later); err != nil {
		t.Fatal(err)
	}
	markLive(t, e.p.ClaudeDir, "sess-live")

	code := e.main("continue", "--from", "main", "--to", "work")

	calls := e.resumeRuns()
	if code != ExitOK || len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, []string{"--resume", "sess-1"}) {
		t.Fatalf("code=%d err=%s calls=%v", code, e.err, calls)
	}
}

func TestContinueExplicitSessionIgnoresLiveCheck(t *testing.T) {
	e := continueEnv(t)
	markLive(t, e.workDir, "sess-1")

	code := e.main("continue", "--from", "main", "--to", "work", "--session", "sess-1")

	if code != ExitOK {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
}

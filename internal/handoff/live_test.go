package handoff

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/registry"
)

func fakeAlive(t *testing.T, alive ...int) {
	t.Helper()
	old := pidAlive
	pidAlive = func(pid int) bool {
		for _, p := range alive {
			if p == pid {
				return true
			}
		}
		return false
	}
	t.Cleanup(func() { pidAlive = old })
}

func TestLiveSessionsReadsEveryAccountAndIgnoresDeadAndKeyFiles(t *testing.T) {
	e := newEnv(t)
	fakeAlive(t, 100, 200)
	write(t, filepath.Join(e.p.ClaudeDir, "sessions", "100.json"), `{"pid":100,"sessionId":"live-main","cwd":"/x"}`, time.Time{})
	write(t, filepath.Join(e.to.Dir, "sessions", "200.json"), `{"pid":200,"sessionId":"live-work","cwd":"/x"}`, time.Time{})
	write(t, filepath.Join(e.to.Dir, "sessions", "300.json"), `{"pid":300,"sessionId":"dead","cwd":"/x"}`, time.Time{})
	write(t, filepath.Join(e.to.Dir, "sessions", "200.abc.key"), `secret`, time.Time{})
	write(t, filepath.Join(e.to.Dir, "sessions", "bad.json"), `not json`, time.Time{})

	got := LiveSessions(e.p, []registry.Account{e.from, e.to})

	if len(got) != 2 || !got["live-main"] || !got["live-work"] {
		t.Fatalf("got %v", got)
	}
}

func TestFindLatestExcludingSkipsLiveSession(t *testing.T) {
	e := newEnv(t)
	dir := filepath.Join(e.p.ClaudeDir, "projects", Slug(testCwd))
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	write(t, filepath.Join(dir, "idle.jsonl"), "{}\n", base)
	write(t, filepath.Join(dir, "busy.jsonl"), "{}\n", base.Add(time.Hour))

	s, err := FindLatestExcluding(e.p, e.from, testCwd, map[string]bool{"busy": true})

	if err != nil || s.ID != "idle" {
		t.Fatalf("s=%+v err=%v", s, err)
	}
}

func TestFindLatestExcludingOnlyLiveReturnsLiveError(t *testing.T) {
	e := newEnv(t)
	dir := filepath.Join(e.p.ClaudeDir, "projects", Slug(testCwd))
	write(t, filepath.Join(dir, "busy.jsonl"), "{}\n", time.Time{})

	_, err := FindLatestExcluding(e.p, e.from, testCwd, map[string]bool{"busy": true})

	var le *LiveError
	if !errors.As(err, &le) || le.ID != "busy" {
		t.Fatalf("err=%v", err)
	}
	want := "the latest chat here is still open in another window (session busy); close it first or pass --session"
	if err.Error() != want {
		t.Fatalf("got %q", err.Error())
	}
}

func TestFindLatestExcludingFallbackScanSkipsLive(t *testing.T) {
	e := newEnv(t)
	other := filepath.Join(e.p.ClaudeDir, "projects", "renamed-slug")
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	line := `{"cwd":"` + testCwd + `"}` + "\n"
	write(t, filepath.Join(other, "idle.jsonl"), line, base)
	write(t, filepath.Join(other, "busy.jsonl"), line, base.Add(time.Hour))

	s, err := FindLatestExcluding(e.p, e.from, testCwd, map[string]bool{"busy": true})

	if err != nil || s.ID != "idle" {
		t.Fatalf("s=%+v err=%v", s, err)
	}
}

func TestSlugReplacesEveryNonAlphanumeric(t *testing.T) {
	if got, want := Slug("/Users/x/My Proj_v2 (a)/é"), "-Users-x-My-Proj-v2--a---"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

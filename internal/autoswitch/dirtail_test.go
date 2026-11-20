package autoswitch

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

// idA and idB come from session_test.go.
const idC = "cccccccc-2222-4333-8444-555555555555"

// movableOwner is an Owner whose answer a test changes (claude ran /clear).
type movableOwner struct {
	mu sync.Mutex
	id string
}

func (o *movableOwner) set(id string) { o.mu.Lock(); o.id = id; o.mu.Unlock() }

func (o *movableOwner) get() (string, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.id, o.id != ""
}

// startDirWatch follows dir starting on id and returns a channel of limits
// and the owner the test moves.
func startDirWatch(t *testing.T, dir, id string) (<-chan Limit, *movableOwner) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan Limit, 8)
	owner := &movableOwner{}
	dt := NewDirTail(dir, id, time.Now(), owner.get)
	dt.scanEvery = 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		dt.Follow(ctx, testPoll, func(l Limit) { ch <- l })
	}()
	t.Cleanup(func() { cancel(); <-done })
	return ch, owner
}

func jsonlPath(dir, id string) string { return filepath.Join(dir, id+".jsonl") }

// stampedErr is a limit line written at time at.
func stampedErr(at time.Time) string {
	return `{"type":"assistant","isApiErrorMessage":true,"error":"rate_limit","timestamp":"` +
		at.UTC().Format(time.RFC3339Nano) + `","message":{"content":"You've hit your 5-hour limit"}}`
}

// ageFile sets path's mtime to d ago.
func ageFile(t *testing.T, path string, d time.Duration) {
	t.Helper()
	old := time.Now().Add(-d)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
}

func TestDirTailTagsLimitWithSession(t *testing.T) {
	dir := t.TempDir()
	ch, _ := startDirWatch(t, dir, idA)

	appendFile(t, jsonlPath(dir, idA), apiErr("rate_limit", "You've hit your 5-hour limit")+"\n")

	if l := waitLimit(t, ch); l.Session != idA {
		t.Fatalf("got %+v", l)
	}
}

func TestDirTailFollowsNewSessionFileFromItsStart(t *testing.T) {
	// Arrange: /clear started a new session file after launch.
	dir := t.TempDir()
	appendFile(t, jsonlPath(dir, idA), `{"type":"user"}`+"\n")
	ageFile(t, jsonlPath(dir, idA), time.Minute)
	ch, owner := startDirWatch(t, dir, idA)
	expectNone(t, ch)

	// Act
	appendFile(t, jsonlPath(dir, idB), `{"type":"user"}`+"\n"+apiErr("rate_limit", "You've hit your 5-hour limit")+"\n")
	owner.set(idB)

	// Assert
	if l := waitLimit(t, ch); l.Session != idB {
		t.Fatalf("got %+v", l)
	}
}

func TestDirTailFollowsResumedOlderSessionFromLaunchSize(t *testing.T) {
	dir := t.TempDir()
	appendFile(t, jsonlPath(dir, idC), apiErr("rate_limit", "old limit")+"\n")
	ageFile(t, jsonlPath(dir, idC), time.Hour)
	ch, owner := startDirWatch(t, dir, idA)
	appendFile(t, jsonlPath(dir, idA), `{"type":"user"}`+"\n")
	ageFile(t, jsonlPath(dir, idA), time.Minute)
	owner.set(idC)
	expectNone(t, ch)

	appendFile(t, jsonlPath(dir, idC), apiErr("rate_limit", "You've hit your weekly limit")+"\n")

	if l := waitLimit(t, ch); l.Session != idC || !l.Weekly {
		t.Fatalf("got %+v", l)
	}
	expectNone(t, ch)
}

func TestDirTailIgnoresSubagentsAndUntouchedFiles(t *testing.T) {
	dir := t.TempDir()
	appendFile(t, jsonlPath(dir, idC), apiErr("rate_limit", "old")+"\n")
	ageFile(t, jsonlPath(dir, idC), time.Hour)
	ch, _ := startDirWatch(t, dir, idA)
	sub := filepath.Join(dir, idA, "subagents")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}

	appendFile(t, filepath.Join(sub, "agent-1.jsonl"), apiErr("rate_limit", "You've hit your 5-hour limit")+"\n")

	expectNone(t, ch)
}

func TestDirTailDropsLimitsOlderThanLaunch(t *testing.T) {
	// A new file that copies history carries the earlier limit line.
	dir := t.TempDir()
	ch, owner := startDirWatch(t, dir, idA)
	owner.set(idB)

	appendFile(t, jsonlPath(dir, idB), stampedErr(time.Now().Add(-time.Minute))+"\n")
	expectNone(t, ch)
	appendFile(t, jsonlPath(dir, idB), stampedErr(time.Now())+"\n")

	if l := waitLimit(t, ch); l.Session != idB {
		t.Fatalf("got %+v", l)
	}
}

func TestDetectLimitReadsTimestamp(t *testing.T) {
	at := time.Date(2026, 11, 20, 9, 0, 0, 123e6, time.UTC)
	l, ok := DetectLimit([]byte(stampedErr(at)))
	if !ok || !l.At.Equal(at) {
		t.Fatalf("got %+v ok=%v", l, ok)
	}
}

func TestDirTailIgnoresOtherClaudesConversationInSharedDir(t *testing.T) {
	// Arrange: another terminal's claude works in the same (shared) project
	// directory and its newer conversation hits a limit.
	dir := t.TempDir()
	ch, _ := startDirWatch(t, dir, idA)
	appendFile(t, jsonlPath(dir, idA), `{"type":"user"}`+"\n")

	// Act
	appendFile(t, jsonlPath(dir, idB), `{"type":"user"}`+"\n"+apiErr("rate_limit", "You've hit your 5-hour limit")+"\n")

	// Assert: not ours, so no switch.
	expectNone(t, ch)
}

func TestSessionOfPidReadsClaudeSessionFile(t *testing.T) {
	cfg := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cfg, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(pid int, body string) {
		path := filepath.Join(cfg, "sessions", strconv.Itoa(pid)+".json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(42, `{"pid":42,"sessionId":"`+idB+`","kind":"interactive"}`)
	write(43, `{"pid":99,"sessionId":"`+idB+`"}`)
	write(44, `{"pid":44,"sessionId":"../../etc"}`)

	if id, ok := SessionOfPid(cfg, 42); !ok || id != idB {
		t.Fatalf("pid 42: got %q %v", id, ok)
	}
	for _, pid := range []int{43, 44, 45} {
		if id, ok := SessionOfPid(cfg, pid); ok {
			t.Fatalf("pid %d: got %q, want unknown", pid, id)
		}
	}
}

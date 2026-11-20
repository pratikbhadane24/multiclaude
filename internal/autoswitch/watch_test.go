package autoswitch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testPoll = 5 * time.Millisecond

func appendFile(t *testing.T, path, data string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(data); err != nil {
		t.Fatal(err)
	}
}

// startWatch runs Watch in the background and returns a channel of limits.
func startWatch(t *testing.T, path string) (<-chan Limit, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan Limit, 8)
	tail := NewTail(path)
	done := make(chan struct{})
	go func() {
		defer close(done)
		tail.Follow(ctx, testPoll, func(l Limit) { ch <- l })
	}()
	t.Cleanup(func() { cancel(); <-done })
	return ch, cancel
}

func waitLimit(t *testing.T, ch <-chan Limit) Limit {
	t.Helper()
	select {
	case l := <-ch:
		return l
	case <-time.After(2 * time.Second):
		t.Fatal("no limit detected")
	}
	return Limit{}
}

func expectNone(t *testing.T, ch <-chan Limit) {
	t.Helper()
	select {
	case l := <-ch:
		t.Fatalf("unexpected limit %+v", l)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestWatchIgnoresExistingLinesAndSeesNewOnes(t *testing.T) {
	// Arrange
	path := filepath.Join(t.TempDir(), "s.jsonl")
	appendFile(t, path, apiErr("rate_limit", "You've hit your 5-hour limit · resets 3pm")+"\n")
	ch, _ := startWatch(t, path)
	expectNone(t, ch)

	// Act
	appendFile(t, path, `{"type":"user"}`+"\n"+apiErr("rate_limit", "You've hit your weekly limit")+"\n")

	// Assert
	if l := waitLimit(t, ch); !l.Weekly {
		t.Fatalf("got %+v", l)
	}
}

func TestWatchWaitsForFileToAppear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "later.jsonl")
	ch, _ := startWatch(t, path)
	expectNone(t, ch)

	appendFile(t, path, apiErr("rate_limit", "You've hit your 5-hour limit")+"\n")

	waitLimit(t, ch)
}

func TestWatchJoinsPartialLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.jsonl")
	appendFile(t, path, "")
	ch, _ := startWatch(t, path)
	line := apiErr("rate_limit", "You've hit your 5-hour limit")

	appendFile(t, path, line[:20])
	expectNone(t, ch)
	appendFile(t, path, line[20:]+"\n")

	waitLimit(t, ch)
}

func TestWatchRereadsTruncatedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.jsonl")
	appendFile(t, path, strings.Repeat("x", 4096)+"\n")
	ch, _ := startWatch(t, path)

	if err := os.WriteFile(path, []byte(apiErr("rate_limit", "limit")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	waitLimit(t, ch)
}

func TestWatchStopsOnCancel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.jsonl")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { Watch(ctx, path, testPoll, func(Limit) {}); close(done) }()

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watch did not stop")
	}
}

func TestTailSkipsOverlongLine(t *testing.T) {
	tail := &Tail{}
	lines := tail.split([]byte("a\nb"))
	tail.keepPartial(make([]byte, maxPartialLine+1))
	lines = append(lines, tail.split([]byte("rest\nc\n"))...)
	if len(lines) != 2 || string(lines[0]) != "a" || string(lines[1]) != "c" {
		t.Fatalf("lines=%q", lines)
	}
}

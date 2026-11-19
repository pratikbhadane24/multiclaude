package tokens

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, 11, 19, 15, 0, 0, 0, time.UTC)

const todayTS = "2026-11-19T10:00:00.000Z"

func line(ts, model, msgID, reqID string, in, out, cr, cw int) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"requestId":%q,"message":{"id":%q,"model":%q,"usage":{"input_tokens":%d,"output_tokens":%d,"cache_read_input_tokens":%d,"cache_creation_input_tokens":%d}}}`,
		ts, reqID, msgID, model, in, out, cr, cw)
}

func writeTranscript(t *testing.T, dir, name string, mtime time.Time, lines ...string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func TestTodayDedupeKeepsMaxPerChannel(t *testing.T) {
	dir := t.TempDir()
	writeTranscript(t, dir, "p/a.jsonl", testNow,
		line(todayTS, "claude-opus-5-5", "msg_1", "req_1", 10, 10, 100, 5),
		line(todayTS, "claude-opus-5-5", "msg_1", "req_1", 10, 25, 100, 5))
	got, err := Today(dir, testNow)
	if err != nil {
		t.Fatal(err)
	}
	want := Tokens{Input: 10, Output: 25, CacheRead: 100, CacheWrite: 5}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestTodayIgnoresSyntheticYesterdayGarbage(t *testing.T) {
	dir := t.TempDir()
	writeTranscript(t, dir, "p/a.jsonl", testNow,
		line(todayTS, "<synthetic>", "m1", "r1", 1, 1, 1, 1),
		line("2026-11-18T10:00:00.000Z", "claude-opus-5-5", "m2", "r2", 2, 2, 2, 2),
		"not json {{{",
		line(todayTS, "claude-opus-5-5", "m3", "r3", 3, 4, 5, 6))
	got, err := Today(dir, testNow)
	if err != nil {
		t.Fatal(err)
	}
	want := Tokens{Input: 3, Output: 4, CacheRead: 5, CacheWrite: 6}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestTodaySumsDistinctKeysAndCountsMissingIDs(t *testing.T) {
	dir := t.TempDir()
	noID := `{"type":"assistant","timestamp":"` + todayTS + `","message":{"model":"x","usage":{"input_tokens":1,"output_tokens":1}}}`
	writeTranscript(t, dir, "p/a.jsonl", testNow,
		line(todayTS, "m", "a", "r", 1, 2, 3, 4),
		line(todayTS, "m", "b", "r", 10, 20, 30, 40),
		noID, noID)
	got, err := Today(dir, testNow)
	if err != nil {
		t.Fatal(err)
	}
	want := Tokens{Input: 13, Output: 24, CacheRead: 33, CacheWrite: 44}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestTodaySkipsOldFiles(t *testing.T) {
	dir := t.TempDir()
	writeTranscript(t, dir, "old.jsonl", testNow.Add(-48*time.Hour),
		line(todayTS, "m", "a", "r", 9, 9, 9, 9))
	got, err := Today(dir, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if got != (Tokens{}) {
		t.Fatalf("got %+v want zero", got)
	}
}

func TestTodayMissingDir(t *testing.T) {
	got, err := Today(filepath.Join(t.TempDir(), "nope"), testNow)
	if err != nil || got != (Tokens{}) {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestTodaySkipsOversizedLineAndKeepsLaterLines(t *testing.T) {
	dir := t.TempDir()
	huge := `{"type":"user","x":"` + strings.Repeat("a", maxLineBytes+10) + `"}`
	writeTranscript(t, dir, "p/a.jsonl", testNow,
		line(todayTS, "m", "a", "r", 1, 2, 3, 4),
		huge,
		line(todayTS, "m", "b", "r", 10, 20, 30, 40))
	got, err := Today(dir, testNow)
	if err != nil {
		t.Fatal(err)
	}
	want := Tokens{Input: 11, Output: 22, CacheRead: 33, CacheWrite: 44}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestTodayUsesNowLocationForCalendarDay(t *testing.T) {
	dir := t.TempDir()
	loc := time.FixedZone("UTC+10", 10*60*60)
	now := time.Date(2026, 11, 19, 8, 0, 0, 0, loc)
	writeTranscript(t, dir, "p/a.jsonl", now,
		line("2026-11-18T20:00:00.000Z", "m", "a", "r", 1, 1, 1, 1),
		line("2026-11-18T13:00:00.000Z", "m", "b", "r", 5, 5, 5, 5))
	got, err := Today(dir, now)
	if err != nil {
		t.Fatal(err)
	}
	want := Tokens{Input: 1, Output: 1, CacheRead: 1, CacheWrite: 1}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

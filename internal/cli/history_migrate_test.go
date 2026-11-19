package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncMergesAccountHistoryIntoMainAndLinks(t *testing.T) {
	e := newCLI(t, "")
	mainProj := filepath.Join(e.p.ClaudeDir, "projects")
	mustWrite(t, filepath.Join(mainProj, "-p", "m.jsonl"), "m\n")
	mustWrite(t, filepath.Join(mainProj, "-p", "x.jsonl"), "main\n")
	mustWrite(t, filepath.Join(e.workDir, "projects", "-q", "a.jsonl"), "a\n")
	mustWrite(t, filepath.Join(e.workDir, "projects", "-p", "x.jsonl"), "work\n")

	code := e.main("sync")

	if code != ExitOK {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
	if !strings.Contains(e.out.String(), "work: merged 1 chats into shared history (1 conflicts kept as *.from-work-*)") {
		t.Fatalf("out=%s", e.out)
	}
	if got, err := os.Readlink(filepath.Join(e.workDir, "projects")); err != nil || got != mainProj {
		t.Fatalf("readlink=%q err=%v", got, err)
	}
	b, err := os.ReadFile(filepath.Join(mainProj, "-q", "a.jsonl"))
	if err != nil || string(b) != "a\n" {
		t.Fatalf("data lost: %v", err)
	}
}

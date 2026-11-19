package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"multiclaude/internal/registry"
)

func TestAddMigratesExistingHistoryIntoMain(t *testing.T) {
	e := newCLI(t, "")
	mainProj := filepath.Join(e.p.ClaudeDir, "projects")
	mustWrite(t, filepath.Join(mainProj, "-p", "m.jsonl"), "m\n")
	dir := filepath.Join(e.p.AccountsRoot, "inbox1")
	mustWrite(t, filepath.Join(dir, "projects", "-q", "chat.jsonl"), "chat\n")
	accts, err := registry.Load(e.p)
	if err != nil {
		t.Fatal(err)
	}

	code := e.app.setupAccount(registry.Account{Name: "inbox1", Dir: dir}, accts)

	if code != ExitOK {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
	if !strings.Contains(e.out.String(), "inbox1: merged 1 chats into shared history (0 conflicts kept as *.from-inbox1-*)") {
		t.Fatalf("out=%s", e.out)
	}
	if got, err := os.Readlink(filepath.Join(dir, "projects")); err != nil || got != mainProj {
		t.Fatalf("readlink=%q err=%v", got, err)
	}
	if b, err := os.ReadFile(filepath.Join(mainProj, "-q", "chat.jsonl")); err != nil || string(b) != "chat\n" {
		t.Fatalf("chat lost: %v", err)
	}
}

func TestSyncSkipsAccountWithRunningSession(t *testing.T) {
	e := newCLI(t, "")
	mustWrite(t, filepath.Join(e.p.ClaudeDir, "projects", "-p", "m.jsonl"), "m\n")
	mustWrite(t, filepath.Join(e.workDir, "projects", "-q", "a.jsonl"), "a\n")
	pid := strconv.Itoa(os.Getpid())
	mustWrite(t, filepath.Join(e.workDir, "sessions", pid+".json"), "{}")

	code := e.main("sync")

	if code != ExitOK {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
	if !strings.Contains(e.err.String(), "ccm: warning: work has a running Claude session (pid "+pid+" (sessions/"+pid+".json)); close it and run ccm sync to share its history") {
		t.Fatalf("err=%s", e.err)
	}
	if fi, err := os.Lstat(filepath.Join(e.workDir, "projects")); err != nil || !fi.IsDir() {
		t.Fatalf("projects must stay a real dir: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(e.workDir, "projects", "-q", "a.jsonl")); err != nil || string(b) != "a\n" {
		t.Fatal("account data touched")
	}
}

func TestSyncRefusesAliasedAccountDir(t *testing.T) {
	e := newCLI(t, "")
	mustWrite(t, filepath.Join(e.p.ClaudeDir, "projects", "-p", "m.jsonl"), "m\n")
	alias := filepath.Join(e.p.AccountsRoot, "alias")
	if err := os.Symlink(e.p.ClaudeDir, alias); err != nil {
		t.Fatal(err)
	}
	accts, _ := registry.Load(e.p)
	if err := registry.Save(e.p, registry.With(accts, registry.Account{Name: "alias", Dir: alias})); err != nil {
		t.Fatal(err)
	}

	code := e.main("sync")

	if code != ExitError {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
	if b, err := os.ReadFile(filepath.Join(e.p.ClaudeDir, "projects", "-p", "m.jsonl")); err != nil || string(b) != "m\n" {
		t.Fatalf("main history damaged: %v", err)
	}
}

package history

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"multiclaude/internal/paths"
)

var now = time.Unix(1700000000, 0)

type env struct {
	p   paths.Paths
	acc string // account dir, named "inbox1"
}

func newEnv(t *testing.T) env {
	t.Helper()
	root := t.TempDir()
	p := paths.Paths{Home: root, ClaudeDir: filepath.Join(root, ".claude"), AccountsRoot: filepath.Join(root, "accts")}
	acc := filepath.Join(p.AccountsRoot, "inbox1")
	for _, d := range []string{p.ClaudeDir, acc} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return env{p: p, acc: acc}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func (e env) main(parts ...string) string {
	return filepath.Join(append([]string{e.p.ClaudeDir, "projects"}, parts...)...)
}

func (e env) account(parts ...string) string {
	return filepath.Join(append([]string{e.acc, "projects"}, parts...)...)
}

func (e env) merge(t *testing.T) MergeResult {
	t.Helper()
	res, err := MergeIntoMain(e.p, e.acc, "projects", now)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func assertLinked(t *testing.T, e env) {
	t.Helper()
	got, err := os.Readlink(e.account())
	if err != nil || got != e.main() {
		t.Fatalf("readlink=%q err=%v want %q", got, err, e.main())
	}
}

func TestMergeEmptyAccountDirJustLinks(t *testing.T) {
	e := newEnv(t)
	write(t, e.main("keep", "a.jsonl"), "a\n")
	if err := os.MkdirAll(e.account(), 0o700); err != nil {
		t.Fatal(err)
	}

	res := e.merge(t)

	if res.Moved != 0 || res.Conflicts != 0 || !res.Linked {
		t.Fatalf("res=%+v", res)
	}
	assertLinked(t, e)
}

func TestMergeDisjointMovesEverything(t *testing.T) {
	e := newEnv(t)
	write(t, e.main("-main-proj", "m.jsonl"), "m\n")
	write(t, e.account("-acct-proj", "c1.jsonl"), "c1\n")
	write(t, e.account("-acct-proj", "c1", "subagents", "s.jsonl"), "s\n")
	write(t, e.account("-acct-proj2", "c2.jsonl"), "c2\n")

	res := e.merge(t)

	if res.Moved != 2 || res.Conflicts != 0 || !res.Linked {
		t.Fatalf("res=%+v", res)
	}
	assertLinked(t, e)
	if read(t, e.main("-acct-proj", "c1.jsonl")) != "c1\n" || read(t, e.main("-acct-proj", "c1", "subagents", "s.jsonl")) != "s\n" || read(t, e.main("-main-proj", "m.jsonl")) != "m\n" {
		t.Fatal("data missing in main")
	}
}

func TestMergeIdenticalFileIsDropped(t *testing.T) {
	e := newEnv(t)
	write(t, e.main("p", "x.jsonl"), "same\n")
	write(t, e.account("p", "x.jsonl"), "same\n")

	res := e.merge(t)

	if res.Moved != 0 || res.Conflicts != 0 || !res.Linked {
		t.Fatalf("res=%+v", res)
	}
	entries, _ := os.ReadDir(e.main("p"))
	if len(entries) != 1 {
		t.Fatalf("entries=%v", entries)
	}
}

func TestMergeConflictingFileKeptWithSuffix(t *testing.T) {
	e := newEnv(t)
	write(t, e.main("p", "x.jsonl"), "main\n")
	write(t, e.account("p", "x.jsonl"), "account\n")

	res := e.merge(t)

	if res.Conflicts != 1 || !res.Linked {
		t.Fatalf("res=%+v", res)
	}
	if read(t, e.main("p", "x.jsonl")) != "main\n" {
		t.Fatal("main file overwritten")
	}
	kept := e.main("p", "x.jsonl.from-inbox1-1700000000")
	if read(t, kept) != "account\n" {
		t.Fatalf("conflict copy missing at %s", kept)
	}
}

func TestMergeNestedDirsMergeChildren(t *testing.T) {
	e := newEnv(t)
	write(t, e.main("p", "a", "old.jsonl"), "old\n")
	write(t, e.account("p", "a", "new.jsonl"), "new\n")
	write(t, e.account("p", "a", "old.jsonl"), "old\n")
	write(t, e.account("p", "a", "deep", "d.txt"), "d\n")

	res := e.merge(t)

	if res.Conflicts != 0 || res.Moved != 1 || !res.Linked {
		t.Fatalf("res=%+v", res)
	}
	if read(t, e.main("p", "a", "new.jsonl")) != "new\n" || read(t, e.main("p", "a", "deep", "d.txt")) != "d\n" {
		t.Fatal("merged data missing")
	}
}

func TestMergeUniqueSuffixNeverOverwritesPriorConflictCopy(t *testing.T) {
	e := newEnv(t)
	write(t, e.main("x"), "main\n")
	write(t, e.main("x.from-inbox1-1700000000"), "earlier\n")
	write(t, e.account("x"), "account\n")

	e.merge(t)

	if read(t, e.main("x.from-inbox1-1700000000")) != "earlier\n" {
		t.Fatal("earlier conflict copy overwritten")
	}
	if read(t, e.main("x.from-inbox1-1700000000-1")) != "account\n" {
		t.Fatal("account data lost")
	}
}

func TestMergeIsIdempotent(t *testing.T) {
	e := newEnv(t)
	write(t, e.main("other", "o.jsonl"), "o\n")
	write(t, e.account("p", "c.jsonl"), "c\n")
	first := e.merge(t)

	second := e.merge(t)

	if first.Moved != 1 || second.Moved != 0 || second.Linked {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	assertLinked(t, e)
	if read(t, e.main("p", "c.jsonl")) != "c\n" {
		t.Fatal("data lost")
	}
}

func TestMergeSkipsWhenMainSourceMissing(t *testing.T) {
	e := newEnv(t)
	write(t, e.account("p", "c.jsonl"), "c\n")
	// main has no projects dir: nothing may be moved or created in main.
	res, err := MergeIntoMain(e.p, e.acc, "projects", now)
	if err != nil || res.Linked || res.Moved != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if read(t, e.account("p", "c.jsonl")) != "c\n" {
		t.Fatal("account data touched")
	}
}

func TestMergeRejectsNonHistoryItem(t *testing.T) {
	e := newEnv(t)
	if _, err := MergeIntoMain(e.p, e.acc, "rules", now); err == nil || !strings.Contains(err.Error(), "history item") {
		t.Fatalf("err=%v", err)
	}
}

func TestMergeLeavesExistingSymlinkAlone(t *testing.T) {
	e := newEnv(t)
	write(t, e.main("p", "m.jsonl"), "m\n")
	if err := os.Symlink(e.main(), e.account()); err != nil {
		t.Fatal(err)
	}

	res := e.merge(t)

	if res.Moved != 0 || res.Linked {
		t.Fatalf("res=%+v", res)
	}
}

package history

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMergeRejectsAccountDirSymlinkedToMain(t *testing.T) {
	e := newEnv(t)
	write(t, e.main("p", "x.jsonl"), "main\n")
	alias := filepath.Join(e.p.AccountsRoot, "alias")
	if err := os.Symlink(e.p.ClaudeDir, alias); err != nil {
		t.Fatal(err)
	}

	res, err := MergeIntoMain(e.p, alias, "projects", now)

	if err == nil || res.Linked {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if read(t, e.main("p", "x.jsonl")) != "main\n" {
		t.Fatal("main data lost")
	}
	if fi, lerr := os.Lstat(e.main()); lerr != nil || !fi.IsDir() {
		t.Fatalf("main projects no longer a real dir: %v", lerr)
	}
}

func TestMergeRejectsAccountDirEqualToMain(t *testing.T) {
	e := newEnv(t)
	write(t, e.main("p", "x.jsonl"), "main\n")

	res, err := MergeIntoMain(e.p, e.p.ClaudeDir, "projects", now)

	if err == nil || res.Linked || read(t, e.main("p", "x.jsonl")) != "main\n" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestMergeRejectsAccountDirNestedInMain(t *testing.T) {
	e := newEnv(t)
	write(t, e.main("p", "x.jsonl"), "main\n")
	nested := filepath.Join(e.main(), "p", "acct")
	write(t, filepath.Join(nested, "projects", "y.jsonl"), "y\n")

	if _, err := MergeIntoMain(e.p, nested, "projects", now); err == nil {
		t.Fatal("expected overlap error")
	}
	if read(t, filepath.Join(nested, "projects", "y.jsonl")) != "y\n" {
		t.Fatal("data touched")
	}
}

func TestMergeRejectsRelativeAccountDir(t *testing.T) {
	e := newEnv(t)
	write(t, e.main("p", "x.jsonl"), "main\n")
	if _, err := MergeIntoMain(e.p, "relative/acct", "projects", now); err == nil {
		t.Fatal("expected error")
	}
}

func TestMergeErrorsWhenMainItemIsNotADirectory(t *testing.T) {
	e := newEnv(t)
	elsewhere := filepath.Join(e.p.Home, "elsewhere")
	write(t, filepath.Join(elsewhere, "x"), "x\n")
	if err := os.Symlink(elsewhere, e.main()); err != nil {
		t.Fatal(err)
	}
	write(t, e.account("p", "c.jsonl"), "c\n")

	_, err := MergeIntoMain(e.p, e.acc, "projects", now)

	if err == nil || !strings.Contains(err.Error(), "main projects is not a directory") {
		t.Fatalf("err=%v", err)
	}
	if read(t, e.account("p", "c.jsonl")) != "c\n" {
		t.Fatal("account data touched")
	}
}

func TestMergeKeepsSymlinkChildAsConflictCopy(t *testing.T) {
	e := newEnv(t)
	write(t, e.main("p", "m.jsonl"), "m\n")
	if err := os.Symlink(e.main("p"), e.account("link")); err != nil {
		if mkErr := os.MkdirAll(e.account(), 0o700); mkErr != nil {
			t.Fatal(mkErr)
		}
		if err := os.Symlink(e.main("p"), e.account("link")); err != nil {
			t.Fatal(err)
		}
	}

	res := e.merge(t)

	if res.Conflicts != 1 || !res.Linked {
		t.Fatalf("res=%+v", res)
	}
	kept := e.main("link.from-inbox1-1700000000")
	if fi, err := os.Lstat(kept); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink not kept at %s: %v", kept, err)
	}
	if _, err := os.Lstat(e.main("link")); err == nil {
		t.Fatal("symlink must not be moved under its original name")
	}
}

func TestMergeCountsOnlyTopLevelTranscripts(t *testing.T) {
	e := newEnv(t)
	write(t, e.main("other", "o.jsonl"), "o\n")
	write(t, e.account("p", "a.jsonl"), "a\n")
	write(t, e.account("p", "a", "subagents", "s.jsonl"), "s\n")
	write(t, e.account("p", "a", "tool-results", "t.jsonl"), "t\n")

	if res := e.merge(t); res.Moved != 1 {
		t.Fatalf("res=%+v", res)
	}
}

func TestNeedsMerge(t *testing.T) {
	e := newEnv(t)
	if NeedsMerge(e.acc) {
		t.Fatal("empty account needs no merge")
	}
	write(t, e.account("p", "c.jsonl"), "c\n")
	if !NeedsMerge(e.acc) {
		t.Fatal("real projects dir needs merge")
	}
}

func fakeProbe(t *testing.T, alive bool) {
	t.Helper()
	prev := pidAlive
	pidAlive = func(int) bool { return alive }
	t.Cleanup(func() { pidAlive = prev })
}

func writeSession(t *testing.T, e env, name string, age time.Duration) {
	t.Helper()
	path := filepath.Join(e.acc, "sessions", name)
	write(t, path, "{}")
	mt := now.Add(-age)
	if err := os.Chtimes(path, mt, mt); err != nil {
		t.Fatal(err)
	}
}

func TestLiveSessionDetection(t *testing.T) {
	cases := []struct {
		name  string
		file  string
		age   time.Duration
		alive bool
		want  bool
	}{
		{"live pid", "123.json", time.Hour, true, true},
		{"dead pid, recent file", "123.json", time.Minute, false, false},
		{"unparsable, recent", "abc.json", time.Minute, false, true},
		{"unparsable, old", "abc.json", time.Hour, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			fakeProbe(t, tc.alive)
			writeSession(t, e, tc.file, tc.age)
			if got := HasLiveSession(e.acc, now); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestLiveSessionPidFromJSONBody(t *testing.T) {
	e := newEnv(t)
	fakeProbe(t, true)
	write(t, filepath.Join(e.acc, "sessions", "x.json"), `{"pid":`+strconv.Itoa(os.Getpid())+`}`)
	if !HasLiveSession(e.acc, now) {
		t.Fatal("pid in body should count")
	}
}

func TestNoSessionsDirIsNotLive(t *testing.T) {
	e := newEnv(t)
	if HasLiveSession(e.acc, now) {
		t.Fatal("no sessions dir")
	}
}

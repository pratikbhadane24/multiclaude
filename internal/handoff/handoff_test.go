package handoff

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"multiclaude/internal/paths"
	"multiclaude/internal/registry"
)

const testCwd = "/Users/x/Work/multi.claude"

type env struct {
	p        paths.Paths
	from, to registry.Account
}

func newEnv(t *testing.T) env {
	t.Helper()
	root := t.TempDir()
	p := paths.Paths{Home: root, ClaudeDir: filepath.Join(root, ".claude"), AccountsRoot: filepath.Join(root, "accts")}
	return env{
		p:    p,
		from: registry.Account{Name: "main"},
		to:   registry.Account{Name: "work", Dir: filepath.Join(p.AccountsRoot, "work")},
	}
}

func write(t *testing.T, path, content string, mod time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if !mod.IsZero() {
		if err := os.Chtimes(path, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSlugReplacesSlashAndDot(t *testing.T) {
	if got, want := Slug(testCwd), "-Users-x-Work-multi-claude"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestFindLatestDirectPicksNewestByMtime(t *testing.T) {
	e := newEnv(t)
	dir := filepath.Join(e.p.ClaudeDir, "projects", Slug(testCwd))
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	write(t, filepath.Join(dir, "old.jsonl"), "{}\n", base)
	write(t, filepath.Join(dir, "new.jsonl"), "{}\n", base.Add(time.Hour))
	write(t, filepath.Join(dir, "note.txt"), "x", base.Add(2*time.Hour))

	s, err := FindLatest(e.p, e.from, testCwd)

	if err != nil || s.ID != "new" || s.Account != "main" || s.ProjectDir != dir || s.Path != filepath.Join(dir, "new.jsonl") {
		t.Fatalf("s=%+v err=%v", s, err)
	}
	if !s.ModTime.Equal(base.Add(time.Hour)) {
		t.Fatalf("modtime=%v", s.ModTime)
	}
}

func TestFindLatestFallbackScansCwdField(t *testing.T) {
	e := newEnv(t)
	proj := filepath.Join(e.p.ClaudeDir, "projects")
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	write(t, filepath.Join(proj, "other", "a.jsonl"), "{\"cwd\":\"/elsewhere\"}\n", base.Add(5*time.Hour))
	write(t, filepath.Join(proj, "odd-slug", "b.jsonl"), "not json\n{\"type\":\"x\"}\n{\"cwd\":\""+testCwd+"\"}\n", base)
	write(t, filepath.Join(proj, "odd-slug2", "c.jsonl"), "{\"cwd\":\""+testCwd+"\"}\n", base.Add(time.Hour))

	s, err := FindLatest(e.p, e.from, testCwd)

	if err != nil || s.ID != "c" || s.ProjectDir != filepath.Join(proj, "odd-slug2") {
		t.Fatalf("s=%+v err=%v", s, err)
	}
}

func TestFindLatestFallbackOnlyReadsFirstLines(t *testing.T) {
	e := newEnv(t)
	proj := filepath.Join(e.p.ClaudeDir, "projects", "odd")
	content := ""
	for i := 0; i < maxScanLines; i++ {
		content += "{}\n"
	}
	content += "{\"cwd\":\"" + testCwd + "\"}\n"
	write(t, filepath.Join(proj, "late.jsonl"), content, time.Time{})

	if _, err := FindLatest(e.p, e.from, testCwd); !errors.Is(err, ErrNoSession) {
		t.Fatalf("err=%v", err)
	}
}

func TestFindLatestNone(t *testing.T) {
	e := newEnv(t)
	if _, err := FindLatest(e.p, e.from, testCwd); !errors.Is(err, ErrNoSession) {
		t.Fatalf("err=%v", err)
	}
}

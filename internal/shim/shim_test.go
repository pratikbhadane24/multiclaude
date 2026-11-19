package shim

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"multiclaude/internal/links"
	"multiclaude/internal/paths"
)

type execCall struct {
	argv0 string
	argv  []string
	env   []string
}

type fixture struct {
	p      paths.Paths
	stderr *bytes.Buffer
	calls  []execCall
	items  []links.Item
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	home := t.TempDir()
	root := filepath.Join(home, ".claude-accounts")
	p := paths.Paths{
		Home:         home,
		ClaudeDir:    filepath.Join(home, ".claude"),
		ClaudeJSON:   filepath.Join(home, ".claude.json"),
		AccountsRoot: root,
		ShimPath:     filepath.Join(root, "bin", "claude"),
		RealClaude:   filepath.Join(home, ".local", "bin", "claude"),
	}
	for _, f := range []string{p.ShimPath, p.RealClaude} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return &fixture{p: p, stderr: &bytes.Buffer{}}
}

func (f *fixture) deps() Deps {
	return Deps{
		Paths:  f.p,
		Stderr: f.stderr,
		Exec: func(argv0 string, argv []string, env []string) error {
			f.calls = append(f.calls, execCall{argv0, argv, env})
			return nil
		},
		Inspect: func(paths.Paths, string) ([]links.Item, error) { return f.items, nil },
	}
}

func (f *fixture) assertExec(t *testing.T, args, env []string) {
	t.Helper()
	if len(f.calls) != 1 {
		t.Fatalf("exec calls = %d", len(f.calls))
	}
	c := f.calls[0]
	want := append([]string{"claude"}, args...)
	if c.argv0 != f.p.RealClaude || !reflect.DeepEqual(c.argv, want) || !reflect.DeepEqual(c.env, env) {
		t.Fatalf("exec = %+v", c)
	}
}

func TestMainUnsetConfigDirExecsWithoutSync(t *testing.T) {
	f := newFixture(t)
	env := []string{"PATH=/bin", "HOME=" + f.p.Home}
	args := []string{"-p", "hi"}

	err := Main(args, env, f.deps())

	if err != nil {
		t.Fatal(err)
	}
	f.assertExec(t, args, env)
	if f.stderr.Len() != 0 {
		t.Fatalf("stderr = %q (main .claude.json is missing, so a sync would warn)", f.stderr)
	}
	if _, err := os.Stat(filepath.Join(f.p.AccountsRoot, "state.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("sync ran")
	}
}

func TestMainConfigDirEqualToClaudeDirSkipsSync(t *testing.T) {
	f := newFixture(t)
	env := []string{"CLAUDE_CONFIG_DIR=" + f.p.ClaudeDir + "/"}

	if err := Main(nil, env, f.deps()); err != nil {
		t.Fatal(err)
	}
	f.assertExec(t, nil, env)
	if f.stderr.Len() != 0 {
		t.Fatalf("stderr = %q", f.stderr)
	}
}

func TestMainSyncErrorWarnsAndStillExecs(t *testing.T) {
	f := newFixture(t)
	env := []string{"CLAUDE_CONFIG_DIR=" + filepath.Join(f.p.AccountsRoot, "work")}

	err := Main([]string{"x"}, env, f.deps())

	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.stderr.String(), "ccm: warning:") {
		t.Fatalf("stderr = %q", f.stderr)
	}
	f.assertExec(t, []string{"x"}, env)
}

func TestMainSyncsAccountWhenConfigDirSet(t *testing.T) {
	f := newFixture(t)
	acc := filepath.Join(f.p.AccountsRoot, "work")
	if err := os.MkdirAll(acc, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.p.ClaudeJSON, []byte(`{"mcpServers":{"a":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{"CLAUDE_CONFIG_DIR=" + acc}

	if err := Main(nil, env, f.deps()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(acc, ".claude.json")); err != nil {
		t.Fatalf("account .claude.json not written: %v", err)
	}
	if f.stderr.Len() != 0 {
		t.Fatalf("stderr = %q", f.stderr)
	}
	f.assertExec(t, nil, env)
}

func TestMainWarnsOnlyForReplacedLinks(t *testing.T) {
	f := newFixture(t)
	f.items = []links.Item{
		{Name: "settings.json", State: links.StateReplaced},
		{Name: "CLAUDE.md", State: links.StateMissing},
		{Name: "rules", State: links.StateWrongTarget},
	}
	env := []string{"CLAUDE_CONFIG_DIR=" + filepath.Join(f.p.AccountsRoot, "work")}

	if err := Main(nil, env, f.deps()); err != nil {
		t.Fatal(err)
	}
	out := f.stderr.String()
	if !strings.Contains(out, "ccm: warning: settings.json is no longer shared — run ccm doctor\n") {
		t.Fatalf("stderr = %q", out)
	}
	if strings.Contains(out, "CLAUDE.md") || strings.Contains(out, "rules") {
		t.Fatalf("unexpected warnings: %q", out)
	}
	f.assertExec(t, nil, env)
}

func TestMainRefusesWhenRealClaudeIsTheShim(t *testing.T) {
	f := newFixture(t)
	if err := os.Remove(f.p.RealClaude); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(f.p.ShimPath, f.p.RealClaude); err != nil {
		t.Fatal(err)
	}

	err := Main(nil, nil, f.deps())

	if err == nil || !strings.Contains(err.Error(), "CCM_CLAUDE_BIN") {
		t.Fatalf("err = %v", err)
	}
	if !errors.Is(err, ErrSelfExec) {
		t.Fatalf("err = %v, want ErrSelfExec", err)
	}
	if len(f.calls) != 0 {
		t.Fatal("exec called")
	}
}

func TestMainRefusesWhenRealClaudeIsTheRunningExecutable(t *testing.T) {
	f := newFixture(t)
	d := f.deps()
	d.Self = func() (string, error) { return f.p.RealClaude, nil }

	err := Main(nil, nil, d)

	if !errors.Is(err, ErrSelfExec) {
		t.Fatalf("err = %v, want ErrSelfExec", err)
	}
	if len(f.calls) != 0 {
		t.Fatal("exec called")
	}
}

func TestMainRunsWhenExecutableDiffersFromRealClaude(t *testing.T) {
	f := newFixture(t)
	d := f.deps()
	d.Self = func() (string, error) { return f.p.ShimPath, nil }

	if err := Main(nil, nil, d); err != nil {
		t.Fatal(err)
	}
	f.assertExec(t, nil, nil)
}

func TestMainReturnsExecError(t *testing.T) {
	f := newFixture(t)
	d := f.deps()
	d.Exec = func(string, []string, []string) error { return errors.New("boom") }

	err := Main(nil, nil, d)

	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
}

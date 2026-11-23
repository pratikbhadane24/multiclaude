package worker

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/pratikbhadane24/multiclaude/internal/run"
)

func addFiles(t *testing.T, dir string, n int) {
	t.Helper()
	for i := range n {
		if err := os.WriteFile(filepath.Join(dir, "f"+string(rune('a'+i))), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPushSetupTarsItemsTheWorkerMostlyLacks(t *testing.T) {
	r := newFakeRemote()
	s := newSetup(t, r)
	addFiles(t, filepath.Join(s.P.ClaudeDir, "skills"), 4)
	addFiles(t, filepath.Join(s.P.ClaudeDir, "rules"), 2)
	r.onScript = func(script string) (run.Result, bool) {
		if strings.Contains(script, "find") {
			return run.Result{Stdout: "skills=1\nrules=2\nplugins=0\n"}, true
		}
		return run.Result{}, false
	}

	if err := s.PushSetup(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(r.tars) != 1 || !slices.Contains(r.tars[0], "skills") || slices.Contains(r.tars[0], "rules") {
		t.Fatalf("tars = %v", r.tars)
	}
	if !slices.Contains(r.tars[0], ".credentials.json") {
		t.Fatalf("tar lacks credentials exclude: %v", r.tars[0])
	}
	var rsynced []string
	for _, c := range r.rsyncs {
		rsynced = append(rsynced, filepath.Base(c.local))
	}
	if !slices.Contains(rsynced, "rules") || slices.Contains(rsynced, "skills") {
		t.Fatalf("rsyncs = %v", rsynced)
	}
}

func TestSSHTarPipesThroughSSH(t *testing.T) {
	f := &run.Fake{}
	r := SSHRemote{Host: "w1", R: f}

	if err := r.Tar(context.Background(), "/h/.claude", []string{"skills", "plugins"}, ".claude", []string{".credentials.json"}); err != nil {
		t.Fatal(err)
	}

	c := f.Calls[0]
	pipe := c.Args[len(c.Args)-1]
	for _, want := range []string{"tar -C '/h/.claude'", "--exclude '.credentials.json'", "'skills' 'plugins'", "| ssh", "w1", "tar -C '.claude' -xf -"} {
		if c.Name != "/bin/sh" || !strings.Contains(pipe, want) {
			t.Fatalf("pipeline lacks %q: %s %v", want, c.Name, c.Args)
		}
	}
}

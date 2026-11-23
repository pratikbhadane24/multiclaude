package worker

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/pratikbhadane24/multiclaude/internal/paths"
	"github.com/pratikbhadane24/multiclaude/internal/run"
)

// hubSetup makes a hub ~/.claude with shared items, history and settings.
func hubSetup(t *testing.T) paths.Paths {
	t.Helper()
	p := paths.Resolve(func(string) string { return "" }, t.TempDir())
	for _, d := range []string{"skills", "rules", "plugins", "projects", "todos"} {
		if err := os.MkdirAll(filepath.Join(p.ClaudeDir, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(p.ClaudeDir, "CLAUDE.md"):     "# rules",
		filepath.Join(p.ClaudeDir, "settings.json"): `{"model":"opus","statusLine":{"type":"command","command":"/hub/bin/ccm statusline"}}`,
		p.ClaudeJSON: `{"oauthAccount":{"emailAddress":"x"},"mcpServers":{"rea":{"command":"npx"}}}`,
	}
	for path, body := range files {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func newSetup(t *testing.T, r *fakeRemote) *Setup {
	return &Setup{P: hubSetup(t), R: r, Out: io.Discard, Yes: true, Home: "/Users/u"}
}

func TestProbeRefusesArchMismatch(t *testing.T) {
	r := newFakeRemote()
	r.respond["uname"] = run.Result{Stdout: "os=Linux\narch=x86_64\nhome=/home/u\ngit=1\n"}
	s := newSetup(t, r)

	_, err := s.Probe(context.Background())

	if err == nil || !strings.Contains(err.Error(), "Linux/x86_64") {
		t.Fatalf("err = %v", err)
	}
}

func TestProbeParsesTools(t *testing.T) {
	r := newFakeRemote()
	r.respond["uname"] = run.Result{Stdout: "os=" + hubOS() + "\narch=" + hubArch() + "\nhome=/Users/u\ngit=1\nclang=1\nclaude=0\nnpx=0\nuvx=1\n"}
	s := newSetup(t, r)

	info, err := s.Probe(context.Background())

	if err != nil || !info.HasGit || !info.HasClang || info.HasClaude || info.HasNPX || !info.HasUVX || info.Home != "/Users/u" {
		t.Fatalf("info = %+v, %v", info, err)
	}
	if s.Home != "/Users/u" {
		t.Fatalf("home not recorded: %q", s.Home)
	}
}

func TestPushSetupNeverSendsHistoryOrCredentials(t *testing.T) {
	r := newFakeRemote()
	s := newSetup(t, r)

	if err := s.PushSetup(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(r.rsyncs) == 0 {
		t.Fatal("nothing pushed")
	}
	for _, c := range r.rsyncs {
		base := filepath.Base(c.local)
		if slices.Contains([]string{"projects", "file-history", "todos", "plans", "settings.json"}, base) {
			t.Fatalf("pushed %s", base)
		}
		if !slices.Contains(c.excludes, ".credentials.json") {
			t.Fatalf("rsync of %s lacks credentials exclude: %v", base, c.excludes)
		}
	}
}

func TestPushSetupMergesSettingsAndWritesMCPHub(t *testing.T) {
	r := newFakeRemote()
	r.files[".claude/settings.json"] = []byte(`{"theme":"dark"}`)
	s := newSetup(t, r)

	if err := s.PushSetup(context.Background()); err != nil {
		t.Fatal(err)
	}

	got := map[string]os.FileMode{}
	for _, w := range r.writes {
		got[w.path] = w.mode
	}
	if got[".claude/settings.json"] != 0o600 || got[".claude-accounts/mcp-hub.json"] != 0o600 {
		t.Fatalf("writes = %+v", r.writes)
	}
	if _, ok := got[".claude.json"]; ok {
		t.Fatal("wrote ~/.claude.json")
	}
	settings := string(r.files[".claude/settings.json"])
	if !strings.Contains(settings, `"theme": "dark"`) || !strings.Contains(settings, "/Users/u/.local/bin/ccm statusline") {
		t.Fatalf("settings = %s", settings)
	}
	if hub := string(r.files[".claude-accounts/mcp-hub.json"]); !strings.Contains(hub, `"rea"`) || strings.Contains(hub, "oauthAccount") {
		t.Fatalf("mcp-hub = %s", hub)
	}
	if !slices.ContainsFunc(r.scripts, func(s string) bool { return strings.Contains(s, "config set default-account best") }) {
		t.Fatalf("default-account not set: %v", r.scripts)
	}
}

func TestPushSetupSecondRunWritesNothing(t *testing.T) {
	r := newFakeRemote()
	s := newSetup(t, r)
	if err := s.PushSetup(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.writes = nil

	if err := s.PushSetup(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(r.writes) != 0 {
		t.Fatalf("second run wrote %+v", r.writes)
	}
}

func TestShellBlocksIdempotent(t *testing.T) {
	r := newFakeRemote()
	r.files[".zshrc"] = []byte("alias ll='ls -l'\n")
	s := newSetup(t, r)

	if err := s.ShellBlocks(context.Background()); err != nil {
		t.Fatal(err)
	}
	first := len(r.writes)
	if err := s.ShellBlocks(context.Background()); err != nil {
		t.Fatal(err)
	}

	if first != 2 || len(r.writes) != 2 {
		t.Fatalf("writes: first=%d total=%d", first, len(r.writes))
	}
	if !strings.HasPrefix(string(r.files[".zshrc"]), "alias ll='ls -l'\n") {
		t.Fatalf("zshrc = %s", r.files[".zshrc"])
	}
}

func TestShellBlocksDeclinedWritesNothing(t *testing.T) {
	r := newFakeRemote()
	s := newSetup(t, r)
	s.Yes = false
	s.Confirm = func(string) bool { return false }

	err := s.ShellBlocks(context.Background())

	if err == nil || len(r.writes) != 0 {
		t.Fatalf("err=%v writes=%v", err, r.writes)
	}
}

func TestEnsureAccountsRefusesMain(t *testing.T) {
	s := newSetup(t, newFakeRemote())

	if err := s.EnsureAccounts(context.Background(), []string{"main"}, nil); err == nil {
		t.Fatal("want error")
	}
}

func TestEnsureAccountsSkipsLoggedIn(t *testing.T) {
	r := newFakeRemote()
	r.respond["list --json"] = run.Result{Stdout: `[{"name":"main","loggedIn":true},{"name":"inbox3","email":"i3@x","loggedIn":true}]`}
	s := newSetup(t, r)

	err := s.EnsureAccounts(context.Background(), []string{"inbox2", "inbox3"}, map[string]string{"inbox2": "i2@x", "inbox3": "i3@x"})

	if err != nil {
		t.Fatal(err)
	}
	if len(r.interactive) != 1 || !strings.Contains(r.interactive[0], "add inbox2 --email i2@x") {
		t.Fatalf("interactive = %v", r.interactive)
	}
}

func TestRunStepsStopsAtFirstFailure(t *testing.T) {
	var ran []string
	steps := []Step{
		{Name: "a", Run: func(context.Context) error { ran = append(ran, "a"); return nil }},
		{Name: "b", Run: func(context.Context) error { ran = append(ran, "b"); return os.ErrPermission }},
		{Name: "c", Run: func(context.Context) error { ran = append(ran, "c"); return nil }},
	}
	var out strings.Builder

	err := RunSteps(context.Background(), &out, steps)

	if err == nil || !slices.Equal(ran, []string{"a", "b"}) || !strings.Contains(out.String(), "✓ a") || !strings.Contains(out.String(), "✗ b") {
		t.Fatalf("err=%v ran=%v out=%q", err, ran, out.String())
	}
}

func TestEnsureAccountsRejectsShellUnsafeEmail(t *testing.T) {
	r := newFakeRemote()
	r.respond["list --json"] = run.Result{Stdout: `[]`}
	s := newSetup(t, r)

	err := s.EnsureAccounts(context.Background(), []string{"inbox2"}, map[string]string{"inbox2": "a@b;rm -rf ~"})

	if err == nil || len(r.interactive) != 0 {
		t.Fatalf("err=%v interactive=%v", err, r.interactive)
	}
}

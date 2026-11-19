package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"multiclaude/internal/paths"
	"multiclaude/internal/registry"
	"multiclaude/internal/run"
)

const signInLine = "Sign in as you@x.com in the browser. If it's signed in to another Claude account, open the printed URL in a private window."

type harness struct {
	app      *App
	fake     *run.Fake
	out, err *bytes.Buffer
	p        paths.Paths
	loggedIn map[string]bool   // by CLAUDE_CONFIG_DIR value ("" = main)
	emails   map[string]string // email reported after login
}

func configDir(env []string) string {
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "CLAUDE_CONFIG_DIR="); ok {
			return v
		}
	}
	return ""
}

func newHarness(t *testing.T, in string) *harness {
	t.Helper()
	root := t.TempDir()
	p := paths.Paths{
		Home: root, ClaudeDir: filepath.Join(root, ".claude"), ClaudeJSON: filepath.Join(root, ".claude.json"),
		AccountsRoot: filepath.Join(root, "accts"), ShimPath: filepath.Join(root, "accts", "bin", "claude"), RealClaude: "/real/claude",
	}
	if err := os.MkdirAll(p.ClaudeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.ClaudeDir, "CLAUDE.md"), []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.ClaudeJSON, []byte(`{"mcpServers":{"s":{"command":"x"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h := &harness{p: p, out: &bytes.Buffer{}, err: &bytes.Buffer{}, loggedIn: map[string]bool{}, emails: map[string]string{}}
	h.fake = &run.Fake{Respond: func(c run.Call) (run.Result, error) {
		dir := configDir(c.Env)
		if c.Args[1] == "login" {
			h.loggedIn[dir] = true
			return run.Result{}, nil
		}
		if !h.loggedIn[dir] {
			return run.Result{Stdout: `{"loggedIn":false}`}, nil
		}
		return run.Result{Stdout: `{"loggedIn":true,"email":"` + h.emails[dir] + `"}`}, nil
	}}
	h.app = &App{Paths: p, Runner: h.fake, Out: h.out, Err: h.err, In: strings.NewReader(in)}
	return h
}

func (h *harness) loginCalls() int {
	n := 0
	for _, c := range h.fake.Calls {
		if c.Args[1] == "login" {
			n++
		}
	}
	return n
}

func TestAddReservedName(t *testing.T) {
	h := newHarness(t, "")
	if code := h.app.Add(context.Background(), "status", "you@x.com"); code != 1 {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(h.err.String(), "reserved") {
		t.Fatalf("err: %s", h.err)
	}
}

func TestAddCreatesAccountAndLogsInOnce(t *testing.T) {
	h := newHarness(t, "")
	dir := filepath.Join(h.p.AccountsRoot, "work")
	h.emails[dir] = "you@x.com"
	if code := h.app.Add(context.Background(), "work", "you@x.com"); code != 0 {
		t.Fatalf("code %d err %s", code, h.err)
	}
	info, err := os.Stat(dir)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("dir: %v %v", info, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "CLAUDE.md")); err != nil {
		t.Fatalf("link missing: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".claude.json"))
	if err != nil || !strings.Contains(string(data), `"s"`) {
		t.Fatalf("mcp not synced: %s %v", data, err)
	}
	accts, _ := registry.Load(h.p)
	if a, ok := registry.Find(accts, "work"); !ok || a.Email != "you@x.com" {
		t.Fatalf("registry: %+v", accts)
	}
	if h.loginCalls() != 1 || !strings.Contains(h.out.String(), signInLine) {
		t.Fatalf("logins %d out %s", h.loginCalls(), h.out)
	}
}

func TestAddResumeNoDuplicate(t *testing.T) {
	h := newHarness(t, "")
	dir := filepath.Join(h.p.AccountsRoot, "work")
	h.emails[dir] = "you@x.com"
	h.app.Add(context.Background(), "work", "you@x.com")
	if code := h.app.Add(context.Background(), "work", ""); code != 0 {
		t.Fatalf("code %d err %s", code, h.err)
	}
	accts, _ := registry.Load(h.p)
	if len(accts) != 2 {
		t.Fatalf("registry: %+v", accts)
	}
	if h.loginCalls() != 1 {
		t.Fatalf("logins %d, want 1", h.loginCalls())
	}
}

func TestAddPromptsForEmail(t *testing.T) {
	h := newHarness(t, "you@x.com\n")
	h.emails[filepath.Join(h.p.AccountsRoot, "work")] = "you@x.com"
	if code := h.app.Add(context.Background(), "work", ""); code != 0 {
		t.Fatalf("code %d err %s", code, h.err)
	}
	if !strings.Contains(h.out.String(), `Email for account "work": `) {
		t.Fatalf("out: %s", h.out)
	}
}

func TestAddEmptyEmailFails(t *testing.T) {
	h := newHarness(t, "\n")
	if code := h.app.Add(context.Background(), "work", ""); code != 1 {
		t.Fatalf("code %d", code)
	}
}

func TestAddDuplicateEmail(t *testing.T) {
	h := newHarness(t, "")
	h.emails[filepath.Join(h.p.AccountsRoot, "a")] = "you@x.com"
	h.app.Add(context.Background(), "a", "you@x.com")
	if code := h.app.Add(context.Background(), "b", "you@x.com"); code != 1 {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(h.err.String(), `already used by account "a"`) {
		t.Fatalf("err: %s", h.err)
	}
}

func TestAddMismatchExit2(t *testing.T) {
	h := newHarness(t, "")
	h.emails[filepath.Join(h.p.AccountsRoot, "work")] = "other@x.com"
	if code := h.app.Add(context.Background(), "work", "you@x.com"); code != 2 {
		t.Fatalf("code %d", code)
	}
}

func TestLoginAllOnlyLoggedOutInOrder(t *testing.T) {
	h := newHarness(t, "")
	a, b, c := "a", "b", "c"
	var accts []registry.Account
	for _, n := range []string{a, b, c} {
		d := filepath.Join(h.p.AccountsRoot, n)
		h.emails[d] = n + "@x.com"
		accts = append(accts, registry.Account{Name: n, Dir: d, Email: n + "@x.com"})
	}
	if err := registry.Save(h.p, append([]registry.Account{{Name: "main"}}, accts...)); err != nil {
		t.Fatal(err)
	}
	h.loggedIn[""] = true
	h.emails[""] = "m@x.com"
	h.loggedIn[accts[1].Dir] = true
	if code := h.app.Login(context.Background(), "--all"); code != 0 {
		t.Fatalf("code %d err %s", code, h.err)
	}
	var logged []string
	for _, call := range h.fake.Calls {
		if call.Args[1] == "login" {
			logged = append(logged, configDir(call.Env))
		}
	}
	if len(logged) != 2 || logged[0] != accts[0].Dir || logged[1] != accts[2].Dir {
		t.Fatalf("logged %v", logged)
	}
}

func TestLoginAllNothingToDo(t *testing.T) {
	h := newHarness(t, "")
	h.loggedIn[""] = true
	if code := h.app.Login(context.Background(), "--all"); code != 0 {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(h.out.String(), "All accounts are logged in.") || h.loginCalls() != 0 {
		t.Fatalf("out %s", h.out)
	}
}

func TestAddAfterMismatchDoesNotReportSuccess(t *testing.T) {
	h := newHarness(t, "")
	dir := filepath.Join(h.p.AccountsRoot, "work")
	h.emails[dir] = "other@x.com"
	if code := h.app.Add(context.Background(), "work", "you@x.com"); code != 2 {
		t.Fatalf("first code %d", code)
	}
	if code := h.app.Add(context.Background(), "work", ""); code != 2 {
		t.Fatalf("second code %d out %s", code, h.out)
	}
	if h.loginCalls() != 2 {
		t.Fatalf("logins %d", h.loginCalls())
	}
	if !strings.Contains(h.err.String(), "other@x.com") {
		t.Fatalf("mismatch not on stderr: %s", h.err)
	}
}

func TestLoginAllRelogsMismatchedEmail(t *testing.T) {
	h := newHarness(t, "")
	dir := filepath.Join(h.p.AccountsRoot, "a")
	h.emails[dir] = "other@x.com"
	h.loggedIn[dir] = true
	acct := registry.Account{Name: "a", Dir: dir, Email: "a@x.com"}
	if err := registry.Save(h.p, []registry.Account{{Name: "main"}, acct}); err != nil {
		t.Fatal(err)
	}
	h.loggedIn[""] = true
	if code := h.app.Login(context.Background(), "--all"); code != 2 {
		t.Fatalf("code %d", code)
	}
	if h.loginCalls() != 1 {
		t.Fatalf("logins %d", h.loginCalls())
	}
}

func TestLoginAllWarnsOnStatusFailure(t *testing.T) {
	h := newHarness(t, "")
	h.fake.Respond = func(c run.Call) (run.Result, error) {
		if c.Args[1] == "status" {
			return run.Result{Stdout: "garbage"}, nil
		}
		return run.Result{}, nil
	}
	h.app.Login(context.Background(), "--all")
	if !strings.Contains(h.err.String(), "ccm: warning: main: status check failed") {
		t.Fatalf("err: %s", h.err)
	}
}

func TestAddWarnsOnMCPSyncFailureAndContinues(t *testing.T) {
	h := newHarness(t, "")
	if err := os.WriteFile(h.p.ClaudeJSON, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.emails[filepath.Join(h.p.AccountsRoot, "work")] = "you@x.com"
	if code := h.app.Add(context.Background(), "work", "you@x.com"); code != 0 {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(h.err.String(), "ccm: warning: mcp sync") || h.loginCalls() != 1 {
		t.Fatalf("err %s logins %d", h.err, h.loginCalls())
	}
}

func TestAddWithPreexistingDir(t *testing.T) {
	h := newHarness(t, "")
	dir := filepath.Join(h.p.AccountsRoot, "work")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	h.emails[dir] = "you@x.com"
	if code := h.app.Add(context.Background(), "work", "you@x.com"); code != 0 {
		t.Fatalf("code %d err %s", code, h.err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "CLAUDE.md")); err != nil {
		t.Fatalf("link missing: %v", err)
	}
}

func TestLoginUnknownAccount(t *testing.T) {
	h := newHarness(t, "")
	if code := h.app.Login(context.Background(), "nope"); code != 1 {
		t.Fatalf("code %d", code)
	}
}

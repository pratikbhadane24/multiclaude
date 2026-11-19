package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"multiclaude/internal/keychain"
	"multiclaude/internal/paths"
	"multiclaude/internal/registry"
	"multiclaude/internal/run"
)

const (
	testToken    = "sk-test-SECRET-token"
	securityPath = "/usr/bin/security"
	workEmail    = "w@x.com"
)

var testNow = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

// cliEnv is a temp-dir ccm layout with fake runner and HTTP.
type cliEnv struct {
	app      *App
	fake     *run.Fake
	out, err *bytes.Buffer
	p        paths.Paths
	workDir  string
	mu       sync.Mutex
	creds    map[string]bool   // keychain service -> present
	usage    map[string]string // keychain service -> usage body (token is per service)
	exitCode int               // code returned for interactive runs
}

type fakeTransport struct{ env *cliEnv }

func (f fakeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	svc := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "+testToken+"|")
	f.env.mu.Lock()
	body, ok := f.env.usage[svc]
	f.env.mu.Unlock()
	code := http.StatusOK
	if !ok {
		code, body = http.StatusInternalServerError, "oops"
	}
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: r}, nil
}

func newCLI(t *testing.T, in string) *cliEnv {
	t.Helper()
	root := t.TempDir()
	p := paths.Paths{
		Home: root, ClaudeDir: filepath.Join(root, ".claude"), ClaudeJSON: filepath.Join(root, ".claude.json"),
		AccountsRoot: filepath.Join(root, "accts"), ShimPath: filepath.Join(root, "accts", "bin", "claude"),
		RealClaude: filepath.Join(root, "real", "claude"), T3Settings: filepath.Join(root, ".t3", "userdata", "settings.json"),
	}
	e := &cliEnv{p: p, out: &bytes.Buffer{}, err: &bytes.Buffer{}, workDir: filepath.Join(p.AccountsRoot, "work"),
		creds: map[string]bool{}, usage: map[string]string{}}
	mustMkdir(t, p.ClaudeDir)
	mustMkdir(t, e.workDir)
	mustWrite(t, filepath.Join(p.ClaudeDir, "CLAUDE.md"), "shared")
	mustWrite(t, p.ClaudeJSON, `{"mcpServers":{"s":{"command":"x"}}}`)
	accts := []registry.Account{{Name: "main"}, {Name: "work", Dir: e.workDir, Email: workEmail}}
	if err := registry.Save(p, accts); err != nil {
		t.Fatal(err)
	}
	e.fake = &run.Fake{Respond: e.respond}
	e.app = &App{Paths: p, Runner: e.fake, HTTP: &http.Client{Transport: fakeTransport{e}},
		Now: func() time.Time { return testNow }, Out: e.out, Err: e.err, In: strings.NewReader(in)}
	return e
}

func (e *cliEnv) respond(c run.Call) (run.Result, error) {
	if c.Name == securityPath && c.Args[0] == "find-generic-password" {
		svc := c.Args[2]
		e.mu.Lock()
		ok := e.creds[svc]
		e.mu.Unlock()
		if !ok {
			return run.Result{Code: 44}, nil
		}
		return run.Result{Stdout: `{"claudeAiOauth":{"accessToken":"` + testToken + "|" + svc + `","subscriptionType":"max"}}`}, nil
	}
	if c.Name == securityPath {
		return run.Result{}, nil
	}
	if len(c.Args) >= 2 && c.Args[0] == "auth" && c.Args[1] == "status" {
		return run.Result{Stdout: `{"loggedIn":false}`}, nil
	}
	return run.Result{Code: e.exitCode}, nil
}

// loginWithUsage marks an account as logged in, with a usage response body.
func (e *cliEnv) loginWithUsage(dir, body string) {
	svc := keychain.Service(dir)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.creds[svc] = true
	e.usage[svc] = body
}

func (e *cliEnv) main(args ...string) int {
	return e.app.Main(context.Background(), args)
}

func (e *cliEnv) callsTo(name string) []run.Call {
	var out []run.Call
	for _, c := range e.fake.Calls {
		if c.Name == name {
			out = append(out, c)
		}
	}
	return out
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func usageBody(five, seven string) string {
	return `{"five_hour":{"utilization":` + five + `,"resets_at":"2026-11-19T13:12:00Z"},"seven_day":{"utilization":` + seven + `,"resets_at":null}}`
}

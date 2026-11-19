package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// healthyLayout makes every doctor global check pass: executable real
// claude and shim, a t3 binary on PATH, both accounts logged in.
func healthyLayout(t *testing.T, e *cliEnv) {
	t.Helper()
	for _, f := range []string{e.p.RealClaude, e.p.ShimPath} {
		mustWrite(t, f, "#!/bin/sh\n")
		if err := os.Chmod(f, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(e.p.Home, "pathbin")
	mustWrite(t, filepath.Join(bin, "t3"), "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(bin, "t3"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	e.loginWithUsage("", usageBody("10", "10"))
	e.loginWithUsage(e.workDir, usageBody("10", "10"))
}

func TestDoctorReportsReplacedLinkThenFixWithBackup(t *testing.T) {
	e := newCLI(t, "")
	healthyLayout(t, e)
	if code := e.main("sync"); code != ExitOK {
		t.Fatalf("sync code=%d err=%s", code, e.err)
	}
	if code := e.main("t3-sync"); code != ExitOK {
		t.Fatalf("t3-sync code=%d err=%s", code, e.err)
	}
	link := filepath.Join(e.workDir, "CLAUDE.md")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, link, "local edits")
	e.out.Reset()

	if code := e.main("doctor"); code != ExitError || !strings.Contains(e.out.String(), "[work] link CLAUDE.md") {
		t.Fatalf("doctor code=%d out=%s", code, e.out)
	}
	e.out.Reset()
	code := e.main("doctor", "--fix")

	if code != ExitOK || !strings.Contains(e.out.String(), "backup: ") || !strings.Contains(e.out.String(), "doctor: all good") {
		t.Fatalf("doctor --fix code=%d out=%s err=%s", code, e.out, e.err)
	}
}

func TestDoctorFixesMCPDrift(t *testing.T) {
	e := newCLI(t, "")
	healthyLayout(t, e)
	e.main("sync")
	e.main("t3-sync")
	mustWrite(t, e.p.ClaudeJSON, `{"mcpServers":{"s":{"command":"x"},"t":{"command":"y"}}}`)
	e.out.Reset()

	if code := e.main("doctor"); code != ExitError || !strings.Contains(e.out.String(), "MCP servers out of sync") {
		t.Fatalf("code=%d out=%s", code, e.out)
	}
	if code := e.main("doctor", "--fix"); code != ExitOK {
		t.Fatalf("fix code=%d out=%s", code, e.out)
	}
}

func TestStatusJSONHasAccountsAndNoToken(t *testing.T) {
	e := newCLI(t, "")
	e.loginWithUsage("", usageBody("40", "20"))
	e.loginWithUsage(e.workDir, usageBody("30", "10"))

	code := e.main("status", "--json")

	if code != ExitOK {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
	var got struct {
		Accounts []struct {
			Account, State string
			LinkWarnings   []string `json:"link_warnings"`
		}
	}
	if err := json.Unmarshal(e.out.Bytes(), &got); err != nil {
		t.Fatalf("bad json: %v\n%s", err, e.out)
	}
	if len(got.Accounts) != 2 || got.Accounts[1].Account != "work" || got.Accounts[1].State != "ok" {
		t.Fatalf("accounts=%+v", got.Accounts)
	}
	if len(got.Accounts[0].LinkWarnings) != 0 {
		t.Fatalf("main must not report link warnings: %v", got.Accounts[0].LinkWarnings)
	}
	if strings.Contains(e.out.String(), testToken) || strings.Contains(strings.ToLower(e.out.String()), "accesstoken") {
		t.Fatal("token leaked")
	}
}

func TestStatusTableRenders(t *testing.T) {
	e := newCLI(t, "")
	e.loginWithUsage(e.workDir, usageBody("30", "10"))
	if code := e.main("status"); code != ExitOK || !strings.Contains(e.out.String(), "ACCOUNT") || !strings.Contains(e.out.String(), "ALL") {
		t.Fatalf("code=%d out=%s", code, e.out)
	}
}

type cancelWriter struct {
	cancel context.CancelFunc
	buf    strings.Builder
}

func (w *cancelWriter) Write(b []byte) (int, error) {
	w.cancel()
	return w.buf.Write(b)
}

func TestStatusWatchClearsScreenAndStopsOnCancel(t *testing.T) {
	e := newCLI(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	w := &cancelWriter{cancel: cancel}
	e.app.Out = w
	done := make(chan int, 1)

	go func() { done <- e.app.Main(ctx, []string{"status", "--watch", "1h"}) }()

	select {
	case code := <-done:
		if code != ExitOK || !strings.HasPrefix(w.buf.String(), clearScreen) {
			t.Fatalf("code=%d out=%q", code, w.buf.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watch did not stop on cancel")
	}
}

func TestSyncLinksAndReportsErrors(t *testing.T) {
	e := newCLI(t, "")
	if code := e.main("sync"); code != ExitOK || !strings.Contains(e.out.String(), "linked CLAUDE.md") {
		t.Fatalf("code=%d out=%s err=%s", code, e.out, e.err)
	}
	if _, err := os.Lstat(filepath.Join(e.workDir, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(e.workDir, ".claude.json"), "{corrupt")
	e.out.Reset()

	if code := e.main("sync"); code != ExitError || !strings.Contains(e.err.String(), "ccm: warning: work") {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
}

func TestT3SyncSecondRunIsUpToDate(t *testing.T) {
	e := newCLI(t, "")
	e.main("t3-sync")
	if !strings.Contains(e.out.String(), "Added: claude-main, claude-work") {
		t.Fatalf("out=%s", e.out)
	}
	e.out.Reset()
	if code := e.main("t3-sync"); code != ExitOK || !strings.Contains(e.out.String(), "T3 Code already up to date.") {
		t.Fatalf("code=%d out=%s", code, e.out)
	}
}

func TestListShowsAccountsAndLoginState(t *testing.T) {
	e := newCLI(t, "")
	e.main("list")
	out := e.out.String()
	for _, want := range []string{"NAME", "DIR", "EMAIL", "LOGGED IN", "main", e.p.ClaudeDir, "work", e.workDir, workEmail, "no"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

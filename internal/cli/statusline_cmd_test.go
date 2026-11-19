package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"multiclaude/internal/status"
)

const cavemanSettings = `{
  "theme": "dark",
  "statusLine": {"type":"command","command":"bash \"/Users/x/caveman-statusline.sh\""},
  "env": {"A": "<&>"}
}
`

func settingsPath(e *cliEnv) string { return filepath.Join(e.p.ClaudeDir, "settings.json") }

func writeSettings(t *testing.T, e *cliEnv, body string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(settingsPath(e), []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(settingsPath(e), mode); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func backups(e *cliEnv) []string {
	m, _ := filepath.Glob(settingsPath(e) + ".ccm-backup-*")
	return m
}

func TestStatuslineInstallWrapsCavemanWithBackupAndMode(t *testing.T) {
	e := newCLI(t, "y\n")
	writeSettings(t, e, cavemanSettings, 0o640)

	code := e.main("statusline", "install")

	if code != ExitOK {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
	got := readFile(t, settingsPath(e))
	want := `{"type":"command","command":"/bin/ccm statusline --wrap 'bash \"/Users/x/caveman-statusline.sh\"'"}`
	if !strings.Contains(got, want) || !strings.HasPrefix(got, "{\n  \"theme\": \"dark\",\n  \"statusLine\": ") || !strings.HasSuffix(got, ",\n  \"env\": {\"A\": \"<&>\"}\n}\n") {
		t.Fatalf("settings:\n%s", got)
	}
	if st, _ := os.Stat(settingsPath(e)); st.Mode().Perm() != 0o640 {
		t.Fatalf("mode %v", st.Mode())
	}
	bs := backups(e)
	if len(bs) != 1 || readFile(t, bs[0]) != cavemanSettings || !strings.HasSuffix(bs[0], "-1795089600") {
		t.Fatalf("backups=%v", bs)
	}
	if !strings.Contains(e.out.String(), "Install ccm status line into ~/.claude/settings.json? [y/N]") ||
		!strings.Contains(e.out.String(), "before:") || !strings.Contains(e.out.String(), "after:") {
		t.Fatalf("out=%s", e.out)
	}
}

func TestStatuslineInstallDeclineWritesNothing(t *testing.T) {
	for _, answer := range []string{"n\n", "\n", ""} {
		e := newCLI(t, answer)
		writeSettings(t, e, cavemanSettings, 0o600)

		code := e.main("statusline", "install")

		if code != ExitOK && answer != "" {
			t.Fatalf("answer %q code=%d", answer, code)
		}
		if readFile(t, settingsPath(e)) != cavemanSettings || len(backups(e)) != 0 {
			t.Fatalf("answer %q modified files", answer)
		}
	}
}

func TestStatuslineInstallNoStatusLineAndNoFile(t *testing.T) {
	e := newCLI(t, "y\n")
	writeSettings(t, e, "{\n  \"model\": \"opus\"\n}\n", 0o600)
	if code := e.main("statusline", "install"); code != ExitOK {
		t.Fatalf("code=%d %s", code, e.err)
	}
	if got := readFile(t, settingsPath(e)); !strings.Contains(got, `"model": "opus",`) || !strings.Contains(got, `"command":"/bin/ccm statusline"`) {
		t.Fatalf("got %s", got)
	}

	e2 := newCLI(t, "y\n")
	_ = os.Remove(settingsPath(e2))
	if code := e2.main("statusline", "install"); code != ExitOK {
		t.Fatalf("code=%d %s", code, e2.err)
	}
	if !strings.Contains(readFile(t, settingsPath(e2)), `"/bin/ccm statusline"`) || len(backups(e2)) != 0 {
		t.Fatal("new file case wrong")
	}
}

func TestStatuslineInstallIdempotent(t *testing.T) {
	e := newCLI(t, "y\n")
	writeSettings(t, e, cavemanSettings, 0o600)
	e.main("statusline", "install")
	first := readFile(t, settingsPath(e))
	e.out.Reset()

	code := e.main("statusline", "install")

	if code != ExitOK || strings.TrimSpace(e.out.String()) != "already installed" || readFile(t, settingsPath(e)) != first {
		t.Fatalf("code=%d out=%q", code, e.out)
	}
	if len(backups(e)) != 1 {
		t.Fatalf("backups=%v", backups(e))
	}
}

func TestStatuslineInstallNonCommandTypeWarns(t *testing.T) {
	e := newCLI(t, "y\n")
	writeSettings(t, e, `{"statusLine":{"type":"static","text":"x"}}`, 0o600)

	code := e.main("statusline", "install")

	if code != ExitOK || !strings.Contains(e.err.String(), "warning") || strings.Contains(readFile(t, settingsPath(e)), "static") {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
}

func TestStatuslineInstallRejectsBadSettings(t *testing.T) {
	e := newCLI(t, "y\n")
	writeSettings(t, e, `[not an object`, 0o600)
	if code := e.main("statusline", "install"); code != ExitError {
		t.Fatalf("code=%d", code)
	}
	if readFile(t, settingsPath(e)) != `[not an object` {
		t.Fatal("file modified")
	}
}

func TestStatuslineUninstallRestoresOriginal(t *testing.T) {
	e := newCLI(t, "y\n")
	writeSettings(t, e, cavemanSettings, 0o600)
	e.main("statusline", "install")
	e.app.In = strings.NewReader("y\n")

	code := e.main("statusline", "uninstall")

	if code != ExitOK || readFile(t, settingsPath(e)) != cavemanSettings {
		t.Fatalf("code=%d\n%s", code, readFile(t, settingsPath(e)))
	}
	if len(backups(e)) != 1 {
		t.Fatalf("second backup in same second overwrites the first; backups=%v", backups(e))
	}
}

func TestStatuslineUninstallFreshAndDeclineAndNotInstalled(t *testing.T) {
	orig := "{\n  \"a\": 1\n}\n"
	e := newCLI(t, "y\n")
	writeSettings(t, e, orig, 0o600)
	e.main("statusline", "install")
	installed := readFile(t, settingsPath(e))
	e.app.In = strings.NewReader("n\n")

	if code := e.main("statusline", "uninstall"); code != ExitOK || readFile(t, settingsPath(e)) != installed {
		t.Fatalf("declined uninstall changed file (code %d)", code)
	}
	e.app.In = strings.NewReader("y\n")
	if code := e.main("statusline", "uninstall"); code != ExitOK || readFile(t, settingsPath(e)) != orig {
		t.Fatalf("fresh uninstall not restored (code %d):\n%s", code, readFile(t, settingsPath(e)))
	}
	e2 := newCLI(t, "")
	writeSettings(t, e2, `{"a":1}`, 0o600)
	e2.main("statusline", "uninstall")
	if strings.TrimSpace(e2.out.String()) != "not installed" {
		t.Fatalf("out=%q", e2.out)
	}
}

func TestStatusWritesCacheAndRefreshCacheIsQuiet(t *testing.T) {
	e := newCLI(t, "")
	if code := e.main("status"); code != ExitOK {
		t.Fatalf("code=%d %s", code, e.err)
	}
	c, err := status.ReadCache(e.p)
	if err != nil || len(c.Accounts) != 2 || !c.WrittenAt.Equal(testNow) {
		t.Fatalf("cache=%+v err=%v", c, err)
	}
	_ = os.Remove(status.CachePath(e.p))
	e.out.Reset()

	code := e.main("status", "--refresh-cache")

	if code != ExitOK || e.out.Len() != 0 {
		t.Fatalf("code=%d out=%q", code, e.out)
	}
	if _, err := status.ReadCache(e.p); err != nil {
		t.Fatalf("cache not written: %v", err)
	}
}

func TestStatuslineCommandRendersFromCacheWithWrap(t *testing.T) {
	e := newCLI(t, `{"model":"m"}`)
	e.main("status", "--refresh-cache")
	t.Setenv("CLAUDE_CONFIG_DIR", e.workDir)
	e.app.In = strings.NewReader(`{"model":"m"}`)
	e.out.Reset()

	code := e.main("statusline", "--wrap", "cat >/dev/null; echo WRAPPED")

	if code != ExitOK || !strings.HasPrefix(e.out.String(), "WRAPPED │ ccm: work") || strings.Count(e.out.String(), "\n") != 1 {
		t.Fatalf("code=%d out=%q", code, e.out)
	}
}

func TestStatuslineCommandMissingCacheSpawnsRefresher(t *testing.T) {
	e := newCLI(t, "{}")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	spawned := 0
	old := startRefresh
	startRefresh = func(string) error { spawned++; return nil }
	defer func() { startRefresh = old }()

	code := e.main("statusline")

	if code != ExitOK || e.out.String() != "ccm: main\n" || spawned != 1 {
		t.Fatalf("code=%d out=%q spawned=%d", code, e.out, spawned)
	}
	if len(e.fake.Calls) != 0 {
		t.Fatalf("render path ran external commands (keychain?): %v", e.fake.Calls)
	}
}

func TestCacheJSONHasNoSecrets(t *testing.T) {
	e := newCLI(t, "")
	e.main("status", "--refresh-cache")
	b := readFile(t, status.CachePath(e.p))
	var v map[string]any
	if json.Unmarshal([]byte(b), &v) != nil || strings.Contains(b, testToken) {
		t.Fatalf("bad cache: %s", b)
	}
}

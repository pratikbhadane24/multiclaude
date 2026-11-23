package worker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/paths"
)

const orcaHook = `if [ -f "$HOME/.orca/agent-hooks/claude-hook.sh" ]; then x; fi`

func hookSettings(statusCmd string, cmds ...string) []byte {
	hooks := make([]map[string]any, 0, len(cmds))
	for _, c := range cmds {
		hooks = append(hooks, map[string]any{"type": "command", "command": c})
	}
	m := map[string]any{
		"hooks": map[string]any{"Stop": []any{map[string]any{"matcher": "", "hooks": hooks}}},
	}
	if statusCmd != "" {
		m["statusLine"] = map[string]any{"type": "command", "command": statusCmd}
	}
	b, _ := json.Marshal(m)
	return b
}

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("bad json: %v\n%s", err, b)
	}
	return m
}

func stopCommands(t *testing.T, b []byte) []string {
	t.Helper()
	var out []string
	groups := decode(t, b)["hooks"].(map[string]any)["Stop"].([]any)
	for _, g := range groups {
		for _, h := range g.(map[string]any)["hooks"].([]any) {
			out = append(out, h.(map[string]any)["command"].(string))
		}
	}
	return out
}

func TestMergeSettingsRewritesCCMStatusLine(t *testing.T) {
	hub := hookSettings(`/Users/u/Work/multiclaude/bin/ccm statusline --wrap 'x'`)

	got, err := MergeSettings(hub, nil, "/Users/u/.local/bin/ccm")

	if err != nil {
		t.Fatal(err)
	}
	cmd := decode(t, got)["statusLine"].(map[string]any)["command"]
	if cmd != `/Users/u/.local/bin/ccm statusline --wrap 'x'` {
		t.Fatalf("statusLine = %q", cmd)
	}
}

func TestMergeSettingsKeepsNonCCMStatusLine(t *testing.T) {
	got, err := MergeSettings(hookSettings("bash foo.sh"), nil, "/w/ccm")

	if err != nil {
		t.Fatal(err)
	}
	if cmd := decode(t, got)["statusLine"].(map[string]any)["command"]; cmd != "bash foo.sh" {
		t.Fatalf("statusLine = %q", cmd)
	}
}

func TestMergeSettingsUnionsHooksNoDuplicates(t *testing.T) {
	hub := hookSettings("", orcaHook, "hub-only")
	worker := hookSettings("", orcaHook, "worker-only")

	got, err := MergeSettings(hub, worker, "/w/ccm")

	if err != nil {
		t.Fatal(err)
	}
	want := []string{orcaHook, "hub-only", "worker-only"}
	if cmds := stopCommands(t, got); strings.Join(cmds, "|") != strings.Join(want, "|") {
		t.Fatalf("Stop hooks = %q, want %q", cmds, want)
	}
}

func TestMergeSettingsKeepsWorkerOnlyKeys(t *testing.T) {
	hub := []byte(`{"model":"opus"}`)
	worker := []byte(`{"model":"haiku","theme":"dark"}`)

	got, err := MergeSettings(hub, worker, "/w/ccm")

	if err != nil {
		t.Fatal(err)
	}
	m := decode(t, got)
	if m["model"] != "opus" || m["theme"] != "dark" {
		t.Fatalf("merged = %v", m)
	}
}

func TestMergeSettingsIdempotent(t *testing.T) {
	hub := hookSettings("/h/ccm statusline", orcaHook, "a")
	worker := hookSettings("", orcaHook, "b")

	once, err := MergeSettings(hub, worker, "/w/ccm")
	if err != nil {
		t.Fatal(err)
	}
	twice, err := MergeSettings(hub, once, "/w/ccm")

	if err != nil || string(once) != string(twice) {
		t.Fatalf("not idempotent:\n%s\n---\n%s", once, twice)
	}
}

func TestMergeSettingsRejectsBadHubJSON(t *testing.T) {
	if _, err := MergeSettings([]byte("{"), nil, "/w/ccm"); err == nil {
		t.Fatal("want error")
	}
}

func TestUpsertBlockAppendsOnce(t *testing.T) {
	once := UpsertBlock("export A=1\n", "echo hi")
	twice := UpsertBlock(once, "echo hi")

	if once != twice || strings.Count(twice, BlockStart) != 1 {
		t.Fatalf("got:\n%s", twice)
	}
	if !strings.HasPrefix(once, "export A=1\n") {
		t.Fatalf("existing content lost:\n%s", once)
	}
}

func TestUpsertBlockReplacesBody(t *testing.T) {
	got := UpsertBlock(UpsertBlock("", "old"), "new")

	if strings.Contains(got, "old") || !strings.Contains(got, "new") || strings.Count(got, BlockEnd) != 1 {
		t.Fatalf("got:\n%s", got)
	}
}

func TestBlockBodies(t *testing.T) {
	if !strings.Contains(ZshenvBody(), `$HOME/.local/bin`) || !strings.Contains(ZshenvBody(), `$HOME/.claude-accounts/bin`) {
		t.Fatalf("zshenv body: %s", ZshenvBody())
	}
	if ZshrcBody() != `eval "$(ccm shell-init zsh --worker)"` {
		t.Fatalf("zshrc body: %s", ZshrcBody())
	}
}

func TestWorkersRoundTrip(t *testing.T) {
	p := paths.Resolve(func(string) string { return "" }, t.TempDir())
	at := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	ws := With(nil, Worker{Name: "worker1", SSHHost: "worker1", Accounts: []string{"inbox2"}, AddedAt: at})
	ws = With(ws, Worker{Name: "worker1", SSHHost: "worker1", Accounts: []string{"inbox2", "inbox3"}, AddedAt: at})

	if err := Save(p, ws); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)

	if err != nil || len(got) != 1 || len(got[0].Accounts) != 2 {
		t.Fatalf("got %+v, %v", got, err)
	}
	info, err := os.Stat(filepath.Join(p.AccountsRoot, "workers.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, %v", info.Mode().Perm(), err)
	}
}

func TestLoadMissingIsEmpty(t *testing.T) {
	p := paths.Resolve(func(string) string { return "" }, t.TempDir())
	if ws, err := Load(p); err != nil || len(ws) != 0 {
		t.Fatalf("got %v, %v", ws, err)
	}
}

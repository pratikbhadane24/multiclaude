package mcp

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"multiclaude/internal/paths"
)

func setupSync(t *testing.T) (paths.Paths, string) {
	t.Helper()
	home := t.TempDir()
	p := paths.Paths{
		Home:         home,
		ClaudeDir:    filepath.Join(home, ".claude"),
		ClaudeJSON:   filepath.Join(home, ".claude.json"),
		AccountsRoot: filepath.Join(home, ".claude-accounts"),
	}
	acc := filepath.Join(p.AccountsRoot, "work")
	if err := os.MkdirAll(acc, 0o700); err != nil {
		t.Fatal(err)
	}
	return p, acc
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readTop(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("invalid json in %s: %v\n%s", path, err, b)
	}
	return m
}

func serverNames(t *testing.T, path string) []string {
	t.Helper()
	var s map[string]json.RawMessage
	if err := json.Unmarshal(readTop(t, path)["mcpServers"], &s); err != nil {
		t.Fatal(err)
	}
	return keysOf(s)
}

const mainJSON = `{
  "numStartups": 3,
  "mcpServers": {
    "a": {"command": "a"},
    "b": {"command": "b"}
  }
}`

const accountJSON = `{
  "hasCompletedOnboarding": true,
  "projects": {"/x": {"allowedTools": [ "Bash" ]}},
  "mcpServers": {"x": {"command": "x"}},
  "oauthAccount": {"emailAddress": "e@x"}
}`

func TestSyncPreservesOtherKeysByteForByte(t *testing.T) {
	p, acc := setupSync(t)
	writeFile(t, p.ClaudeJSON, mainJSON)
	accFile := filepath.Join(acc, ".claude.json")
	writeFile(t, accFile, accountJSON)
	before := readTop(t, accFile)

	changed, err := Sync(p, acc)

	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	after := readTop(t, accFile)
	for k, v := range before {
		if k == "mcpServers" {
			continue
		}
		if string(after[k]) != string(v) {
			t.Fatalf("key %s changed: %s -> %s", k, v, after[k])
		}
	}
	if got := serverNames(t, accFile); !reflect.DeepEqual(got, []string{"a", "b", "x"}) {
		t.Fatalf("servers = %v", got)
	}
	fi, err := os.Stat(accFile)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v err %v", fi.Mode().Perm(), err)
	}
}

func TestSyncWritesStateAndRemovesServerDroppedFromMain(t *testing.T) {
	p, acc := setupSync(t)
	writeFile(t, p.ClaudeJSON, mainJSON)
	accFile := filepath.Join(acc, ".claude.json")
	writeFile(t, accFile, accountJSON)
	if _, err := Sync(p, acc); err != nil {
		t.Fatal(err)
	}
	var st struct {
		MCPSynced map[string][]string `json:"mcpSynced"`
	}
	if err := json.Unmarshal(mustRead(t, filepath.Join(p.AccountsRoot, "state.json")), &st); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(st.MCPSynced[acc], []string{"a", "b"}) {
		t.Fatalf("state = %v", st.MCPSynced)
	}

	writeFile(t, p.ClaudeJSON, `{"mcpServers": {"a": {"command": "a"}}}`)
	if _, err := Sync(p, acc); err != nil {
		t.Fatal(err)
	}
	if got := serverNames(t, accFile); !reflect.DeepEqual(got, []string{"a", "x"}) {
		t.Fatalf("servers = %v", got)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSyncNoChangeLeavesFileUntouched(t *testing.T) {
	p, acc := setupSync(t)
	writeFile(t, p.ClaudeJSON, mainJSON)
	accFile := filepath.Join(acc, ".claude.json")
	writeFile(t, accFile, accountJSON)
	if _, err := Sync(p, acc); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(accFile, old, old); err != nil {
		t.Fatal(err)
	}

	changed, err := Sync(p, acc)

	if err != nil || changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	fi, _ := os.Stat(accFile)
	if !fi.ModTime().Equal(old) {
		t.Fatalf("mtime changed: %v != %v", fi.ModTime(), old)
	}
}

func TestSyncMainWithoutMCPServersIsEmptySet(t *testing.T) {
	p, acc := setupSync(t)
	writeFile(t, p.ClaudeJSON, `{"numStartups": 1}`)
	accFile := filepath.Join(acc, ".claude.json")
	writeFile(t, accFile, accountJSON)

	changed, err := Sync(p, acc)

	if err != nil || changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
}

func TestSyncSourceErrorsLeaveAccountUntouched(t *testing.T) {
	cases := map[string]string{"missing": "", "corrupt": "{not json", "bad servers": `{"mcpServers": [1]}`}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			p, acc := setupSync(t)
			if content != "" {
				writeFile(t, p.ClaudeJSON, content)
			}
			accFile := filepath.Join(acc, ".claude.json")
			writeFile(t, accFile, accountJSON)

			_, err := Sync(p, acc)

			if !errors.Is(err, ErrSource) {
				t.Fatalf("err = %v, want ErrSource", err)
			}
			if string(mustRead(t, accFile)) != accountJSON {
				t.Fatal("account file modified")
			}
		})
	}
}

func TestSyncCorruptAccountFileIsErrorAndUntouched(t *testing.T) {
	p, acc := setupSync(t)
	writeFile(t, p.ClaudeJSON, mainJSON)
	accFile := filepath.Join(acc, ".claude.json")
	writeFile(t, accFile, "{oops")

	_, err := Sync(p, acc)

	if err == nil || errors.Is(err, ErrSource) {
		t.Fatalf("err = %v", err)
	}
	if string(mustRead(t, accFile)) != "{oops" {
		t.Fatal("account file modified")
	}
}

func TestSyncCreatesMissingAccountFile(t *testing.T) {
	p, acc := setupSync(t)
	writeFile(t, p.ClaudeJSON, mainJSON)
	accFile := filepath.Join(acc, ".claude.json")

	changed, err := Sync(p, acc)

	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if got := serverNames(t, accFile); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("servers = %v", got)
	}
	fi, _ := os.Stat(accFile)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
}

func TestSyncConcurrentCallsLeaveValidFileAndNoTemps(t *testing.T) {
	p, acc := setupSync(t)
	writeFile(t, p.ClaudeJSON, mainJSON)
	accFile := filepath.Join(acc, ".claude.json")
	writeFile(t, accFile, accountJSON)
	const workers = 20
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Sync(p, acc); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("sync error: %v", err)
	}
	if got := serverNames(t, accFile); !reflect.DeepEqual(got, []string{"a", "b", "x"}) {
		t.Fatalf("servers = %v", got)
	}
	for _, dir := range []string{acc, p.AccountsRoot} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".tmp") {
				t.Fatalf("leftover temp file %s", e.Name())
			}
		}
	}
}

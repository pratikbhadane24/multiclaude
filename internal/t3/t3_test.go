package t3

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/paths"
	"github.com/pratikbhadane24/multiclaude/internal/registry"
)

const (
	shim        = "/h/.claude-accounts/bin/claude"
	realFile    = `{"projectSettingsFolded":true,"idleCompaction":"onReturn","providerInstances":{"codex":{"driver":"codex","enabled":false,"config":{}}}}`
	workDir     = "/h/.claude-accounts/work"
	mainAccount = "main"
)

var accts = []registry.Account{{Name: mainAccount}, {Name: "work", Dir: workDir}}

func compact(raw []byte) string {
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		return string(raw)
	}
	return b.String()
}

func instances(t *testing.T, b []byte) map[string]json.RawMessage {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		t.Fatalf("bad json: %v\n%s", err, b)
	}
	var inst map[string]json.RawMessage
	if err := json.Unmarshal(top["providerInstances"], &inst); err != nil {
		t.Fatal(err)
	}
	return inst
}

func TestMergeKeepsUnrelatedBytesAndAddsInstances(t *testing.T) {
	out, added, updated, err := Merge([]byte(realFile), accts, shim)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(added, []string{"claude-main", "claude-work"}) || len(updated) != 0 {
		t.Fatalf("added=%v updated=%v", added, updated)
	}
	inst := instances(t, out)
	if compact(inst["codex"]) != `{"driver":"codex","enabled":false,"config":{}}` {
		t.Fatalf("codex changed: %s", inst["codex"])
	}
	var w struct {
		Driver  string
		Enabled bool
		Config  map[string]string
	}
	if err := json.Unmarshal(inst["claude-work"], &w); err != nil {
		t.Fatal(err)
	}
	if w.Driver != "claudeAgent" || !w.Enabled || w.Config["binaryPath"] != shim || w.Config["homePath"] != workDir {
		t.Fatalf("bad work instance: %s", inst["claude-work"])
	}
	var m struct{ Config map[string]string }
	_ = json.Unmarshal(inst["claude-main"], &m)
	if v, ok := m.Config["homePath"]; !ok || v != "" {
		t.Fatalf("main homePath: %s", inst["claude-main"])
	}
	var top map[string]json.RawMessage
	_ = json.Unmarshal(out, &top)
	if string(top["idleCompaction"]) != `"onReturn"` || string(top["projectSettingsFolded"]) != "true" {
		t.Fatalf("unrelated keys changed: %s", out)
	}
	if out[len(out)-1] != '\n' {
		t.Fatal("missing trailing newline")
	}
}

func TestMergeUpdatesMatchingInstanceInPlace(t *testing.T) {
	in := `{"providerInstances":{"mine":{"driver":"claudeAgent","config":{"homePath":"/h/.claude-accounts/work/","customModels":["x"],"launchArgs":"--foo"}}}}`
	out, added, updated, err := Merge([]byte(in), []registry.Account{accts[1]}, shim)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 0 || !reflect.DeepEqual(updated, []string{"mine"}) {
		t.Fatalf("added=%v updated=%v", added, updated)
	}
	var got struct {
		Enabled bool
		Config  map[string]json.RawMessage
	}
	if err := json.Unmarshal(instances(t, out)["mine"], &got); err != nil {
		t.Fatal(err)
	}
	if string(got.Config["binaryPath"]) != `"`+shim+`"` || compact(got.Config["customModels"]) != `["x"]` || string(got.Config["launchArgs"]) != `"--foo"` {
		t.Fatalf("config: %v", got.Config)
	}
	if !got.Enabled {
		t.Fatal("enabled should be set when missing")
	}
}

func TestMergeRespectsDisabledAndMatchesMainByEmptyHome(t *testing.T) {
	in := `{"providerInstances":{"m":{"driver":"claudeAgent","enabled":false,"config":{}}}}`
	out, added, updated, err := Merge([]byte(in), []registry.Account{accts[0]}, shim)
	if err != nil || len(added) != 0 || !reflect.DeepEqual(updated, []string{"m"}) {
		t.Fatalf("err=%v added=%v updated=%v", err, added, updated)
	}
	var got struct{ Enabled bool }
	_ = json.Unmarshal(instances(t, out)["m"], &got)
	if got.Enabled {
		t.Fatal("user enabled:false must be kept")
	}
}

func TestMergeIsIdempotent(t *testing.T) {
	first, _, _, _ := Merge([]byte(realFile), accts, shim)
	second, added, updated, err := Merge(first, accts, shim)
	if err != nil || len(added) != 0 || len(updated) != 0 {
		t.Fatalf("err=%v added=%v updated=%v", err, added, updated)
	}
	if string(second) != string(first) {
		t.Fatal("output changed on second run")
	}
}

func TestMergeEmptyInput(t *testing.T) {
	for _, in := range []string{"", "  ", "{}"} {
		out, added, _, err := Merge([]byte(in), accts[:1], shim)
		if err != nil || len(added) != 1 {
			t.Fatalf("%q: err=%v added=%v", in, err, added)
		}
		if _, ok := instances(t, out)["claude-main"]; !ok {
			t.Fatalf("%q: missing instance", in)
		}
	}
}

func TestMergeInvalidJSON(t *testing.T) {
	if _, _, _, err := Merge([]byte("{nope"), accts, shim); err == nil {
		t.Fatal("expected error")
	}
}

func testPaths(t *testing.T) paths.Paths {
	return paths.Paths{ShimPath: shim, T3Settings: filepath.Join(t.TempDir(), "userdata", "settings.json")}
}

func TestSyncMissingFileCreatesWithoutBackup(t *testing.T) {
	p := testPaths(t)
	added, _, backup, err := Sync(p, accts, time.Unix(100, 0))
	if err != nil || len(added) != 2 || backup != "" {
		t.Fatalf("err=%v added=%v backup=%q", err, added, backup)
	}
	if _, err := os.Stat(p.T3Settings); err != nil {
		t.Fatal(err)
	}
}

func TestSyncBacksUpOriginalAndKeepsMode(t *testing.T) {
	p := testPaths(t)
	_ = os.MkdirAll(filepath.Dir(p.T3Settings), 0o755)
	_ = os.WriteFile(p.T3Settings, []byte(realFile), 0o640)
	_, _, backup, err := Sync(p, accts, time.Unix(1700000000, 0))
	if err != nil {
		t.Fatal(err)
	}
	want := p.T3Settings + ".ccm-backup-1700000000"
	if backup != want {
		t.Fatalf("backup=%q want %q", backup, want)
	}
	if b, _ := os.ReadFile(backup); string(b) != realFile {
		t.Fatalf("backup content: %s", b)
	}
	if fi, _ := os.Stat(p.T3Settings); fi.Mode().Perm() != 0o640 {
		t.Fatalf("mode %v", fi.Mode())
	}
}

func TestSyncNoChangeWritesNothing(t *testing.T) {
	p := testPaths(t)
	if _, _, _, err := Sync(p, accts, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(p.T3Settings)
	added, updated, backup, err := Sync(p, accts, time.Unix(2, 0))
	if err != nil || len(added)+len(updated) != 0 || backup != "" {
		t.Fatalf("err=%v %v %v %q", err, added, updated, backup)
	}
	after, _ := os.ReadFile(p.T3Settings)
	if string(before) != string(after) {
		t.Fatal("file changed")
	}
	entries, _ := os.ReadDir(filepath.Dir(p.T3Settings))
	if len(entries) != 1 {
		t.Fatalf("stray files: %v", entries)
	}
}

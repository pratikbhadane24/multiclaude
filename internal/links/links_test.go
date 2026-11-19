package links

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"multiclaude/internal/paths"
)

func setup(t *testing.T) (paths.Paths, string) {
	t.Helper()
	root := t.TempDir()
	p := paths.Paths{Home: root, ClaudeDir: filepath.Join(root, ".claude")}
	acc := filepath.Join(root, "acc")
	for _, d := range []string{p.ClaudeDir, acc} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return p, acc
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func stateOf(t *testing.T, p paths.Paths, acc, name string) State {
	t.Helper()
	items, err := Inspect(p, acc)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Name == name {
			return it.State
		}
	}
	t.Fatalf("item %s not found", name)
	return ""
}

func TestLinkMissingLinksOnlyExistingSourcesAndIsIdempotent(t *testing.T) {
	p, acc := setup(t)
	write(t, filepath.Join(p.ClaudeDir, "CLAUDE.md"), "x")
	write(t, filepath.Join(p.ClaudeDir, "rules", "a.md"), "r")

	got, err := LinkMissing(p, acc)
	if err != nil || len(got) != 2 {
		t.Fatalf("got %v err %v", got, err)
	}
	dest, err := os.Readlink(filepath.Join(acc, "CLAUDE.md"))
	if err != nil || dest != filepath.Join(p.ClaudeDir, "CLAUDE.md") {
		t.Fatalf("readlink %q %v", dest, err)
	}
	if _, err := os.Lstat(filepath.Join(acc, "skills")); !os.IsNotExist(err) {
		t.Fatal("skills should not exist")
	}
	if stateOf(t, p, acc, "skills") != StateNoSource {
		t.Fatal("skills want no-source")
	}
	again, err := LinkMissing(p, acc)
	if err != nil || len(again) != 0 {
		t.Fatalf("second call %v %v", again, err)
	}
}

func TestInspectStates(t *testing.T) {
	p, acc := setup(t)
	write(t, filepath.Join(p.ClaudeDir, "CLAUDE.md"), "x")
	write(t, filepath.Join(p.ClaudeDir, "settings.json"), "{}")
	write(t, filepath.Join(p.ClaudeDir, "keybindings.json"), "{}")
	write(t, filepath.Join(acc, "settings.json"), "mine")
	if err := os.Symlink(filepath.Join(p.Home, "elsewhere"), filepath.Join(acc, "keybindings.json")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(acc, "hooks"), "orphan") // no source

	want := map[string]State{
		"CLAUDE.md": StateMissing, "settings.json": StateReplaced,
		"keybindings.json": StateWrongTarget, "hooks": StateNoSource,
	}
	for name, s := range want {
		if got := stateOf(t, p, acc, name); got != s {
			t.Errorf("%s: got %s want %s", name, got, s)
		}
	}
	if _, err := LinkMissing(p, acc); err != nil {
		t.Fatal(err)
	}
	if stateOf(t, p, acc, "CLAUDE.md") != StateOK {
		t.Fatal("want ok")
	}
}

func TestFixReplacedIdenticalFileNoBackup(t *testing.T) {
	p, acc := setup(t)
	write(t, filepath.Join(p.ClaudeDir, "CLAUDE.md"), "same")
	write(t, filepath.Join(acc, "CLAUDE.md"), "same")

	res, err := FixReplaced(p, acc, "CLAUDE.md", time.Unix(100, 0))
	if err != nil || res.Action != ActionRelinkedIdentical || res.BackupPath != "" {
		t.Fatalf("%+v %v", res, err)
	}
	if stateOf(t, p, acc, "CLAUDE.md") != StateOK {
		t.Fatal("want ok")
	}
	entries, _ := os.ReadDir(acc)
	if len(entries) != 1 {
		t.Fatalf("unexpected entries %v", entries)
	}
}

func TestFixReplacedDifferingFileBacksUpAndLeavesSource(t *testing.T) {
	p, acc := setup(t)
	src := filepath.Join(p.ClaudeDir, "settings.json")
	write(t, src, "source")
	old := time.Unix(1_000_000, 0)
	if err := os.Chtimes(src, old, old); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(acc, "settings.json"), "mine")

	res, err := FixReplaced(p, acc, "settings.json", time.Unix(1234, 0))
	if err != nil || res.Action != ActionRelinkedWithBackup {
		t.Fatalf("%+v %v", res, err)
	}
	if res.BackupPath != filepath.Join(acc, "settings.json.ccm-backup-1234") {
		t.Fatalf("backup %q", res.BackupPath)
	}
	if b, _ := os.ReadFile(res.BackupPath); string(b) != "mine" {
		t.Fatalf("backup content %q", b)
	}
	if stateOf(t, p, acc, "settings.json") != StateOK {
		t.Fatal("want ok")
	}
	b, _ := os.ReadFile(src)
	fi, _ := os.Stat(src)
	if string(b) != "source" || !fi.ModTime().Equal(old) {
		t.Fatal("source changed")
	}
}

func TestFixReplacedDifferingDirRenamesPreservingInode(t *testing.T) {
	p, acc := setup(t)
	write(t, filepath.Join(p.ClaudeDir, "rules", "a.md"), "s")
	write(t, filepath.Join(acc, "rules", "a.md"), "mine")
	before, _ := os.Stat(filepath.Join(acc, "rules"))

	res, err := FixReplaced(p, acc, "rules", time.Unix(5, 0))
	if err != nil || res.Action != ActionRelinkedWithBackup {
		t.Fatalf("%+v %v", res, err)
	}
	after, err := os.Stat(res.BackupPath)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("inode not preserved: %v", err)
	}
	if stateOf(t, p, acc, "rules") != StateOK {
		t.Fatal("want ok")
	}
}

func TestFixReplacedIdenticalDirNoBackup(t *testing.T) {
	p, acc := setup(t)
	write(t, filepath.Join(p.ClaudeDir, "rules", "sub", "a.md"), "s")
	write(t, filepath.Join(acc, "rules", "sub", "a.md"), "s")
	res, err := FixReplaced(p, acc, "rules", time.Unix(5, 0))
	if err != nil || res.Action != ActionRelinkedIdentical {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestFixReplacedNeverOverwritesExistingBackup(t *testing.T) {
	p, acc := setup(t)
	write(t, filepath.Join(p.ClaudeDir, "settings.json"), "source")
	write(t, filepath.Join(acc, "settings.json"), "mine")
	write(t, filepath.Join(acc, "settings.json.ccm-backup-7"), "old1")
	write(t, filepath.Join(acc, "settings.json.ccm-backup-7-1"), "old2")

	res, err := FixReplaced(p, acc, "settings.json", time.Unix(7, 0))
	if err != nil {
		t.Fatal(err)
	}
	if res.BackupPath != filepath.Join(acc, "settings.json.ccm-backup-7-2") {
		t.Fatalf("backup %q", res.BackupPath)
	}
	for name, want := range map[string]string{
		"settings.json.ccm-backup-7": "old1", "settings.json.ccm-backup-7-1": "old2",
		"settings.json.ccm-backup-7-2": "mine",
	} {
		if b, _ := os.ReadFile(filepath.Join(acc, name)); string(b) != want {
			t.Errorf("%s = %q", name, b)
		}
	}
}

func TestFixReplacedRejectsNonReplacedItems(t *testing.T) {
	p, acc := setup(t)
	write(t, filepath.Join(p.ClaudeDir, "CLAUDE.md"), "x")
	write(t, filepath.Join(p.ClaudeDir, "keybindings.json"), "x")
	if err := os.Symlink("/nowhere", filepath.Join(acc, "keybindings.json")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(acc, "hooks"), "keep")
	for _, name := range []string{"CLAUDE.md", "keybindings.json", "hooks", "bogus"} {
		if _, err := FixReplaced(p, acc, name, time.Unix(1, 0)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(acc, "hooks")); string(b) != "keep" {
		t.Fatal("hooks changed")
	}
	if d, _ := os.Readlink(filepath.Join(acc, "keybindings.json")); d != "/nowhere" {
		t.Fatal("symlink changed")
	}
}

package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"multiclaude/internal/paths"
)

func testPaths(t *testing.T) paths.Paths {
	t.Helper()
	return paths.Paths{AccountsRoot: filepath.Join(t.TempDir(), "accts")}
}

func TestLoadMissingIsEmpty(t *testing.T) {
	c, err := Load(testPaths(t))
	if err != nil || len(c.DefaultArgs) != 0 || len(c.Accounts) != 0 {
		t.Fatalf("c=%+v err=%v", c, err)
	}
}

func TestSaveLoadRoundTripMode0600(t *testing.T) {
	p := testPaths(t)
	want := Config{DefaultArgs: []string{"--dangerously-skip-permissions"}, Accounts: map[string]AccountConfig{"work": {Args: []string{"--model", "opus"}}}}

	if err := Save(p, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)

	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	fi, _ := os.Stat(filepath.Join(p.AccountsRoot, "config.json"))
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%v", fi.Mode())
	}
	raw, _ := os.ReadFile(filepath.Join(p.AccountsRoot, "config.json"))
	if !strings.Contains(string(raw), `"defaultArgs"`) || !strings.Contains(string(raw), `"accounts"`) {
		t.Fatalf("raw=%s", raw)
	}
}

func TestLoadCorruptErrors(t *testing.T) {
	p := testPaths(t)
	if err := os.MkdirAll(p.AccountsRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.AccountsRoot, "config.json"), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("want error")
	}
}

func TestMergedArgsOrderAndNoAliasing(t *testing.T) {
	defaults := make([]string, 1, 8)
	defaults[0] = "--d"
	c := Config{DefaultArgs: defaults, Accounts: map[string]AccountConfig{"w": {Args: []string{"--a"}}}}

	got := MergedArgs(c, "w", []string{"--t"})
	got[0] = "mutated"

	if c.DefaultArgs[0] != "--d" {
		t.Fatal("input mutated")
	}
	if want := []string{"--d", "--a", "--t"}; !reflect.DeepEqual(MergedArgs(c, "w", []string{"--t"}), want) {
		t.Fatalf("got %q", MergedArgs(c, "w", []string{"--t"}))
	}
	if got := MergedArgs(c, "other", nil); !reflect.DeepEqual(got, []string{"--d"}) {
		t.Fatalf("got %q", got)
	}
	if got := MergedArgs(Config{}, "x", nil); len(got) != 0 {
		t.Fatalf("got %q", got)
	}
}

func TestShellJoinQuoting(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"--dangerously-skip-permissions"}, "--dangerously-skip-permissions"},
		{[]string{"--model", "claude opus"}, "--model 'claude opus'"},
		{[]string{"it's"}, `'it'\''s'`},
		{[]string{""}, "''"},
		{[]string{"a$b", "c;d"}, "'a$b' 'c;d'"},
	}
	for _, tc := range cases {
		if got := ShellJoin(tc.in); got != tc.want {
			t.Errorf("ShellJoin(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

// splitPOSIX is a minimal splitter (single quotes and backslash) used to check
// that ShellJoin output round-trips.
func splitPOSIX(s string) []string {
	var out []string
	var cur strings.Builder
	in, started := false, false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case in && ch == '\'':
			in = false
		case in:
			cur.WriteByte(ch)
		case ch == '\'':
			in, started = true, true
		case ch == '\\' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
			started = true
		case ch == ' ':
			if started {
				out = append(out, cur.String())
				cur.Reset()
				started = false
			}
		default:
			cur.WriteByte(ch)
			started = true
		}
	}
	if started {
		out = append(out, cur.String())
	}
	return out
}

func TestSaveFailsWhenRootIsAFile(t *testing.T) {
	root := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(root, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Save(paths.Paths{AccountsRoot: root}, Config{}); err == nil {
		t.Fatal("want error")
	}
}

func TestLoadFailsWhenConfigIsADirectory(t *testing.T) {
	p := testPaths(t)
	if err := os.MkdirAll(filepath.Join(p.AccountsRoot, "config.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("want error")
	}
}

func TestShellJoinRoundTrip(t *testing.T) {
	args := []string{"--append-system-prompt", "it's a \"test\" $HOME", "", "--x=a b", "ünï"}
	if got := splitPOSIX(ShellJoin(args)); !reflect.DeepEqual(got, args) {
		t.Fatalf("got %q", got)
	}
}

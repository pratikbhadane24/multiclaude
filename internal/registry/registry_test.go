package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"multiclaude/internal/paths"
)

func TestValidateName(t *testing.T) {
	if err := ValidateName("work"); err != nil {
		t.Fatalf("work: %v", err)
	}
	for _, n := range []string{"Work", "a b", "", strings.Repeat("a", 33), "status"} {
		if err := ValidateName(n); err == nil {
			t.Errorf("%q should fail", n)
		}
	}
	if err := ValidateName("status"); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Errorf("want reserved error, got %v", err)
	}
}

func TestNormalizeDir(t *testing.T) {
	if got := NormalizeDir("~/.claude-accounts/work/", "/h"); got != "/h/.claude-accounts/work" {
		t.Errorf("got %q", got)
	}
	if got := NormalizeDir("x", "/h"); got != "/h/x" {
		t.Errorf("got %q", got)
	}
	if got := NormalizeDir("/h/é", "/h"); got != "/h/é" {
		t.Errorf("got %q", got)
	}
}

func testPaths(t *testing.T) paths.Paths {
	t.Helper()
	root := filepath.Join(t.TempDir(), "accts")
	return paths.Paths{Home: "/h", AccountsRoot: root}
}

func TestLoadMissingFile(t *testing.T) {
	got, err := Load(testPaths(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "main" || !got[0].IsMain() {
		t.Fatalf("got %+v", got)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	p := testPaths(t)
	work := Account{Name: "work", Dir: "/h/w", Email: "a@b.c"}
	if err := Save(p, []Account{{Name: "main"}, work}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(p.AccountsRoot, "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"main"`) {
		t.Errorf("main persisted: %s", raw)
	}
	st, _ := os.Stat(filepath.Join(p.AccountsRoot, "accounts.json"))
	if st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode().Perm())
	}
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "main" || got[1] != work {
		t.Fatalf("got %+v", got)
	}
}

func TestLoadCorruptMentionsPath(t *testing.T) {
	p := testPaths(t)
	_ = os.MkdirAll(p.AccountsRoot, 0o700)
	f := filepath.Join(p.AccountsRoot, "accounts.json")
	_ = os.WriteFile(f, []byte("{nope"), 0o600)
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), f) {
		t.Fatalf("got %v", err)
	}
}

func TestWithWithoutFind(t *testing.T) {
	in := []Account{{Name: "main"}, {Name: "a", Dir: "/1"}}
	out := With(in, Account{Name: "a", Dir: "/2"})
	if in[1].Dir != "/1" || out[1].Dir != "/2" || len(out) != 2 {
		t.Fatalf("in=%+v out=%+v", in, out)
	}
	out = With(in, Account{Name: "b", Dir: "/3"})
	if len(out) != 3 || len(in) != 2 {
		t.Fatal("append failed")
	}
	if w := Without(out, "a"); len(w) != 2 || len(out) != 3 {
		t.Fatalf("without %+v", w)
	}
	if a, ok := Find(out, "b"); !ok || a.Dir != "/3" {
		t.Fatal("find")
	}
	if _, ok := Find(out, "zz"); ok {
		t.Fatal("find zz")
	}
}

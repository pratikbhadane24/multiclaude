package install

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pratikbhadane24/multiclaude/internal/paths"
)

type env struct {
	p        paths.Paths
	exe      string
	localBin string
	shimSrc  string
}

func setup(t *testing.T) env {
	t.Helper()
	root := t.TempDir()
	bin := filepath.Join(root, "build")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(bin, "ccm")
	shim := filepath.Join(bin, "ccm-shim")
	if err := os.WriteFile(exe, []byte("ccm-bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shim, []byte("shim-v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(root, "localbin")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatal(err)
	}
	return env{
		p:        paths.Paths{ShimPath: filepath.Join(root, "ccm-home", "bin", "claude")},
		exe:      real,
		localBin: local,
		shimSrc:  filepath.Join(filepath.Dir(real), "ccm-shim"),
	}
}

func TestFreshInstall(t *testing.T) {
	e := setup(t)
	res, err := Install(e.p, e.exe, e.localBin)
	if err != nil {
		t.Fatal(err)
	}
	if !res.ShimUpdated || !res.LinkCreated {
		t.Fatalf("res = %+v", res)
	}
	st, err := os.Stat(e.p.ShimPath)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v", st.Mode())
	}
	got, _ := os.ReadFile(e.p.ShimPath)
	if !bytes.Equal(got, []byte("shim-v1")) {
		t.Fatalf("bytes %q", got)
	}
	target, err := os.Readlink(filepath.Join(e.localBin, "ccm"))
	if err != nil || target != e.exe {
		t.Fatalf("link %q %v", target, err)
	}
	if res.ShimPath != e.p.ShimPath || res.CCMLink != filepath.Join(e.localBin, "ccm") {
		t.Fatalf("res paths %+v", res)
	}
}

func TestRerunNoop(t *testing.T) {
	e := setup(t)
	if _, err := Install(e.p, e.exe, e.localBin); err != nil {
		t.Fatal(err)
	}
	res, err := Install(e.p, e.exe, e.localBin)
	if err != nil {
		t.Fatal(err)
	}
	if res.ShimUpdated || res.LinkCreated {
		t.Fatalf("res = %+v", res)
	}
}

func TestChangedShimUpdated(t *testing.T) {
	e := setup(t)
	if _, err := Install(e.p, e.exe, e.localBin); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.shimSrc, []byte("shim-v2"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := Install(e.p, e.exe, e.localBin)
	if err != nil {
		t.Fatal(err)
	}
	if !res.ShimUpdated {
		t.Fatalf("res = %+v", res)
	}
	got, _ := os.ReadFile(e.p.ShimPath)
	if string(got) != "shim-v2" {
		t.Fatalf("bytes %q", got)
	}
}

func TestForeignFileNotOverwritten(t *testing.T) {
	e := setup(t)
	link := filepath.Join(e.localBin, "ccm")
	if err := os.WriteFile(link, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Install(e.p, e.exe, e.localBin)
	if err == nil || !strings.Contains(err.Error(), link+" exists; remove it or install manually") {
		t.Fatalf("err = %v", err)
	}
	got, _ := os.ReadFile(link)
	if string(got) != "mine" {
		t.Fatalf("file modified: %q", got)
	}
}

func TestForeignSymlinkNotOverwritten(t *testing.T) {
	e := setup(t)
	link := filepath.Join(e.localBin, "ccm")
	if err := os.Symlink("/nonexistent/other", link); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(e.p, e.exe, e.localBin); err == nil {
		t.Fatal("expected error")
	}
	if tgt, _ := os.Readlink(link); tgt != "/nonexistent/other" {
		t.Fatalf("link changed: %q", tgt)
	}
}

func TestMissingShim(t *testing.T) {
	e := setup(t)
	if err := os.Remove(e.shimSrc); err != nil {
		t.Fatal(err)
	}
	_, err := Install(e.p, e.exe, e.localBin)
	want := "ccm-shim not found next to " + filepath.Dir(e.exe) + "; reinstall ccm or run make build"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestSelfExeViaSymlinkResolves(t *testing.T) {
	e := setup(t)
	alias := filepath.Join(t.TempDir(), "alias-ccm")
	if err := os.Symlink(e.exe, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(e.p, alias, e.localBin); err != nil {
		t.Fatal(err)
	}
	target, _ := os.Readlink(filepath.Join(e.localBin, "ccm"))
	if target != e.exe {
		t.Fatalf("target %q want %q", target, e.exe)
	}
}

func TestNextSteps(t *testing.T) {
	want := "Installed.\nAdd this line to ~/.zshrc, then open a new terminal:\n  eval \"$(ccm shell-init zsh)\"\nNext: ccm add <name> --email <you@example.com>\n"
	if got := NextSteps("/x"); got != want {
		t.Fatalf("got %q", got)
	}
}

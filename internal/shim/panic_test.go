package shim

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/pratikbhadane24/multiclaude/internal/links"
	"github.com/pratikbhadane24/multiclaude/internal/paths"
)

func TestPrepareAccountPanicIsWarning(t *testing.T) {
	f := newFixture(t)
	d := f.deps()
	d.Inspect = func(paths.Paths, string) ([]links.Item, error) { panic("kaboom") }
	prepareAccount(f.p, filepath.Join(f.p.AccountsRoot, "work"), d)
	if !strings.Contains(f.stderr.String(), "ccm: warning: internal error during sync: kaboom") {
		t.Fatalf("stderr %q", f.stderr.String())
	}
}

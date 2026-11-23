package sdkproxy

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pratikbhadane24/multiclaude/internal/config"
	"github.com/pratikbhadane24/multiclaude/internal/paths"
)

func TestSupervisorDeclinesWithoutStreamJSONOrAutoSwitch(t *testing.T) {
	root := t.TempDir()
	p := paths.Paths{Home: root, AccountsRoot: filepath.Join(root, "acc")}
	var stderr bytes.Buffer
	sup := Supervisor(p, &stderr)
	if _, ok := sup([]string{"-p", "hi"}, nil, nil); ok {
		t.Fatal("plain run supervised")
	}
	if _, ok := sup(streamArgs, nil, nil); ok {
		t.Fatal("auto-switch off (no config) supervised")
	}
	if err := config.Save(p, config.Config{AutoSwitch: true}); err != nil {
		t.Fatal(err)
	}
	if _, ok := sup(streamArgs, []string{"CCM_NO_PROXY=1"}, nil); ok {
		t.Fatal("CCM_NO_PROXY ignored")
	}
	if err := os.WriteFile(filepath.Join(p.AccountsRoot, "config.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := sup(streamArgs, nil, nil); ok || !strings.Contains(stderr.String(), "auto-switch off") {
		t.Fatalf("corrupt config: ok=%v stderr %q", ok, stderr.String())
	}
}

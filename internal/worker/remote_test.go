package worker

import (
	"context"
	"strings"
	"testing"

	"github.com/pratikbhadane24/multiclaude/internal/run"
)

func TestSSHWriteIsPrivateAndRefusesSymlinks(t *testing.T) {
	f := &run.Fake{}
	r := SSHRemote{Host: "w", R: f}

	if err := r.Write(context.Background(), ".claude-accounts/mcp-hub.json", []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	script := f.Calls[0].Args[len(f.Calls[0].Args)-1]
	if !strings.Contains(script, "umask 077") {
		t.Fatalf("no umask 077 before creating the temp file:\n%s", script)
	}
	if !strings.Contains(script, `[ -L "$f" ]`) {
		t.Fatalf("does not refuse symlinks:\n%s", script)
	}
	if !strings.Contains(script, "mktemp") {
		t.Fatalf("temp file is not created with mktemp:\n%s", script)
	}
}

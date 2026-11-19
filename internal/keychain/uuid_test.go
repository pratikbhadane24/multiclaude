package keychain

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"multiclaude/internal/registry"
	"multiclaude/internal/run"
)

func TestReadParsesAccountUUIDAndRedacts(t *testing.T) {
	f := fakeWith(run.Result{Stdout: `{"claudeAiOauth":{"accessToken":"tok","accountUuid":"uuid-123"}}`}, nil)
	c, err := Read(context.Background(), f, registry.Account{Name: "work", Dir: workDir})
	if err != nil || c == nil || c.AccountUUID != "uuid-123" {
		t.Fatalf("got %+v err %v", c, err)
	}
	for _, s := range []string{c.String(), fmt.Sprintf("%v %+v", c, c)} {
		if strings.Contains(s, "uuid-123") || strings.Contains(s, "tok") {
			t.Fatalf("leaked: %s", s)
		}
	}
}

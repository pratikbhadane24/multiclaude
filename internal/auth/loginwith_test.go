package auth

import (
	"context"
	"strings"
	"testing"

	"multiclaude/internal/browser"
	"multiclaude/internal/registry"
)

func TestLoginWithSetsBrowserEnv(t *testing.T) {
	f := statusFake("you@x.com")
	opts := LoginOptions{SelfExe: "/bin/ccm", Mode: browser.Mode{Kind: browser.KindIncognito}}
	if _, err := LoginWith(context.Background(), f, testPaths(t), registry.Account{Name: "work", Dir: "/d"}, "you@x.com", opts); err != nil {
		t.Fatal(err)
	}
	env := strings.Join(f.Calls[0].Env, "\n")
	for _, want := range []string{"BROWSER=/bin/ccm", "CCM_LOGIN_ACCOUNT=work", "CCM_LOGIN_EMAIL=you@x.com", "CCM_LOGIN_OPEN=incognito", "CLAUDE_CONFIG_DIR=/d"} {
		if !strings.Contains(env, want) {
			t.Errorf("missing %s in env", want)
		}
	}
}

func TestLoginWithoutSelfExeLeavesBrowserAlone(t *testing.T) {
	f := statusFake("a@x.com")
	if _, err := Login(context.Background(), f, testPaths(t), registry.Account{Name: "main"}, ""); err != nil {
		t.Fatal(err)
	}
	for _, e := range f.Calls[0].Env {
		if strings.HasPrefix(e, "CCM_LOGIN_") {
			t.Fatalf("unexpected %s", e)
		}
	}
}

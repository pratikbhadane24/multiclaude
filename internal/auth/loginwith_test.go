package auth

import (
	"context"
	"strings"
	"testing"

	"github.com/pratikbhadane24/multiclaude/internal/browser"
	"github.com/pratikbhadane24/multiclaude/internal/paths"
	"github.com/pratikbhadane24/multiclaude/internal/registry"
	"github.com/pratikbhadane24/multiclaude/internal/run"
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
	if _, err := loginPlain(context.Background(), f, testPaths(t), registry.Account{Name: "main"}, ""); err != nil {
		t.Fatal(err)
	}
	for _, e := range f.Calls[0].Env {
		if strings.HasPrefix(e, "CCM_LOGIN_") {
			t.Fatalf("unexpected %s", e)
		}
	}
}

// loginPlain runs LoginWith without any browser options.
func loginPlain(ctx context.Context, r run.Runner, p paths.Paths, a registry.Account, email string) (Status, error) {
	return LoginWith(ctx, r, p, a, email, LoginOptions{})
}

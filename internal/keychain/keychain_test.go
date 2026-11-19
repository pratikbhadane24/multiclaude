package keychain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"multiclaude/internal/registry"
	"multiclaude/internal/run"
)

const workDir = "/Users/test/.claude-accounts/work"
const workSvc = "Claude Code-credentials-d9263ab7"

func TestServiceMain(t *testing.T) {
	if got := Service(""); got != "Claude Code-credentials" {
		t.Fatalf("got %q", got)
	}
}

func TestServiceHashed(t *testing.T) {
	if got := Service(workDir); got != workSvc {
		t.Fatalf("got %q", got)
	}
}

func TestServiceStripsTrailingSlash(t *testing.T) {
	if got := Service(workDir + "/"); got != workSvc {
		t.Fatalf("got %q", got)
	}
}

func TestServiceNormalizesNFD(t *testing.T) {
	nfc := "/Users/test/.claude-accounts/café"
	nfd := "/Users/test/.claude-accounts/café"
	if Service(nfd) != Service(nfc) {
		t.Fatalf("NFD and NFC services differ")
	}
}

func fakeWith(res run.Result, err error) *run.Fake {
	return &run.Fake{Respond: func(run.Call) (run.Result, error) { return res, err }}
}

func TestReadCallsSecurity(t *testing.T) {
	f := fakeWith(run.Result{Code: 44}, nil)
	_, _ = Read(context.Background(), f, registry.Account{Name: "work", Dir: workDir})
	if len(f.Calls) != 1 {
		t.Fatalf("calls: %d", len(f.Calls))
	}
	c := f.Calls[0]
	want := []string{"find-generic-password", "-s", workSvc, "-w"}
	if c.Name != "/usr/bin/security" || fmt.Sprint(c.Args) != fmt.Sprint(want) {
		t.Fatalf("bad call %+v", c)
	}
}

func TestReadMainUsesBaseService(t *testing.T) {
	f := fakeWith(run.Result{Code: 44}, nil)
	_, _ = Read(context.Background(), f, registry.Account{Name: "main"})
	if f.Calls[0].Args[2] != "Claude Code-credentials" {
		t.Fatalf("args %v", f.Calls[0].Args)
	}
}

func TestReadNonZeroReturnsNil(t *testing.T) {
	c, err := Read(context.Background(), fakeWith(run.Result{Code: 44}, nil), registry.Account{Name: "w", Dir: workDir})
	if c != nil || err != nil {
		t.Fatalf("got %v %v", c, err)
	}
}

func TestReadParsesCreds(t *testing.T) {
	js := `{"claudeAiOauth":{"accessToken":"tok","expiresAt":1700000000000,"subscriptionType":"max","rateLimitTier":"t5"}}`
	c, err := Read(context.Background(), fakeWith(run.Result{Stdout: js + "\n"}, nil), registry.Account{Name: "w", Dir: workDir})
	if err != nil || c == nil {
		t.Fatalf("got %v %v", c, err)
	}
	if c.AccessToken != "tok" || c.ExpiresAt != 1700000000000 || c.SubscriptionType != "max" || c.RateLimitTier != "t5" {
		t.Fatalf("bad creds %+v", *c)
	}
}

func TestReadBadJSONHidesSecret(t *testing.T) {
	_, err := Read(context.Background(), fakeWith(run.Result{Stdout: "SECRET not json"}, nil), registry.Account{Name: "w", Dir: workDir})
	if !errors.Is(err, ErrCredsParse) {
		t.Fatalf("err %v", err)
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("leak: %v", err)
	}
}

func TestReadRunnerError(t *testing.T) {
	_, err := Read(context.Background(), fakeWith(run.Result{}, errors.New("boom")), registry.Account{Name: "w", Dir: workDir})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err %v", err)
	}
}

func TestCredsStringRedacted(t *testing.T) {
	c := Creds{AccessToken: "tok-secret"}
	for _, s := range []string{fmt.Sprint(c), fmt.Sprint(&c), fmt.Sprintf("%v %+v", c, c)} {
		if strings.Contains(s, "tok-secret") {
			t.Fatalf("leak %q", s)
		}
	}
	if c.String() != "Creds{redacted}" {
		t.Fatal(c.String())
	}
}

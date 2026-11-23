package keychain

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/pratikbhadane24/multiclaude/internal/registry"
	"github.com/pratikbhadane24/multiclaude/internal/run"
)

func TestPresentCodes(t *testing.T) {
	a := registry.Account{Name: "work", Dir: workDir}
	for code, want := range map[int]bool{0: true, 44: false} {
		f := fakeWith(run.Result{Code: code}, nil)
		got, err := Present(context.Background(), f, a)
		if err != nil || got != want {
			t.Fatalf("code %d: %v %v", code, got, err)
		}
		wantArgs := []string{"find-generic-password", "-s", workSvc}
		if f.Calls[0].Name != "/usr/bin/security" || fmt.Sprint(f.Calls[0].Args) != fmt.Sprint(wantArgs) {
			t.Fatalf("argv %+v (must not contain -w)", f.Calls[0])
		}
	}
	// Any other code (e.g. 36: Keychain locked over SSH) falls back to the
	// credentials file, which this account does not have.
	if ok, err := Present(context.Background(), fakeWith(run.Result{Code: 1}, nil), a); err != nil || ok {
		t.Fatalf("other exit code: %v %v", ok, err)
	}
	if _, err := Present(context.Background(), fakeWith(run.Result{}, errors.New("boom")), a); err == nil {
		t.Fatal("want runner error")
	}
}

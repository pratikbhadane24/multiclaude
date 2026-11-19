package cli

import (
	"context"
	"sync/atomic"
	"testing"

	"multiclaude/internal/registry"
)

func TestRefreshCacheSkipsEmailLookupsAndTodayScan(t *testing.T) {
	e := sameLoginEnv(t)

	code := e.main("status", "--refresh-cache")

	if code != ExitOK {
		t.Fatalf("code=%d err=%s", code, e.err)
	}
	for _, c := range e.callsTo(e.p.RealClaude) {
		if len(c.Args) > 0 && c.Args[0] == "auth" {
			t.Fatalf("refresh-cache must not run `claude auth status`: %v", c.Args)
		}
	}
}

func TestCheapDepsDropTodayAndEmailFallback(t *testing.T) {
	e := newCLI(t, "")
	d := e.app.statusDeps(nil, e.app.cachedFetch(false))
	if d.Today == nil || d.EmailFallback == nil {
		t.Fatal("precondition: normal deps use both")
	}

	got := cheapDeps(d)

	if got.Today != nil || got.EmailFallback != nil {
		t.Fatalf("Today=%v EmailFallback=%v", got.Today != nil, got.EmailFallback != nil)
	}
	if d.Today == nil || d.EmailFallback == nil {
		t.Fatal("input deps were mutated")
	}
}

func TestMemoEmailLooksUpOncePerAccount(t *testing.T) {
	var calls atomic.Int32
	lookup := func(_ context.Context, a registry.Account) string {
		calls.Add(1)
		return a.Name + "@x.com"
	}
	memo := memoEmail(lookup)
	ctx := context.Background()

	first := memo(ctx, registry.Account{Name: "a"})
	again := memo(ctx, registry.Account{Name: "a"})
	other := memo(ctx, registry.Account{Name: "b"})

	if first != "a@x.com" || again != first || other != "b@x.com" || calls.Load() != 2 {
		t.Fatalf("first=%q again=%q other=%q calls=%d", first, again, other, calls.Load())
	}
}

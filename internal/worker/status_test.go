package worker

import (
	"context"
	"testing"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/run"
)

func TestCollectUnreachableDoesNotBlock(t *testing.T) {
	slow := newFakeRemote()
	slow.delay = 5 * time.Second
	fast := newFakeRemote()
	fast.respond["loadavg"] = run.Result{Stdout: "load=1.5\ncores=10\nfreemem=6.2\nprocs=2\nccm=ccm v0.2.0\naccounts=[{\"name\":\"inbox2\",\"loggedIn\":true}]\n"}
	remotes := map[string]Remote{"slow": slow, "fast": fast}
	ws := []Worker{{Name: "w1", SSHHost: "slow"}, {Name: "w2", SSHHost: "fast"}}
	start := time.Now()

	got := Collect(context.Background(), ws, func(h string) Remote { return remotes[h] }, 200*time.Millisecond)

	if time.Since(start) > 2*time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
	if got[0].Name != "w1" || got[0].Reachable || got[0].Err == "" {
		t.Fatalf("slow = %+v", got[0])
	}
	h := got[1]
	if !h.Reachable || h.Load1 != 1.5 || h.Cores != 10 || h.FreeMemGB != 6.2 || h.ClaudeProcs != 2 || h.CCMVersion != "ccm v0.2.0" || !h.Accounts["inbox2"] {
		t.Fatalf("fast = %+v", h)
	}
}

func TestParseHealthWrongLoginIsNotLoggedIn(t *testing.T) {
	out := "load=1\ncores=1\naccounts=[{\"name\":\"inbox3\",\"email\":\"i3@x\",\"loginEmail\":\"i2@x\",\"loggedIn\":true}]\n"

	h := parseHealth(Health{Name: "w"}, out)

	if h.Accounts["inbox3"] {
		t.Fatalf("inbox3 signed in as i2 counted as logged in: %+v", h.Accounts)
	}
}

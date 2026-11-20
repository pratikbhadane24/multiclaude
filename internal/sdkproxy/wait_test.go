package sdkproxy

import (
	"strings"
	"testing"
	"time"

	"multiclaude/internal/registry"
	"multiclaude/internal/status"
)

func limitedCache(name string, reset time.Time) status.Cache {
	full := 100.0
	return status.Cache{Accounts: []status.CacheAccount{{Name: name, FiveHourUsed: &full, FiveHourResetsAt: &reset}}}
}

func TestNoFreeAccountWaitsForResetThenSwitches(t *testing.T) {
	h := newHarness(t)
	h.accts = []registry.Account{{Name: "work", Dir: "/acc/work"}, {Name: "alt", Dir: "/acc/alt"}}
	altReset := t0.Add(150 * time.Second)
	h.setCache(limitedCache("alt", altReset))
	c := h.run(streamArgs, workEnv, nil)
	startTurn(t, h, c)
	c.say(t, rejected(t0.Add(5*time.Hour), "five_hour"))
	c.say(t, errResult(limitText))
	h.waitTimer(pollInterval)
	h.noChild()
	h.noOut()
	queued := `{"type":"control_request","request_id":"r9","request":{"subtype":"interrupt"}}`
	h.send(queued)
	h.clock.Advance(pollInterval) // t0+60s: alt frees at reset+grace = t0+210s
	h.waitTimer(pollInterval)
	h.noChild()
	h.clock.Advance(pollInterval) // t0+120s
	h.waitTimer(pollInterval)
	h.clock.Advance(pollInterval) // t0+180s
	h.waitTimer(30 * time.Second)
	h.clock.Advance(30 * time.Second) // t0+210s
	c2 := expectReplacement(t, h, "/acc/alt", "switched from work to alt")
	if got := c2.read(t); got != queued+"\n" {
		t.Fatalf("queued line %q", got)
	}
	if !strings.Contains(h.stderr.String(), "no account is free — waiting until") {
		t.Fatalf("stderr %q", h.stderr.String())
	}
}

func TestWaitOnSameAccountUsesResetPrompt(t *testing.T) {
	h := newHarness(t)
	h.accts = []registry.Account{{Name: "work", Dir: "/acc/work"}}
	c := h.run(streamArgs, workEnv, nil)
	startTurn(t, h, c)
	c.say(t, rejected(t0.Add(30*time.Second), "five_hour"))
	c.say(t, errResult(limitText))
	h.waitTimer(pollInterval) // free at reset + 60s grace = t0+90s
	h.clock.Advance(pollInterval)
	h.waitTimer(30 * time.Second)
	h.clock.Advance(30 * time.Second)
	expectReplacement(t, h, "/acc/work", "The limit has now reset")
}

func TestStdinClosedWhileWaitingExits(t *testing.T) {
	h := newHarness(t)
	h.accts = []registry.Account{{Name: "work", Dir: "/acc/work"}}
	c := h.run(streamArgs, workEnv, nil)
	startTurn(t, h, c)
	c.say(t, errResult(limitText))
	h.waitTimer(pollInterval)
	_ = h.sdkIn.Close()
	if code := h.exitCode(); code != exitAbandoned {
		t.Fatalf("exit %d", code)
	}
}

func TestSecondSwitchWithin30sIsDelayed(t *testing.T) {
	h := newHarness(t)
	c := h.run(streamArgs, workEnv, nil)
	startTurn(t, h, c)
	c.say(t, errResult(limitText))
	c2 := expectReplacement(t, h, "", "switched from work to main")
	c2.say(t, initResp)
	c2.say(t, errResult(limitText))
	h.waitTimer(minSwitchGap)
	h.noChild()
	h.clock.Advance(minSwitchGap)
	expectReplacement(t, h, "/acc/alt", "switched from main to alt")
}

func TestGivesUpAfterMaxSwitches(t *testing.T) {
	h := newHarness(t)
	h.accts = []registry.Account{{Name: "work", Dir: "/acc/work"}}
	c := h.run(streamArgs, workEnv, nil)
	startTurn(t, h, c)
	// The limit is already over, so the same account is free again at once.
	ev := rejected(t0.Add(-time.Hour), "five_hour")
	for i := 0; i < maxSwitches; i++ {
		c.say(t, ev)
		c.say(t, errResult(limitText))
		if i > 0 {
			h.waitTimer(minSwitchGap)
			h.clock.Advance(minSwitchGap)
		}
		c = h.child()
		c.read(t)
		c.read(t)
	}
	c.say(t, ev)
	c.say(t, errResult(limitText))
	if got := h.out(); got != ev+"\n" {
		t.Fatalf("sdk got %q", got)
	}
	if got := h.out(); !strings.Contains(got, "hit your") {
		t.Fatalf("sdk got %q", got)
	}
	h.noChild()
	if !strings.Contains(h.stderr.String(), "not switching again") {
		t.Fatalf("stderr %q", h.stderr.String())
	}
}

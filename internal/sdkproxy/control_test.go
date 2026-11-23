package sdkproxy

import (
	"strings"
	"testing"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/registry"
)

const (
	setModelReq  = `{"type":"control_request","request_id":"r2","request":{"subtype":"set_model","model":"opus"}}`
	setModelResp = `{"type":"control_response","response":{"subtype":"success","request_id":"r2","response":{}}}`
	mcpReq       = `{"type":"control_request","request_id":"r3","request":{"subtype":"mcp_status"}}`
	interruptReq = `{"type":"control_request","request_id":"r9","request":{"subtype":"interrupt"}}`
)

// sendControl sends a control request the child receives but never answers.
func sendControl(t *testing.T, h *harness, c *fakeChild, line string) {
	t.Helper()
	h.send(line)
	if got := c.read(t); got != line+"\n" {
		t.Fatalf("child got %q", got)
	}
}

func TestUnansweredStateRequestIsReplayedAndAnswered(t *testing.T) {
	h := newHarness(t)
	c := h.run(streamArgs, workEnv, nil)
	startTurn(t, h, c)
	sendControl(t, h, c, setModelReq)
	c.say(t, errResult(limitText))
	c2 := h.child()
	for _, want := range []string{initReq, setModelReq} {
		if got := c2.read(t); got != want+"\n" {
			t.Fatalf("replay got %q, want %q", got, want)
		}
	}
	if got := c2.read(t); !strings.Contains(got, "[ccm]") {
		t.Fatalf("resume prompt %q", got)
	}
	c2.say(t, initResp)
	c2.say(t, setModelResp)
	if got := h.out(); got != setModelResp+"\n" {
		t.Fatalf("sdk got %q (unanswered set_model reply swallowed)", got)
	}
}

func TestAnsweredStateRequestReplyStaysHidden(t *testing.T) {
	h := newHarness(t)
	c := h.run(streamArgs, workEnv, nil)
	startTurn(t, h, c)
	sendControl(t, h, c, setModelReq)
	c.say(t, setModelResp)
	h.out()
	c.say(t, errResult(limitText))
	c2 := h.child()
	c2.read(t)
	c2.read(t)
	c2.read(t)
	c2.say(t, initResp)
	c2.say(t, setModelResp)
	c2.say(t, assistOK)
	if got := h.out(); got != assistOK+"\n" {
		t.Fatalf("sdk got %q", got)
	}
}

func TestUnansweredOtherRequestIsResent(t *testing.T) {
	h := newHarness(t)
	c := h.run(streamArgs, workEnv, nil)
	startTurn(t, h, c)
	sendControl(t, h, c, mcpReq)
	c.say(t, errResult(limitText))
	c2 := expectReplacement(t, h, "", "switched from work to main")
	if got := c2.read(t); got != mcpReq+"\n" {
		t.Fatalf("unanswered request not resent, got %q", got)
	}
}

// waitOnOnlyAccount runs a turn on the single account "work" that hits its
// limit (resetting at t0+30s) and leaves the proxy waiting.
func waitOnOnlyAccount(t *testing.T, h *harness, limitLine string) *fakeChild {
	t.Helper()
	h.accts = []registry.Account{{Name: "work", Dir: "/acc/work"}}
	c := h.run(streamArgs, workEnv, nil)
	startTurn(t, h, c)
	c.say(t, rejected(t0.Add(30*time.Second), "five_hour"))
	c.say(t, limitLine)
	h.waitTimer(pollInterval)
	h.noOut()
	return c
}

func TestInterruptWhileWaitingEndsTurn(t *testing.T) {
	h := newHarness(t)
	waitOnOnlyAccount(t, h, errResult(limitText))
	h.send(interruptReq)
	if got := h.out(); !strings.Contains(got, `"control_response"`) || !strings.Contains(got, `"r9"`) || !strings.Contains(got, `"success"`) {
		t.Fatalf("interrupt answer %q", got)
	}
	if got := h.out(); !strings.Contains(got, "rate_limit_event") {
		t.Fatalf("sdk got %q", got)
	}
	if got := h.out(); got != errResult(limitText)+"\n" {
		t.Fatalf("sdk got %q", got)
	}
	h.clock.Advance(10 * time.Minute)
	h.noChild()
	h.send(userLine)
	c2 := h.child()
	if got := c2.read(t); got != initReq+"\n" {
		t.Fatalf("replay %q", got)
	}
	if got := c2.read(t); got != userLine+"\n" {
		t.Fatalf("want the new message, no resume prompt; got %q", got)
	}
}

func TestInterruptWhileWaitingSynthesizesResult(t *testing.T) {
	h := newHarness(t)
	waitOnOnlyAccount(t, h, assistErr("rate_limit", limitText))
	h.send(interruptReq)
	h.out() // control_response
	h.out() // rate_limit_event
	if got := h.out(); !strings.Contains(got, `"rate_limit"`) {
		t.Fatalf("sdk got %q", got)
	}
	got := h.out()
	m, ok := parse([]byte(got))
	if !ok || m.Type != typeResult || !m.IsError || m.SessionID != "S1" {
		t.Fatalf("want an error result, got %q", got)
	}
}

package sdkproxy

import (
	"io"
	"strings"
	"testing"
	"time"
)

const userLine2 = `{"type":"user","message":{"role":"user","content":"and add a test"},"parent_tool_use_id":null,"session_id":"","uuid":"u-2"}`

// sendSecond sends a second user message while turn 1 runs.
func sendSecond(t *testing.T, h *harness, c *fakeChild) {
	t.Helper()
	h.send(userLine2)
	if got := c.read(t); got != userLine2+"\n" {
		t.Fatalf("child got %q", got)
	}
}

func TestSecondMessageTurnCutOffGetsResumePrompt(t *testing.T) {
	h := newHarness(t)
	c := h.run(streamArgs, workEnv, nil)
	startTurn(t, h, c)
	sendSecond(t, h, c)
	c.say(t, resultOK) // turn 1 ends
	if got := h.out(); got != resultOK+"\n" {
		t.Fatalf("sdk got %q", got)
	}
	c.say(t, errResult(limitText)) // turn 2 cut off
	c2 := expectReplacement(t, h, "", "switched from work to main")
	finishOnReplacement(t, h, c2)
}

func TestArmedSwitchResendsMessageAlreadyWrittenToOldChild(t *testing.T) {
	h := newHarness(t)
	c := h.run(streamArgs, workEnv, nil)
	startTurn(t, h, c)
	sendSecond(t, h, c)
	c.say(t, rejected(t0.Add(time.Hour), "five_hour"))
	c.say(t, resultOK)
	if got := h.out(); got != resultOK+"\n" {
		t.Fatalf("sdk got %q", got)
	}
	c2 := h.child()
	if got := c2.read(t); got != initReq+"\n" {
		t.Fatalf("replay %q", got)
	}
	if got := c2.read(t); got != userLine2+"\n" {
		t.Fatalf("unanswered message not resent, got %q", got)
	}
}

func TestCutOffResendsLaterMessagesAfterPrompt(t *testing.T) {
	h := newHarness(t)
	c := h.run(streamArgs, workEnv, nil)
	startTurn(t, h, c)
	sendSecond(t, h, c)
	c.say(t, errResult(limitText))
	c2 := expectReplacement(t, h, "", "switched from work to main")
	if got := c2.read(t); got != userLine2+"\n" {
		t.Fatalf("later message not resent, got %q", got)
	}
	// Two results end both turns; a further limit has no turn to resume.
	c2.say(t, initResp)
	c2.say(t, resultOK)
	c2.say(t, resultOK)
	h.out()
	h.out()
	c2.say(t, rejected(t0.Add(-time.Hour), "five_hour"))
	c2.say(t, errResult(limitText))
	h.waitTimer(minSwitchGap)
	h.clock.Advance(minSwitchGap)
	c3 := h.child()
	if got := c3.read(t); got != initReq+"\n" {
		t.Fatalf("replay %q", got)
	}
	h.send(userLine)
	if got := c3.read(t); got != userLine+"\n" {
		t.Fatalf("want no resume prompt, got %q", got)
	}
}

// A child that stops reading stdin while it writes stdout must not stall
// the proxy: its output still reaches the SDK.
func TestBlockedChildStdinDoesNotStallOutput(t *testing.T) {
	h := newHarness(t)
	c := h.run(streamArgs, workEnv, nil)
	go func() {
		for i := 0; i < 600; i++ {
			if _, err := io.WriteString(h.sdkIn, userLine+"\n"); err != nil {
				return
			}
		}
	}()
	time.Sleep(50 * time.Millisecond)
	go func() { _, _ = io.WriteString(c.outW, assistOK+"\n") }()
	if got := h.out(); got != assistOK+"\n" {
		t.Fatalf("sdk got %q", got)
	}
	for i := 0; i < 600; i++ {
		if got := c.read(t); !strings.Contains(got, "fix the bug") {
			t.Fatalf("line %d: %q", i, got)
		}
	}
}

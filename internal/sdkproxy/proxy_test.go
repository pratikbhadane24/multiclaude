package sdkproxy

import (
	"fmt"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/status"
)

const (
	initReq   = `{"type":"control_request","request_id":"r1","request":{"subtype":"initialize","hooks":{}}}`
	initResp  = `{"type":"control_response","response":{"subtype":"success","request_id":"r1","response":{}}}`
	userLine  = `{"type":"user","message":{"role":"user","content":"fix the bug"},"parent_tool_use_id":null,"session_id":"","uuid":"u-1"}`
	sysInit   = `{"type":"system","subtype":"init","session_id":"S1","model":"m"}`
	assistOK  = `{"type":"assistant","message":{"content":[{"type":"text","text":"done"}]},"session_id":"S1"}`
	resultOK  = `{"type":"result","subtype":"success","is_error":false,"result":"done","session_id":"S1"}`
	limitText = "You've hit your 5-hour limit · resets 1:20am (Asia/Calcutta)"
)

var workEnv = []string{"HOME=/home", "CLAUDE_CONFIG_DIR=/acc/work"}

func rejected(resetsAt time.Time, kind string) string {
	return fmt.Sprintf(`{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","resetsAt":%d,"rateLimitType":%q},"session_id":"S1"}`,
		resetsAt.Unix(), kind)
}

func errResult(text string) string {
	return fmt.Sprintf(`{"type":"result","subtype":"success","is_error":true,"result":%q,"session_id":"S1"}`, text)
}

func assistErr(kind, text string) string {
	return fmt.Sprintf(`{"type":"assistant","message":{"content":[{"type":"text","text":%q}]},"error":%q,"session_id":"S1"}`, text, kind)
}

// startTurn runs the SDK handshake and one user turn on c, checking the
// lines arrive verbatim both ways.
func startTurn(t *testing.T, h *harness, c *fakeChild) {
	t.Helper()
	h.send(initReq)
	if got := c.read(t); got != initReq+"\n" {
		t.Fatalf("child got %q", got)
	}
	c.say(t, initResp)
	if got := h.out(); got != initResp+"\n" {
		t.Fatalf("sdk got %q", got)
	}
	h.send(userLine)
	if got := c.read(t); got != userLine+"\n" {
		t.Fatalf("child got %q", got)
	}
	c.say(t, sysInit)
	if got := h.out(); got != sysInit+"\n" {
		t.Fatalf("sdk got %q", got)
	}
}

// expectReplacement checks a replacement child: resumed argv, account env,
// replayed initialize, then the resume prompt.
func expectReplacement(t *testing.T, h *harness, wantDir, wantPrompt string) *fakeChild {
	t.Helper()
	c := h.child()
	want := append([]string{"claude"}, ResumeArgs(streamArgs, "S1")...)
	if !reflect.DeepEqual(c.argv, want) || c.argv0 != testPaths.RealClaude {
		t.Fatalf("argv %q", c.argv)
	}
	dir, has := c.envValue("CLAUDE_CONFIG_DIR")
	if (wantDir == "") == has || dir != wantDir {
		t.Fatalf("CLAUDE_CONFIG_DIR = %q (set %v), want %q", dir, has, wantDir)
	}
	if got := c.read(t); got != initReq+"\n" {
		t.Fatalf("replay got %q", got)
	}
	if got := c.read(t); !strings.Contains(got, wantPrompt) || !strings.Contains(got, `"type":"user"`) {
		t.Fatalf("resume prompt %q", got)
	}
	return c
}

// finishOnReplacement plays a replacement child's start: its answer to the
// replayed initialize and its init are hidden, the rest reaches the SDK.
func finishOnReplacement(t *testing.T, h *harness, c *fakeChild) {
	t.Helper()
	c.say(t, initResp)
	c.say(t, sysInit)
	c.say(t, assistOK)
	if got := h.out(); got != assistOK+"\n" {
		t.Fatalf("sdk got %q (initialize answer or init leaked)", got)
	}
	c.say(t, resultOK)
	if got := h.out(); got != resultOK+"\n" {
		t.Fatalf("sdk got %q", got)
	}
}

func TestPassthroughIsVerbatimAndPropagatesExit(t *testing.T) {
	h := newHarness(t)
	c := h.run(streamArgs, workEnv, nil)
	if !reflect.DeepEqual(c.argv, append([]string{"claude"}, streamArgs...)) || !reflect.DeepEqual(c.env, workEnv) {
		t.Fatalf("child argv %q env %q", c.argv, c.env)
	}
	big := `{"type":"user","message":{"content":"` + strings.Repeat("x", 300_000) + `"}}`
	h.send(big)
	if got := c.read(t); got != big+"\n" {
		t.Fatalf("big line changed (len %d)", len(got))
	}
	h.send("not json")
	if got := c.read(t); got != "not json\n" {
		t.Fatalf("got %q", got)
	}
	for _, l := range []string{"garbage {", big, `{"type":"rate_limit_event","rate_limit_info":{"status":"allowed_warning"}}`, errResult("API Error: Connection error (ECONNRESET)")} {
		c.say(t, l)
		if got := h.out(); got != l+"\n" {
			t.Fatalf("sdk got %q", got[:min(len(got), 80)])
		}
	}
	c.exit(3)
	if code := h.exitCode(); code != 3 {
		t.Fatalf("exit %d", code)
	}
	h.noChild()
	if len(h.getMarks()) != 0 {
		t.Fatalf("marks %v", h.getMarks())
	}
}

func TestLimitViaRateLimitEventSwitchesAccount(t *testing.T) {
	h := newHarness(t)
	c := h.run(streamArgs, workEnv, nil)
	startTurn(t, h, c)
	reset := t0.Add(2 * time.Hour)
	c.say(t, rejected(reset, "five_hour"))
	c.say(t, assistErr("rate_limit", limitText))
	c.say(t, errResult(limitText))
	c2 := expectReplacement(t, h, "", "switched from work to main")
	if sigs := c.signals(); len(sigs) == 0 || sigs[0] != syscall.SIGTERM {
		t.Fatalf("old child signals %v", sigs)
	}
	finishOnReplacement(t, h, c2)
	marks := h.getMarks()
	if len(marks) != 1 || marks[0].account != "work" || marks[0].limit.Type != "five_hour" || !marks[0].limit.ResetsAt.Equal(reset) {
		t.Fatalf("marks %+v", marks)
	}
	if !strings.Contains(h.stderr.String(), "ccm: work limited — continuing on main") {
		t.Fatalf("stderr %q", h.stderr.String())
	}
	c2.exit(0)
	if code := h.exitCode(); code != 0 {
		t.Fatalf("exit %d", code)
	}
}

func TestLimitViaResultTextSwitchesAccount(t *testing.T) {
	h := newHarness(t)
	h.accts = defaultAccounts()[1:] // work, alt
	c := h.run(streamArgs, workEnv, nil)
	startTurn(t, h, c)
	c.say(t, errResult("API Error: Request rejected (429) · This request would exceed your account's rate limit"))
	c2 := expectReplacement(t, h, "/acc/alt", "switched from work to alt")
	finishOnReplacement(t, h, c2)
	marks := h.getMarks()
	if len(marks) != 1 || !marks[0].limit.ResetsAt.Equal(t0.Add(defaultLimitHold)) {
		t.Fatalf("marks %+v", marks)
	}
}

func TestNonLimitErrorsPassThrough(t *testing.T) {
	h := newHarness(t)
	c := h.run(streamArgs, workEnv, nil)
	startTurn(t, h, c)
	for _, l := range []string{
		assistErr("billing_error", "Credit balance is too low"),
		errResult("Credit balance is too low"),
		assistErr("server_error", "API Error: Can't reach the API server"),
		`{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","isUsingOverage":true}}`,
		assistOK,
	} {
		c.say(t, l)
		if got := h.out(); got != l+"\n" {
			t.Fatalf("sdk got %q", got)
		}
	}
	h.noChild()
}

func TestRejectedEventThenOtherErrorDoesNotSwitch(t *testing.T) {
	h := newHarness(t)
	c := h.run(streamArgs, workEnv, nil)
	startTurn(t, h, c)
	c.say(t, rejected(t0.Add(time.Hour), "five_hour"))
	c.say(t, assistErr("server_error", "ECONNRESET"))
	if got := h.out(); got != rejected(t0.Add(time.Hour), "five_hour")+"\n" {
		t.Fatalf("held event not flushed first, sdk got %q", got)
	}
	if got := h.out(); !strings.Contains(got, "ECONNRESET") {
		t.Fatalf("sdk got %q", got)
	}
	c.say(t, errResult("API Error: ECONNRESET"))
	if got := h.out(); !strings.Contains(got, "ECONNRESET") {
		t.Fatalf("sdk got %q", got)
	}
	h.noChild()
}

func TestRejectedThenAllowedEventDoesNotSwitch(t *testing.T) {
	h := newHarness(t)
	c := h.run(streamArgs, workEnv, nil)
	startTurn(t, h, c)
	ev := rejected(t0.Add(time.Hour), "five_hour")
	allowed := `{"type":"rate_limit_event","rate_limit_info":{"status":"allowed_warning"},"session_id":"S1"}`
	c.say(t, ev)
	c.say(t, allowed)
	for _, want := range []string{ev, allowed} {
		if got := h.out(); got != want+"\n" {
			t.Fatalf("sdk got %q, want %q", got, want)
		}
	}
	c.say(t, resultOK)
	if got := h.out(); got != resultOK+"\n" {
		t.Fatalf("sdk got %q", got)
	}
	h.noChild()
}

func TestCredentialEnvIsStripped(t *testing.T) {
	h := newHarness(t)
	env := append([]string{"ANTHROPIC_API_KEY=k", "CLAUDE_CODE_OAUTH_TOKEN=t", "ANTHROPIC_AUTH_TOKEN=a"}, workEnv...)
	c := h.run(streamArgs, env, nil)
	for _, k := range credentialEnv {
		if _, has := c.envValue(k); has {
			t.Fatalf("%s passed to child: %q", k, c.env)
		}
	}
	if n := strings.Count(h.stderr.String(), "ignored"); n != 1 {
		t.Fatalf("stderr %q", h.stderr.String())
	}
	startTurn(t, h, c)
	c.say(t, errResult(limitText))
	c2 := expectReplacement(t, h, "", "switched from work to main")
	if _, has := c2.envValue("ANTHROPIC_API_KEY"); has {
		t.Fatalf("replacement env %q", c2.env)
	}
}

func TestRejectedEventThenSuccessSwitchesWithoutPrompt(t *testing.T) {
	h := newHarness(t)
	c := h.run(streamArgs, workEnv, nil)
	startTurn(t, h, c)
	c.say(t, rejected(t0.Add(time.Hour), "seven_day"))
	c.say(t, resultOK)
	if got := h.out(); got != resultOK+"\n" {
		t.Fatalf("sdk got %q", got)
	}
	c2 := h.child()
	if got := c2.read(t); got != initReq+"\n" {
		t.Fatalf("replay %q", got)
	}
	h.send(userLine)
	if got := c2.read(t); got != userLine+"\n" {
		t.Fatalf("next turn should go straight to the new child, got %q", got)
	}
}

func TestReplacementInitWithNewSessionIDIsForwarded(t *testing.T) {
	h := newHarness(t)
	c := h.run(streamArgs, workEnv, nil)
	startTurn(t, h, c)
	c.say(t, errResult(limitText))
	c2 := expectReplacement(t, h, "", "switched from work to main")
	c2.say(t, initResp)
	other := strings.Replace(sysInit, "S1", "S2", 1)
	c2.say(t, other)
	if got := h.out(); got != other+"\n" {
		t.Fatalf("sdk got %q", got)
	}
}

func TestReplayUserMessagesEchoOfPromptIsHidden(t *testing.T) {
	h := newHarness(t)
	args := append(append([]string(nil), streamArgs...), "--replay-user-messages")
	c := h.run(args, workEnv, nil)
	startTurn(t, h, c)
	c.say(t, errResult(limitText))
	c2 := h.child()
	c2.read(t)
	c2.read(t)
	c2.say(t, `{"type":"user","isReplay":true,"message":{"content":"[ccm] ..."}}`)
	c2.say(t, assistOK)
	if got := h.out(); got != assistOK+"\n" {
		t.Fatalf("sdk got %q", got)
	}
}

func TestStartsOnFreeAccountWhenCurrentCachedLimited(t *testing.T) {
	h := newHarness(t)
	full, reset := 100.0, t0.Add(time.Hour)
	h.setCache(status.Cache{Accounts: []status.CacheAccount{{Name: "work", FiveHourUsed: &full, FiveHourResetsAt: &reset}}})
	c := h.run(streamArgs, workEnv, nil)
	if _, has := c.envValue("CLAUDE_CONFIG_DIR"); has {
		t.Fatalf("env %q (want main: unset)", c.env)
	}
	if !reflect.DeepEqual(c.argv, append([]string{"claude"}, streamArgs...)) {
		t.Fatalf("argv %q", c.argv)
	}
	if !strings.Contains(h.stderr.String(), "work is limited — starting on main") {
		t.Fatalf("stderr %q", h.stderr.String())
	}
}

func TestSignalsAreForwarded(t *testing.T) {
	h := newHarness(t)
	c := h.run(streamArgs, workEnv, nil)
	h.signals <- syscall.SIGINT
	deadline := time.Now().Add(waitLimit)
	for len(c.signals()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if s := c.signals(); len(s) != 1 || s[0] != syscall.SIGINT {
		t.Fatalf("signals %v", s)
	}
	c.exit(130)
	if code := h.exitCode(); code != 130 {
		t.Fatalf("exit %d", code)
	}
}

func TestStdinEOFClosesChildStdin(t *testing.T) {
	h := newHarness(t)
	c := h.run(streamArgs, workEnv, nil)
	_ = h.sdkIn.Close()
	select {
	case _, ok := <-c.in:
		if ok {
			t.Fatal("unexpected line")
		}
	case <-time.After(waitLimit):
		t.Fatal("child stdin not closed")
	}
	c.exit(0)
	if code := h.exitCode(); code != 0 {
		t.Fatalf("exit %d", code)
	}
}

func TestCustomResumePrompt(t *testing.T) {
	h := newHarness(t)
	c := h.run(streamArgs, workEnv, func(d *Deps) { d.ResumePrompt = "keep going please" })
	startTurn(t, h, c)
	c.say(t, errResult(limitText))
	expectReplacement(t, h, "", "keep going please")
}

func TestStartFailureExits127(t *testing.T) {
	h := newHarness(t)
	d := h.deps(strings.NewReader(""), &syncBuffer{})
	d.Start = func(string, []string, []string) (Child, error) { return nil, fmt.Errorf("nope") }
	if code := Run(streamArgs, workEnv, d); code != exitCannotStart {
		t.Fatalf("exit %d", code)
	}
}

package sdkproxy

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"multiclaude/internal/paths"
	"multiclaude/internal/registry"
)

func TestIsLimitText(t *testing.T) {
	yes := []string{
		"You've hit your weekly limit · resets 10:30am (Asia/Calcutta)",
		"You've hit your 5-hour limit · resets 1:20am (Asia/Calcutta)",
		"You’ve hit your limit",
		"API Error: Request rejected (429) · This request would exceed your account's rate limit",
	}
	no := []string{
		"API Error: Connection error. ECONNRESET",
		"API Error: Can't reach the API server",
		"Credit balance is too low",
		"Invalid API key · Please run /login",
		"limit reached somewhere",
	}
	for _, s := range yes {
		if !isLimitText(s) {
			t.Errorf("want limit: %q", s)
		}
	}
	for _, s := range no {
		if isLimitText(s) {
			t.Errorf("not a limit: %q", s)
		}
	}
}

func TestLimitDetectionRules(t *testing.T) {
	mustParse := func(l string) message {
		m, ok := parse([]byte(l))
		if !ok {
			t.Fatalf("parse %q", l)
		}
		return m
	}
	if !mustParse(rejected(t0, "five_hour")).limitRejected() {
		t.Error("rejected event")
	}
	if mustParse(`{"type":"rate_limit_event","rate_limit_info":{"status":"allowed_warning"}}`).limitRejected() {
		t.Error("allowed_warning must not trigger")
	}
	if mustParse(resultOK).limitError(true) {
		t.Error("successful result is never a limit")
	}
	if !mustParse(errResult("anything")).limitError(true) {
		t.Error("error result after rejected event")
	}
	if mustParse(errResult("anything")).limitError(false) {
		t.Error("unrelated error result")
	}
	if !mustParse(assistErr("rate_limit", limitText)).limitError(false) {
		t.Error("synthetic rate_limit assistant message")
	}
	normal := `{"type":"assistant","message":{"content":[{"type":"text","text":"` + limitText + `"}]}}`
	if mustParse(normal).limitError(true) {
		t.Error("a normal assistant message quoting the text is not a limit")
	}
	if _, ok := parse([]byte("nope")); ok {
		t.Error("non-JSON parsed")
	}
}

func TestLimitFrom(t *testing.T) {
	reset := t0.Add(3 * time.Hour)
	l := limitFrom(rateLimitInfo{ResetsAt: reset.Unix(), RateLimitType: "seven_day"}, t0)
	if l.Type != "seven_day" || !l.ResetsAt.Equal(reset) {
		t.Fatalf("%+v", l)
	}
	if l := limitFrom(rateLimitInfo{}, t0); !l.ResetsAt.Equal(t0.Add(defaultLimitHold)) {
		t.Fatalf("%+v", l)
	}
}

func decode(t *testing.T, line []byte) map[string]any {
	t.Helper()
	if line[len(line)-1] != '\n' {
		t.Fatal("no newline")
	}
	var m map[string]any
	if err := json.Unmarshal(line, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestResumeLineKeepsShapeAndReplacesContent(t *testing.T) {
	captured := []byte(`{"type":"user","message":{"role":"user","content":"orig"},"parent_tool_use_id":"x","session_id":"S1","uuid":"u-1","priority":"now"}`)
	before := string(captured)
	m := decode(t, resumeLine(captured, "S9", "NOTE"))
	if string(captured) != before {
		t.Fatal("input mutated")
	}
	want := map[string]any{
		"type": "user", "session_id": "S1", "priority": "now", "parent_tool_use_id": nil,
		"message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "NOTE"}}},
	}
	if !reflect.DeepEqual(m, want) {
		t.Fatalf("got %v", m)
	}
}

func TestResumeLineFallback(t *testing.T) {
	m := decode(t, resumeLine(nil, "S9", "NOTE"))
	if m["type"] != "user" || m["session_id"] != "S9" || m["parent_tool_use_id"] != nil {
		t.Fatalf("got %v", m)
	}
	if !strings.Contains(string(resumeLine([]byte("garbage"), "S9", "NOTE")), `"text":"NOTE"`) {
		t.Fatal("garbage capture")
	}
}

func TestResumeNote(t *testing.T) {
	if got := resumeNote("", "a", "b"); !strings.Contains(got, "switched from a to b") || strings.Contains(got, "{") {
		t.Fatalf("%q", got)
	}
	if got := resumeNote("", "a", "a"); got != ResumeResetPrompt {
		t.Fatalf("%q", got)
	}
	if got := resumeNote("custom {from}", "a", "b"); got != "custom {from}" {
		t.Fatalf("%q", got)
	}
}

func TestCurrentAccountAndEnv(t *testing.T) {
	p := paths.Paths{Home: "/home", ClaudeDir: "/home/.claude"}
	accts := []registry.Account{{Name: "main"}, {Name: "work", Dir: "/acc/work"}}
	for _, c := range []struct {
		env  []string
		want string
		ok   bool
	}{
		{nil, "main", true},
		{[]string{"CLAUDE_CONFIG_DIR=/home/.claude/"}, "main", true},
		{[]string{"CLAUDE_CONFIG_DIR=/acc/work/"}, "work", true},
		{[]string{"CLAUDE_CONFIG_DIR=/elsewhere"}, "", false},
	} {
		a, ok := currentAccount(c.env, p, accts)
		if a.Name != c.want || ok != c.ok {
			t.Errorf("%q -> %+v %v", c.env, a, ok)
		}
	}
	env := []string{"A=1", "CLAUDE_CONFIG_DIR=/acc/work"}
	if got := envFor(env, accts[0]); !reflect.DeepEqual(got, []string{"A=1"}) {
		t.Fatalf("main env %q", got)
	}
	if got := envFor(env, accts[1]); !reflect.DeepEqual(got, env) {
		t.Fatalf("work env %q", got)
	}
}

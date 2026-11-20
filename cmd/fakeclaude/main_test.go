package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

var fixedNow = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }

func TestSlugReplacesEveryNonAlphanumeric(t *testing.T) {
	got := slug("/Users/a.b/Work/my_proj x")
	if want := "-Users-a-b-Work-my-proj-x"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestTranscriptPath(t *testing.T) {
	if got, want := transcriptPath("/acc/work", "/home/u", "/w/p.x", "id1"), "/acc/work/projects/-w-p-x/id1.jsonl"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if got, want := transcriptPath("", "/home/u", "/w", "id1"), "/home/u/.claude/projects/-w/id1.jsonl"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want options
	}{
		{"session id and prompt", []string{"--session-id", "abc", "hello"}, options{SessionID: "abc", Prompt: "hello"}},
		{"resume", []string{"--resume", "abc", "[ccm] go"}, options{SessionID: "abc", IsResume: true, Prompt: "[ccm] go"}},
		{"value flag not a prompt", []string{"--model", "opus", "--dangerously-skip-permissions"}, options{}},
		{"equals spelling", []string{"--output-format=stream-json", "--input-format=stream-json"},
			options{OutputFormat: "stream-json", InputFormat: "stream-json"}},
		{"space spelling", []string{"--output-format", "stream-json", "--input-format", "stream-json", "-p"},
			options{OutputFormat: "stream-json", InputFormat: "stream-json"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseArgs(tc.args); got != tc.want {
				t.Fatalf("got %+v want %+v", got, tc.want)
			}
		})
	}
	if parseArgs([]string{"--output-format", "stream-json"}).isStream() {
		t.Fatal("output format alone must not be stream mode")
	}
}

func TestLoadEnvLimit(t *testing.T) {
	get := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	e := loadEnv(get(map[string]string{"CLAUDE_CONFIG_DIR": "/x/work", "FAKECLAUDE_LIMIT_ACCOUNTS": "a, work", "FAKECLAUDE_LIMIT_AFTER": "0.5"}), "/h", "/c")
	if !e.Limited || e.Account != "work" || e.LimitAfter != 500*time.Millisecond {
		t.Fatalf("unexpected env %+v", e)
	}
	e = loadEnv(get(map[string]string{"FAKECLAUDE_LIMIT_ACCOUNTS": "main"}), "/h", "/c")
	if !e.Limited || e.Account != "main" || e.LimitAfter != defaultLimitAfter {
		t.Fatalf("main should be limited with default delay: %+v", e)
	}
	if loadEnv(get(nil), "/h", "/c").Limited {
		t.Fatal("nothing limited by default")
	}
}

func TestLimitLineRoundTrips(t *testing.T) {
	var got map[string]any
	if err := json.Unmarshal(marshalLine(limitLine("sid", "/cwd", fixedNow())), &got); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]any{
		"type": "assistant", "isApiErrorMessage": true, "error": "rate_limit", "isSidechain": false,
		"sessionId": "sid", "session_id": "sid", "cwd": "/cwd", "timestamp": "2026-01-02T03:04:05Z",
	} {
		if got[k] != want {
			t.Errorf("%s = %v want %v", k, got[k], want)
		}
	}
	if v, ok := got["parentUuid"]; !ok || v != nil {
		t.Errorf("parentUuid must be present and null, got %v (%v)", v, ok)
	}
	if u, _ := got["uuid"].(string); len(u) != 36 {
		t.Errorf("uuid %q", u)
	}
	msg := got["message"].(map[string]any)
	block := msg["content"].([]any)[0].(map[string]any)
	if msg["role"] != "assistant" || block["type"] != "text" || block["text"] != limitText {
		t.Errorf("bad message %v", msg)
	}
}

func streamOut(t *testing.T, e env, input string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := runStream(strings.NewReader(input), &out, e, "sid", fixedNow, func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	var msgs []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("bad line %q: %v", l, err)
		}
		msgs = append(msgs, m)
	}
	return msgs
}

const streamInput = `{"type":"control_request","request_id":"r1","request":{"subtype":"initialize"}}
{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hi there"}]}}
`

func TestStreamLimited(t *testing.T) {
	msgs := streamOut(t, env{Account: "work", Limited: true}, streamInput)
	if len(msgs) != 4 {
		t.Fatalf("got %d messages: %v", len(msgs), msgs)
	}
	if msgs[0]["type"] != "system" || msgs[0]["subtype"] != "init" || msgs[0]["session_id"] != "sid" {
		t.Errorf("bad init %v", msgs[0])
	}
	resp := msgs[1]["response"].(map[string]any)
	if msgs[1]["type"] != "control_response" || resp["subtype"] != "success" || resp["request_id"] != "r1" {
		t.Errorf("bad control response %v", msgs[1])
	}
	info := msgs[2]["rate_limit_info"].(map[string]any)
	if msgs[2]["type"] != "rate_limit_event" || info["status"] != "rejected" || info["rateLimitType"] != "five_hour" ||
		info["utilization"] != 1.0 || info["resetsAt"] != float64(fixedNow().Unix()+3600) {
		t.Errorf("bad rate limit event %v", msgs[2])
	}
	if msgs[3]["type"] != "result" || msgs[3]["is_error"] != true || msgs[3]["result"] != limitText || msgs[3]["session_id"] != "sid" {
		t.Errorf("bad result %v", msgs[3])
	}
}

func TestStreamOK(t *testing.T) {
	msgs := streamOut(t, env{Account: "work"}, streamInput)
	if len(msgs) != 4 {
		t.Fatalf("got %d messages", len(msgs))
	}
	text := msgs[2]["message"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if msgs[2]["type"] != "assistant" || !strings.Contains(text, "hi there") || !strings.Contains(text, "work") {
		t.Errorf("bad echo %v", msgs[2])
	}
	if msgs[3]["is_error"] != false || msgs[3]["result"] != "ok" {
		t.Errorf("bad result %v", msgs[3])
	}
}

func TestStreamStringContentAndGarbage(t *testing.T) {
	msgs := streamOut(t, env{Account: "a"}, "not json\n\n"+`{"type":"user","message":{"role":"user","content":"plain"}}`+"\n")
	if len(msgs) != 3 || !strings.Contains(msgs[1]["message"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string), "plain") {
		t.Fatalf("unexpected %v", msgs)
	}
}

func readLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatal(err)
		}
		out = append(out, m)
	}
	return out
}

func TestRunInteractiveOK(t *testing.T) {
	dir := t.TempDir()
	e := env{Log: filepath.Join(dir, "log.jsonl"), ConfigDir: filepath.Join(dir, "acc", "work"), Cwd: "/w/p", Account: "work"}
	var out, errb bytes.Buffer
	code := run([]string{"--resume", "sid9", "[ccm] continue"}, e, nil, &out, &errb, make(chan os.Signal), fixedNow)
	if code != 0 {
		t.Fatalf("code %d stderr %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "fakeclaude ok on work, prompt: [ccm] continue") {
		t.Errorf("stdout %q", out.String())
	}
	log := readLines(t, e.Log)
	if len(log) != 1 || log[0]["session_id"] != "sid9" || log[0]["resume"] != true || log[0]["account"] != "work" || log[0]["prompt"] != "[ccm] continue" {
		t.Errorf("bad log %v", log)
	}
	tr := readLines(t, filepath.Join(e.ConfigDir, "projects", "-w-p", "sid9.jsonl"))
	if len(tr) != 2 || tr[0]["type"] != "user" || tr[1]["type"] != "assistant" {
		t.Errorf("bad transcript %v", tr)
	}
}

func TestRunInteractiveLimitedWaitsForSignal(t *testing.T) {
	tests := []struct {
		sig  os.Signal
		want int
	}{{syscall.SIGINT, 0}, {syscall.SIGTERM, 143}}
	for _, tc := range tests {
		t.Run(tc.sig.String(), func(t *testing.T) {
			dir := t.TempDir()
			e := env{ConfigDir: filepath.Join(dir, "work"), Cwd: "/w", Account: "work", Limited: true, LimitAfter: 10 * time.Millisecond}
			sigs := make(chan os.Signal, 1)
			codes := make(chan int, 1)
			go func() {
				codes <- run([]string{"--session-id", "s1"}, e, nil, &bytes.Buffer{}, &bytes.Buffer{}, sigs, fixedNow)
			}()
			select {
			case c := <-codes:
				t.Fatalf("exited early with %d", c)
			case <-time.After(150 * time.Millisecond):
			}
			sigs <- tc.sig
			if c := <-codes; c != tc.want {
				t.Fatalf("code %d want %d", c, tc.want)
			}
			tr := readLines(t, filepath.Join(e.ConfigDir, "projects", "-w", "s1.jsonl"))
			last := tr[len(tr)-1]
			if last["error"] != "rate_limit" || last["isApiErrorMessage"] != true {
				t.Errorf("last transcript line %v", last)
			}
		})
	}
}

func TestRunStreamMode(t *testing.T) {
	var out, errb bytes.Buffer
	e := env{Account: "main", Cwd: "/w"}
	code := run([]string{"--output-format=stream-json", "--input-format", "stream-json", "--session-id", "q"},
		e, strings.NewReader(streamInput), &out, &errb, nil, fixedNow)
	if code != 0 || !strings.Contains(out.String(), `"session_id":"q"`) {
		t.Fatalf("code %d out %s err %s", code, out.String(), errb.String())
	}
}

func TestRunSubcommandExitsImmediately(t *testing.T) {
	dir := t.TempDir()
	e := env{Log: filepath.Join(dir, "log.jsonl"), ConfigDir: filepath.Join(dir, "work"), Cwd: "/w", Account: "work"}
	var out bytes.Buffer
	if code := run([]string{"auth", "status"}, e, nil, &out, &out, nil, fixedNow); code != 0 {
		t.Fatalf("code %d", code)
	}
	log := readLines(t, e.Log)
	if len(log) != 1 || log[0]["mode"] != "subcommand" {
		t.Errorf("bad log %v", log)
	}
	if _, err := os.Stat(filepath.Join(e.ConfigDir, "projects")); err == nil {
		t.Error("subcommand must not write a transcript")
	}
}

func TestRunStreamRecordsUserMessages(t *testing.T) {
	dir := t.TempDir()
	e := env{Log: filepath.Join(dir, "log.jsonl"), Account: "b", Cwd: "/w"}
	code := run([]string{"--output-format=stream-json", "--input-format=stream-json", "--resume", "s7"},
		e, strings.NewReader(streamInput), &bytes.Buffer{}, &bytes.Buffer{}, nil, fixedNow)
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	log := readLines(t, e.Log)
	if len(log) != 2 || log[1]["event"] != "user_message" || log[1]["text"] != "hi there" ||
		log[1]["session_id"] != "s7" || log[1]["account"] != "b" || log[0]["mode"] != "stream-json" {
		t.Errorf("bad log %v", log)
	}
}

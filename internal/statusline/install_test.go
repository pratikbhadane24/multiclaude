package statusline

import (
	"encoding/json"
	"strings"
	"testing"
)

const cavemanEscaped = `bash \"/Users/x/.claude/plugins/caveman-statusline.sh\"`

func statusLineOf(t *testing.T, doc []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(doc, &m); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, doc)
	}
	sl, _ := m["statusLine"].(map[string]any)
	return sl
}

func TestShellQuoteRoundTrip(t *testing.T) {
	for _, s := range []string{"plain", "has space", `bash "/a b/c.sh"`, "it's", "", "a'b'c", "$x `y`"} {
		w, rest, ok := parseWord(shellQuote(s))
		if !ok || w != s || rest != "" {
			t.Errorf("%q -> %q rest=%q ok=%v", s, w, rest, ok)
		}
	}
}

func TestInstallNoStatusLinePreservesBytes(t *testing.T) {
	doc := "{\n  \"a\":   [1,  2],\n  \"b\": {\"x\":\"<&>\"}\n}\n"

	e, err := PlanInstall([]byte(doc), "/opt/ccm")

	if err != nil || e.NoOp {
		t.Fatalf("err=%v noop=%v", err, e.NoOp)
	}
	if !strings.HasPrefix(string(e.Content), "{\n  \"a\":   [1,  2],\n  \"b\": {\"x\":\"<&>\"}") {
		t.Fatalf("existing bytes changed:\n%s", e.Content)
	}
	sl := statusLineOf(t, e.Content)
	if sl["type"] != "command" || sl["command"] != "/opt/ccm statusline" {
		t.Fatalf("sl=%v", sl)
	}
	if e.Before != "(none)" || !strings.Contains(e.After, "/opt/ccm statusline") {
		t.Fatalf("before=%q after=%q", e.Before, e.After)
	}
}

func TestInstallMissingOrEmptyFile(t *testing.T) {
	for _, in := range [][]byte{nil, []byte("{}"), []byte("{}\n")} {
		e, err := PlanInstall(in, "/opt/ccm")
		if err != nil {
			t.Fatal(err)
		}
		if sl := statusLineOf(t, e.Content); sl["command"] != "/opt/ccm statusline" {
			t.Fatalf("in=%q sl=%v", in, sl)
		}
	}
}

func TestInstallWrapsCavemanCommandWithQuotesAndKeepsPadding(t *testing.T) {
	doc := `{"x": 1, "statusLine": {"type":"command","command":"` + cavemanEscaped + `","padding":2}, "y": true}`

	e, err := PlanInstall([]byte(doc), "/opt/my ccm")

	if err != nil {
		t.Fatal(err)
	}
	sl := statusLineOf(t, e.Content)
	want := `'/opt/my ccm' statusline --wrap 'bash "/Users/x/.claude/plugins/caveman-statusline.sh"'`
	if sl["command"] != want || sl["padding"] != float64(2) {
		t.Fatalf("sl=%v", sl)
	}
	if !strings.HasPrefix(string(e.Content), `{"x": 1, "statusLine": `) || !strings.HasSuffix(string(e.Content), `, "y": true}`) {
		t.Fatalf("surroundings changed: %s", e.Content)
	}
}

func TestInstallIdempotent(t *testing.T) {
	first, err := PlanInstall([]byte(`{"statusLine":{"type":"command","command":"`+cavemanEscaped+`"}}`), "/opt/ccm")
	if err != nil {
		t.Fatal(err)
	}

	again, err := PlanInstall(first.Content, "/opt/ccm")

	if err != nil || !again.NoOp {
		t.Fatalf("err=%v noop=%v", err, again.NoOp)
	}
}

func TestInstallNonCommandTypeWarnsAndReplaces(t *testing.T) {
	doc := `{"statusLine":{"type":"static","text":"hi"},"k":1}`

	e, err := PlanInstall([]byte(doc), "/opt/ccm")

	if err != nil || e.Warn == "" {
		t.Fatalf("err=%v warn=%q", err, e.Warn)
	}
	sl := statusLineOf(t, e.Content)
	if sl["command"] != "/opt/ccm statusline" || sl["text"] != nil {
		t.Fatalf("sl=%v", sl)
	}
}

func TestInstallRejectsNonObject(t *testing.T) {
	if _, err := PlanInstall([]byte(`[1]`), "/opt/ccm"); err == nil {
		t.Fatal("want error")
	}
	if _, err := PlanInstall([]byte(`{"a":`), "/opt/ccm"); err == nil {
		t.Fatal("want error")
	}
}

func TestUninstallRestoresWrapped(t *testing.T) {
	orig := `{"a":1,"statusLine":{"type":"command","command":"` + cavemanEscaped + `","padding":1},"b":2}`
	inst, err := PlanInstall([]byte(orig), "/opt/ccm")
	if err != nil {
		t.Fatal(err)
	}

	un, err := PlanUninstall(inst.Content)

	if err != nil || un.NoOp {
		t.Fatalf("err=%v noop=%v", err, un.NoOp)
	}
	if string(un.Content) != orig {
		t.Fatalf("not restored:\n%s\nwant\n%s", un.Content, orig)
	}
}

func TestUninstallRemovesFreshStatusLine(t *testing.T) {
	cases := map[string]string{
		"last":   "{\n  \"a\": 1\n}\n",
		"middle": "{\n  \"a\": 1,\n  \"b\": [2]\n}\n",
		"only":   "{}\n",
		"inline": `{"a":1,"b":2}`,
	}
	for name, orig := range cases {
		inst, err := PlanInstall([]byte(orig), "/opt/ccm")
		if err != nil {
			t.Fatal(err)
		}
		un, err := PlanUninstall(inst.Content)
		if err != nil || statusLineOf(t, un.Content) != nil {
			t.Fatalf("%s: err=%v content=%s", name, err, un.Content)
		}
		var a, b any
		_ = json.Unmarshal([]byte(orig), &a)
		_ = json.Unmarshal(un.Content, &b)
		ja, _ := json.Marshal(a)
		jb, _ := json.Marshal(b)
		if string(ja) != string(jb) {
			t.Fatalf("%s: other keys changed: %s vs %s", name, ja, jb)
		}
	}
}

func TestUninstallRemovalWhenStatusLineIsFirstOfMany(t *testing.T) {
	doc := "{\n  \"statusLine\": {\"type\":\"command\",\"command\":\"/opt/ccm statusline\"},\n  \"b\": 1\n}\n"
	un, err := PlanUninstall([]byte(doc))
	if err != nil || string(un.Content) != "{\n  \"b\": 1\n}\n" {
		t.Fatalf("err=%v content=%q", err, un.Content)
	}
}

func TestUninstallNotInstalled(t *testing.T) {
	for _, doc := range []string{`{}`, `{"statusLine":{"type":"command","command":"echo hi"}}`} {
		un, err := PlanUninstall([]byte(doc))
		if err != nil || !un.NoOp {
			t.Fatalf("doc=%s err=%v noop=%v", doc, err, un.NoOp)
		}
	}
}

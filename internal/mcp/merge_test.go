package mcp

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func servers(kv ...string) map[string]json.RawMessage {
	m := map[string]json.RawMessage{}
	for i := 0; i < len(kv); i += 2 {
		m[kv[i]] = json.RawMessage(kv[i+1])
	}
	return m
}

func keysOf(m map[string]json.RawMessage) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestMergeAddsMainServersToAccount(t *testing.T) {
	main := servers("a", `{"command":"a"}`, "b", `{"command":"b"}`)
	acct := servers("x", `{"command":"x"}`)

	got, synced, changed := Merge(main, acct, []string{})

	if !reflect.DeepEqual(keysOf(got), []string{"a", "b", "x"}) {
		t.Fatalf("servers = %v", keysOf(got))
	}
	if !reflect.DeepEqual(synced, []string{"a", "b"}) {
		t.Fatalf("synced = %v", synced)
	}
	if !changed {
		t.Fatal("want changed")
	}
}

func TestMergeRemovesServersDroppedFromMainButKeepsAccountOnly(t *testing.T) {
	main := servers("a", `1`)
	acct := servers("a", `1`, "b", `2`, "x", `3`)

	got, synced, changed := Merge(main, acct, []string{"a", "b"})

	if !reflect.DeepEqual(keysOf(got), []string{"a", "x"}) {
		t.Fatalf("servers = %v", keysOf(got))
	}
	if !reflect.DeepEqual(synced, []string{"a"}) {
		t.Fatalf("synced = %v", synced)
	}
	if !changed {
		t.Fatal("want changed")
	}
}

func TestMergeMainWinsOnClash(t *testing.T) {
	got, _, changed := Merge(servers("a", `1`), servers("a", `2`), nil)
	if string(got["a"]) != "1" || !changed {
		t.Fatalf("a = %s changed=%v", got["a"], changed)
	}
}

func TestMergeIdenticalIsNotChanged(t *testing.T) {
	main := servers("a", `{"command": "a"}`)
	acct := servers("a", `{"command":"a"}`, "x", `{}`)

	_, synced, changed := Merge(main, acct, []string{"a"})

	if changed {
		t.Fatal("want not changed (whitespace-only difference)")
	}
	if !reflect.DeepEqual(synced, []string{"a"}) {
		t.Fatalf("synced = %v", synced)
	}
}

func TestMergeIgnoresHTMLEscapingDifferences(t *testing.T) {
	main := servers("a", `{"url": "?a=1&b=2", "x": "<y>"}`)
	bs := `\`
	acct := servers("a", `{"url":"?a=1`+bs+`u0026b=2","x":"`+bs+`u003cy`+bs+`u003e"}`)
	if !strings.Contains(string(acct["a"]), bs+"u0026") {
		t.Fatalf("fixture not escaped: %s", acct["a"])
	}

	if _, _, changed := Merge(main, acct, []string{"a"}); changed {
		t.Fatal("escape-only difference reported as change")
	}
}

func TestMergeEmptyMainAndAccountIsNotChanged(t *testing.T) {
	got, synced, changed := Merge(nil, nil, nil)
	if changed || len(got) != 0 || len(synced) != 0 {
		t.Fatalf("got %v %v %v", got, synced, changed)
	}
}

func TestMergeDoesNotMutateInputs(t *testing.T) {
	main := servers("a", `1`, "b", `2`)
	acct := servers("a", `9`, "c", `3`, "x", `4`)
	prev := []string{"c", "a"}
	mainCopy := servers("a", `1`, "b", `2`)
	acctCopy := servers("a", `9`, "c", `3`, "x", `4`)
	prevCopy := []string{"c", "a"}

	Merge(main, acct, prev)

	if !reflect.DeepEqual(main, mainCopy) || !reflect.DeepEqual(acct, acctCopy) || !reflect.DeepEqual(prev, prevCopy) {
		t.Fatal("inputs mutated")
	}
}

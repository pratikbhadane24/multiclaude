package sdkproxy

import (
	"reflect"
	"testing"
)

func TestIsStreamJSONFlagSpellings(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"--output-format", "stream-json", "--input-format", "stream-json", "--verbose"}, true},
		{[]string{"--output-format=stream-json", "--input-format=stream-json"}, true},
		{[]string{"--output-format", "stream-json", "--input-format=stream-json"}, true},
		{[]string{"--output-format", "stream-json"}, false},
		{[]string{"--output-format", "json", "--input-format", "stream-json"}, false},
		{[]string{"-p", "hi"}, false},
		{[]string{"--input-format"}, false},
	}
	for _, c := range cases {
		if got := IsStreamJSON(c.args); got != c.want {
			t.Errorf("IsStreamJSON(%q) = %v", c.args, got)
		}
	}
}

func TestEnabledNeedsAutoSwitchAndNoOptOut(t *testing.T) {
	args := []string{"--output-format", "stream-json", "--input-format", "stream-json"}
	if !Enabled(args, []string{"HOME=/h"}, true) {
		t.Fatal("want enabled")
	}
	if Enabled(args, nil, false) {
		t.Fatal("auto-switch off")
	}
	if Enabled(args, []string{"CCM_NO_PROXY=1"}, true) {
		t.Fatal("CCM_NO_PROXY set")
	}
	if Enabled([]string{"-p", "x"}, nil, true) {
		t.Fatal("not stream-json")
	}
}

func TestResumeArgsReplacesSessionFlags(t *testing.T) {
	cases := []struct {
		in, want []string
	}{
		{[]string{"--verbose"}, []string{"--verbose", "--resume", "S"}},
		{[]string{"--resume", "old", "--verbose"}, []string{"--verbose", "--resume", "S"}},
		{[]string{"--resume=old", "-r", "x", "--model", "m"}, []string{"--model", "m", "--resume", "S"}},
		{[]string{"--session-id", "u", "--continue", "--fork-session", "-c"}, []string{"--resume", "S"}},
		{[]string{"--resume", "--verbose"}, []string{"--verbose", "--resume", "S"}},
	}
	for _, c := range cases {
		in := append([]string(nil), c.in...)
		got := ResumeArgs(c.in, "S")
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("ResumeArgs(%q) = %q, want %q", c.in, got, c.want)
		}
		if !reflect.DeepEqual(in, c.in) {
			t.Errorf("input mutated: %q", c.in)
		}
	}
}

func TestWithoutEnvCopies(t *testing.T) {
	env := []string{"A=1", "CLAUDE_CONFIG_DIR=/x", "B=2"}
	got := withoutEnv(env, "CLAUDE_CONFIG_DIR")
	if !reflect.DeepEqual(got, []string{"A=1", "B=2"}) || len(env) != 3 {
		t.Fatalf("%q %q", got, env)
	}
	if lookupEnv([]string{"K=a", "K=b"}, "K") != "b" {
		t.Fatal("last wins")
	}
}

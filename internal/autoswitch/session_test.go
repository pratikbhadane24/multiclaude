package autoswitch

import (
	"errors"
	"reflect"
	"testing"
)

const (
	idA = "11111111-2222-4333-8444-555555555555"
	idB = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
)

func newIDFn() string { return idB }

func noLatest() (string, error) { return "", errors.New("unused") }

func TestResolveSessionAddsNewID(t *testing.T) {
	// Arrange
	in := []string{"--model", "opus"}

	// Act
	out, id, err := ResolveSession(in, newIDFn, noLatest)

	// Assert
	if err != nil || id != idB || !reflect.DeepEqual(out, []string{"--model", "opus", "--session-id", idB}) {
		t.Fatalf("out=%q id=%s err=%v", out, id, err)
	}
	if len(in) != 2 {
		t.Fatal("input mutated")
	}
}

func TestResolveSessionKeepsExplicitIDs(t *testing.T) {
	cases := [][]string{
		{"--resume", idA},
		{"-r", idA, "hi"},
		{"--resume=" + idA},
		{"--session-id", idA},
		{"--session-id=" + idA},
	}
	for _, in := range cases {
		out, id, err := ResolveSession(in, newIDFn, noLatest)
		if err != nil || id != idA || !reflect.DeepEqual(out, in) {
			t.Errorf("in=%q out=%q id=%s err=%v", in, out, id, err)
		}
	}
}

func TestResolveSessionContinueBecomesResume(t *testing.T) {
	for _, flag := range []string{"--continue", "-c"} {
		out, id, err := ResolveSession([]string{"--x", flag, "go"}, newIDFn, func() (string, error) { return idA, nil })
		if err != nil || id != idA || !reflect.DeepEqual(out, []string{"--x", "--resume", idA, "go"}) {
			t.Errorf("flag=%s out=%q id=%s err=%v", flag, out, id, err)
		}
	}
}

func TestResolveSessionContinueWithoutConversation(t *testing.T) {
	if _, _, err := ResolveSession([]string{"-c"}, newIDFn, noLatest); err == nil {
		t.Fatal("want error")
	}
}

func TestResolveSessionUnknown(t *testing.T) {
	for _, in := range [][]string{{"--resume"}, {"--resume", "search words"}, {"-r", "--model"}, {"--resume", idA, "--fork-session"}} {
		if _, _, err := ResolveSession(in, newIDFn, noLatest); !errors.Is(err, ErrUnknownSession) {
			t.Errorf("in=%q err=%v", in, err)
		}
	}
}

func TestRelaunchArgsStripsSessionFlags(t *testing.T) {
	base := []string{"--d", "--session-id", idA, "-c", "--resume=" + idA, "--fork-session", "-r", idA, "--model", "x"}

	got := RelaunchArgs(base, idB, "go on")

	want := []string{"--d", "--model", "x", "--resume", idB, "go on"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
	if base[1] != "--session-id" {
		t.Fatal("input mutated")
	}
}

func TestBuildResumePromptAfterSwitch(t *testing.T) {
	got := BuildResumePrompt("main", "work", false, "")
	want := "[ccm] Your previous turn was cut off because the account hit its usage limit. " +
		"Usage is available again (switched from main to work). Resume the task exactly where you left off: " +
		"first check what was already done (files, commands, todos) so you don't repeat finished steps, then continue."
	if got != want {
		t.Fatalf("got %q", got)
	}
}

func TestBuildResumePromptAfterReset(t *testing.T) {
	got := BuildResumePrompt("main", "main", true, "")
	want := "[ccm] Your previous turn was cut off by a usage limit. The limit has now reset. " +
		"Resume the task exactly where you left off: first check what was already done (files, commands, todos) " +
		"so you don't repeat finished steps, then continue."
	if got != want {
		t.Fatalf("got %q", got)
	}
}

func TestBuildResumePromptOverride(t *testing.T) {
	for _, reset := range []bool{false, true} {
		if got := BuildResumePrompt("a", "b", reset, "keep going"); got != "keep going" {
			t.Fatalf("got %q", got)
		}
	}
}

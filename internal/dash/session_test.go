package dash

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/pratikbhadane24/multiclaude/internal/handoff"
)

const testSessionID = "11111111-2222-4333-8444-555555555555"

func withNewSessionID(m Model) Model {
	m.deps.NewSessionID = func() string { return testSessionID }
	return m
}

func TestEnterLaunchesNewConversationWithExplicitSessionID(t *testing.T) {
	m, r := newLoaded(t)
	m = withNewSessionID(m)

	_, cmd := press(t, m, "enter")
	drain(cmd)

	want := []string{"/bin/claude-fake", "main", "--session-id", testSessionID}
	if got := lastExecArgs(r); !reflect.DeepEqual(got, want) {
		t.Fatalf("exec=%q", got)
	}
}

func TestBestLaunchPassesExplicitSessionID(t *testing.T) {
	m, r := newLoaded(t)
	m = withNewSessionID(m)

	_, cmd := press(t, m, "b")
	drain(cmd)

	if got := lastExecArgs(r); !reflect.DeepEqual(got[2:], []string{"--session-id", testSessionID}) {
		t.Fatalf("exec=%q", got)
	}
}

func TestUserChosenSessionArgsSuppressExplicitSessionID(t *testing.T) {
	for _, userArg := range []string{"--resume", "--continue", "-c", "-r", "--session-id", "--resume=abc"} {
		m, r := newLoaded(t)
		m = withNewSessionID(m)
		m.deps.ArgsFor = func(string) ([]string, error) { return []string{userArg}, nil }

		_, cmd := press(t, m, "enter")
		drain(cmd)

		if got := lastExecArgs(r); !reflect.DeepEqual(got, []string{"/bin/claude-fake", "main"}) {
			t.Fatalf("%s: exec=%q", userArg, got)
		}
	}
}

func TestHandoffResumesExactlyTheLaunchedSession(t *testing.T) {
	m, r := newLoaded(t)
	m = withNewSessionID(m)
	m.deps.LatestSession = func() (string, error) { return "newest-by-mtime-wrong", nil }
	m = exitClaude(t, m, r, limitedMainSnapshot())

	_, cmd := press(t, m, "y")
	drain(cmd)

	want := []string{"/bin/claude-fake", "work", "--resume", testSessionID}
	if got := lastExecArgs(r); !reflect.DeepEqual(got, want) {
		t.Fatalf("exec=%q", got)
	}
}

func TestContinueToastsAndDoesNotLaunchWhenLatestChatIsLive(t *testing.T) {
	m, r := newLoaded(t)
	live := &handoff.LiveError{ID: "busy-1"}
	m.deps.LatestSession = func() (string, error) { return "", live }
	m = exitClaude(t, m, r, limitedMainSnapshot())

	m, cmd := press(t, m, "y")

	if cmd != nil || len(r.launched) != 1 || m.toast != live.Error() {
		t.Fatalf("launched=%v toast=%q", r.launched, m.toast)
	}
}

func TestContinueOtherLookupErrorsKeepPrefix(t *testing.T) {
	m, r := newLoaded(t)
	m.deps.LatestSession = func() (string, error) { return "", errors.New("boom") }
	m = exitClaude(t, m, r, limitedMainSnapshot())

	m, _ = press(t, m, "y")

	if !strings.HasPrefix(m.toast, "cannot continue: ") {
		t.Fatalf("toast=%q", m.toast)
	}
}

package dash

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestInitCollectsAndSchedulesTick(t *testing.T) {
	r := &recorder{snap: twoAccountSnapshot()}
	m := New(context.Background(), newTestDeps(r))

	msgs := drain(m.Init())

	if _, ok := hasMsg[snapshotMsg](msgs); !ok || r.count(&r.collects) != 1 {
		t.Fatalf("collects=%d msgs=%#v", r.collects, msgs)
	}
	if _, ok := hasMsg[tickFired](msgs); !ok {
		t.Fatalf("no tick scheduled: %#v", msgs)
	}
}

func TestRefreshTickTriggersCollectAndReschedules(t *testing.T) {
	m, r := newLoaded(t)

	next, cmd := m.Update(tickMsg{})
	msgs := drain(cmd)

	if r.count(&r.collects) != 1 {
		t.Fatalf("collects=%d", r.collects)
	}
	if _, ok := hasMsg[tickFired](msgs); !ok {
		t.Fatal("tick not rescheduled")
	}
	if !next.(Model).loading {
		t.Fatal("model should be loading")
	}
}

func TestTickWhileLoadingSkipsCollect(t *testing.T) {
	m, r := newLoaded(t)
	m, _ = press(t, m, "r")

	_, cmd := m.Update(tickMsg{})
	drain(cmd)

	if r.count(&r.collects) != 0 {
		t.Fatalf("collects=%d", r.collects)
	}
}

func TestSelectionMovesAndClamps(t *testing.T) {
	m, _ := newLoaded(t)
	cases := []struct {
		key  string
		want int
	}{{"down", 1}, {"j", 1}, {"up", 0}, {"k", 0}, {"j", 1}}
	for _, tc := range cases {
		m, _ = press(t, m, tc.key)
		if m.selected != tc.want {
			t.Fatalf("after %s selected=%d want %d", tc.key, m.selected, tc.want)
		}
	}
}

func TestSelectionKeptByNameAcrossRefresh(t *testing.T) {
	m, r := newLoaded(t)
	m, _ = press(t, m, "j")
	snap := r.snap
	snap.Rows = append(snap.Rows[:0:0], snap.Rows[1], snap.Rows[0])

	m = update(t, m, snapshotMsg{snap: snap, at: testNow})

	if m.selectedName() != "work" {
		t.Fatalf("selected %q", m.selectedName())
	}
}

func TestEnterExecsLaunchAndRefreshesAfterExit(t *testing.T) {
	m, r := newLoaded(t)
	m, _ = press(t, m, "j")

	m, cmd := press(t, m, "enter")
	msgs := drain(cmd)

	if !reflect.DeepEqual(r.launched, []string{"work"}) || len(r.execs) != 1 {
		t.Fatalf("launched=%v execs=%d", r.launched, len(r.execs))
	}
	done, ok := hasMsg[execDoneMsg](msgs)
	if !ok {
		t.Fatalf("msgs=%#v", msgs)
	}
	_, cmd = m.Update(done)
	if _, ok := hasMsg[snapshotMsg](drain(cmd)); !ok {
		t.Fatal("no refresh after claude exited")
	}
}

func TestExecErrorShowsToast(t *testing.T) {
	m, _ := newLoaded(t)

	m = update(t, m, execDoneMsg{label: "claude (work)", err: errors.New("exit status 3")})

	if !strings.Contains(m.toast, "exit status 3") {
		t.Fatalf("toast=%q", m.toast)
	}
}

func TestBestLaunchesAccountWithMostHeadroom(t *testing.T) {
	m, r := newLoaded(t)

	_, cmd := press(t, m, "b")
	drain(cmd)

	if !reflect.DeepEqual(r.launched, []string{"work"}) {
		t.Fatalf("launched=%v", r.launched)
	}
}

func TestBestWithNoUsableAccountToasts(t *testing.T) {
	m, r := newLoaded(t)
	m.snap.Rows = nil

	m, cmd := press(t, m, "b")
	drain(cmd)

	if len(r.launched) != 0 || !strings.Contains(m.toast, "no usable account") {
		t.Fatalf("launched=%v toast=%q", r.launched, m.toast)
	}
}

func TestNewWindowRunsOsascript(t *testing.T) {
	m, r := newLoaded(t)
	m, _ = press(t, m, "j")

	_, cmd := press(t, m, "n")
	msgs := drain(cmd)

	want := []string{"/usr/bin/osascript", "-e", `tell application "Terminal" to do script "'/opt/ccm' 'work'"`}
	if len(r.runs) != 1 || !reflect.DeepEqual(r.runs[0], want) {
		t.Fatalf("runs=%q", r.runs)
	}
	if _, ok := hasMsg[toastMsg](msgs); !ok {
		t.Fatalf("msgs=%#v", msgs)
	}
}

func TestLoginExecsSelfLogin(t *testing.T) {
	m, r := newLoaded(t)
	m, _ = press(t, m, "j")

	_, cmd := press(t, m, "l")
	drain(cmd)

	if len(r.execs) != 1 || !reflect.DeepEqual(r.execs[0].Args, []string{"/opt/ccm", "login", "work"}) {
		t.Fatalf("execs=%v", r.execs)
	}
}

func TestAddPromptsNameThenEmailThenExecs(t *testing.T) {
	m, r := newLoaded(t)
	m, _ = press(t, m, "a")
	if m.mode != modeAddName {
		t.Fatalf("mode=%v", m.mode)
	}
	m, _ = press(t, m, "side")
	m, _ = press(t, m, "enter")
	if m.mode != modeAddEmail {
		t.Fatalf("mode=%v toast=%q", m.mode, m.toast)
	}
	m, _ = press(t, m, "s@x.com")

	m, cmd := press(t, m, "enter")
	drain(cmd)

	want := []string{"/opt/ccm", "add", "side", "--email", "s@x.com"}
	if len(r.execs) != 1 || !reflect.DeepEqual(r.execs[0].Args, want) || m.mode != modeList {
		t.Fatalf("execs=%v mode=%v", r.execs, m.mode)
	}
}

func TestAddRejectsInvalidName(t *testing.T) {
	m, _ := newLoaded(t)
	m, _ = press(t, m, "a")
	m, _ = press(t, m, "Bad Name")

	m, _ = press(t, m, "enter")

	if m.mode != modeAddName || m.toast == "" {
		t.Fatalf("mode=%v toast=%q", m.mode, m.toast)
	}
}

func TestAddEscCancels(t *testing.T) {
	m, r := newLoaded(t)
	m, _ = press(t, m, "a")
	m, _ = press(t, m, "q")

	m, _ = press(t, m, "esc")

	if m.mode != modeList || len(r.execs) != 0 {
		t.Fatalf("mode=%v", m.mode)
	}
}

func TestDoctorShowsSummaryPaneAndEscCloses(t *testing.T) {
	m, r := newLoaded(t)

	m, cmd := press(t, m, "d")
	m = feed(t, m, drain(cmd))

	if r.count(&r.doctors) != 1 || m.mode != modeDoctor {
		t.Fatalf("doctors=%d mode=%v", r.doctors, m.mode)
	}
	if !strings.Contains(m.View(), "logged out → ccm login work") {
		t.Fatalf("view:\n%s", m.View())
	}
	m, _ = press(t, m, "esc")
	if m.mode != modeList {
		t.Fatalf("mode=%v", m.mode)
	}
}

func TestSyncAndT3SyncToastResult(t *testing.T) {
	cases := map[string]string{"s": "work: up to date", "t": "T3 Code already up to date."}
	for k, want := range cases {
		m, _ := newLoaded(t)
		m, cmd := press(t, m, k)
		m = feed(t, m, drain(cmd))
		if !strings.Contains(m.toast, want) {
			t.Errorf("%s: toast=%q", k, m.toast)
		}
	}
}

func TestRefreshKeyCollects(t *testing.T) {
	m, r := newLoaded(t)

	_, cmd := press(t, m, "r")
	drain(cmd)

	if r.count(&r.collects) != 1 {
		t.Fatalf("collects=%d", r.collects)
	}
}

func TestHelpToggles(t *testing.T) {
	m, _ := newLoaded(t)
	m, _ = press(t, m, "?")
	if m.mode != modeHelp || !strings.Contains(m.View(), "Keys") {
		t.Fatalf("mode=%v", m.mode)
	}
	m, _ = press(t, m, "?")
	if m.mode != modeList {
		t.Fatalf("mode=%v", m.mode)
	}
}

func TestQuitKeys(t *testing.T) {
	for _, k := range []string{"q", "ctrl+c"} {
		m, _ := newLoaded(t)
		_, cmd := press(t, m, k)
		if _, ok := hasMsg[tea.QuitMsg](drain(cmd)); !ok {
			t.Errorf("%s did not quit", k)
		}
	}
}

func TestProgramQuitsCleanly(t *testing.T) {
	r := &recorder{snap: twoAccountSnapshot()}
	p := tea.NewProgram(New(context.Background(), newTestDeps(r)),
		tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() {
		_, err := p.Run()
		done <- err
	}()
	p.Send(key("q"))
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		p.Kill()
		t.Fatal("program did not quit")
	}
}

func TestLaunchErrorToasts(t *testing.T) {
	m, r := newLoaded(t)
	m.deps.Launch = func(string, []string) (*exec.Cmd, string, error) { return nil, "", errors.New("unknown account") }

	m, cmd := press(t, m, "enter")

	if cmd != nil || len(r.execs) != 0 || !strings.Contains(m.toast, "unknown account") {
		t.Fatalf("toast=%q", m.toast)
	}
}

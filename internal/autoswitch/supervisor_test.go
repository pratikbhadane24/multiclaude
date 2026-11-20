package autoswitch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"multiclaude/internal/registry"
)

// fakeProc exits with code when Exit is called or when it receives exitOn.
type fakeProc struct {
	mu      sync.Mutex
	exitOn  syscall.Signal
	signals []syscall.Signal
	done    chan int
	once    sync.Once
}

func newFakeProc(exitOn syscall.Signal) *fakeProc {
	return &fakeProc{exitOn: exitOn, done: make(chan int, 1)}
}

func (p *fakeProc) Exit(code int) { p.once.Do(func() { p.done <- code }) }

func (p *fakeProc) Wait() int { return <-p.done }

func (p *fakeProc) Signal(sig syscall.Signal) error {
	p.mu.Lock()
	p.signals = append(p.signals, sig)
	p.mu.Unlock()
	if sig == p.exitOn || sig == syscall.SIGKILL {
		p.Exit(128 + int(sig))
	}
	return nil
}

func (p *fakeProc) sent() []syscall.Signal {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]syscall.Signal(nil), p.signals...)
}

// step scripts one claude run: it hits a limit, or exits with code.
type step struct {
	limit  bool
	code   int
	exitOn syscall.Signal
}

type launchRec struct {
	acct string
	args []string
}

// harness wires fakes for every Deps field.
type harness struct {
	steps    []step
	procs    []*fakeProc
	launches []launchRec
	marked   []string
	waits    []time.Duration
	now      time.Time
	pick     func(exclude []string) (registry.Account, bool)
	reset    func() (registry.Account, time.Time, bool)
	startErr error
	err      bytes.Buffer
	mu       sync.Mutex
}

func (h *harness) deps() Deps {
	return Deps{
		Start:        h.start,
		RelaunchArgs: func(a registry.Account, id, prompt string) []string { return []string{"--resume", id, prompt} },
		Transcript:   func(a registry.Account) string { return "/t/" + a.Name },
		Tail:         h.tail,
		Pick: func(ex []string, _ time.Time) (registry.Account, bool) {
			return h.pick(append([]string(nil), ex...))
		},
		NextReset: func(time.Time) (registry.Account, time.Time, bool) { return h.reset() },
		Mark: func(a registry.Account, _ Limit, _ time.Time) error {
			h.marked = append(h.marked, a.Name)
			return nil
		},
		Now:   func() time.Time { h.mu.Lock(); defer h.mu.Unlock(); return h.now },
		After: h.after,
		Err:   &h.err,
	}
}

func (h *harness) start(a registry.Account, args []string) (Proc, error) {
	if h.startErr != nil {
		return nil, h.startErr
	}
	i := len(h.launches)
	h.launches = append(h.launches, launchRec{a.Name, args})
	st := h.steps[i]
	p := newFakeProc(st.exitOn)
	h.procs = append(h.procs, p)
	if !st.limit {
		p.Exit(st.code)
	}
	return p, nil
}

// tail emits one limit when the run about to start is scripted to hit one.
func (h *harness) tail(string) Follower {
	i := len(h.launches)
	return func(ctx context.Context, onLimit func(Limit)) {
		if i < len(h.steps) && h.steps[i].limit {
			onLimit(Limit{Text: "You've hit your 5-hour limit"})
		}
		<-ctx.Done()
	}
}

// graceDelay is the real delay of a fake signal-grace timer, so a process
// that exits on the signal always wins the race against it.
const graceDelay = 50 * time.Millisecond

// after records d. Signal graces fire after graceDelay without moving the
// fake clock; longer waits fire at once and advance the clock by d.
func (h *harness) after(d time.Duration) <-chan time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.waits = append(h.waits, d)
	if d <= termGrace {
		return time.After(graceDelay)
	}
	h.now = h.now.Add(d)
	ch := make(chan time.Time, 1)
	ch <- h.now
	return ch
}

func acct(name string) registry.Account { return registry.Account{Name: name} }

// pickFrom returns the first of names not excluded.
func pickFrom(names ...string) func([]string) (registry.Account, bool) {
	return func(ex []string) (registry.Account, bool) {
		for _, n := range names {
			if !strings.Contains(" "+strings.Join(ex, " ")+" ", " "+n+" ") {
				return acct(n), true
			}
		}
		return registry.Account{}, false
	}
}

func noReset() (registry.Account, time.Time, bool) { return registry.Account{}, time.Time{}, false }

const sid = "11111111-2222-4333-8444-555555555555"

func TestRunNormalExitDoesNotSwitch(t *testing.T) {
	// Arrange
	h := &harness{steps: []step{{code: 3}}, pick: pickFrom("b"), reset: noReset}

	// Act
	code := Run(context.Background(), h.deps(), acct("a"), []string{"--session-id", sid}, sid)

	// Assert
	if code != 3 || len(h.launches) != 1 || len(h.marked) != 0 || h.err.Len() != 0 {
		t.Fatalf("code=%d launches=%+v marked=%v err=%q", code, h.launches, h.marked, h.err.String())
	}
}

func TestRunSwitchesOnLimitAndResumes(t *testing.T) {
	h := &harness{steps: []step{{limit: true, exitOn: syscall.SIGINT}, {code: 0}}, pick: pickFrom("a", "b"), reset: noReset}

	code := Run(context.Background(), h.deps(), acct("a"), []string{"--session-id", sid}, sid)

	if code != 0 || len(h.launches) != 2 || h.launches[1].acct != "b" {
		t.Fatalf("code=%d launches=%+v", code, h.launches)
	}
	want := BuildResumePrompt("a", "b", false, "")
	if args := h.launches[1].args; args[0] != "--resume" || args[1] != sid || args[2] != want {
		t.Fatalf("relaunch args %q", args)
	}
	if got := h.procs[0].sent(); len(got) != 1 || got[0] != syscall.SIGINT {
		t.Fatalf("signals %v", got)
	}
	if h.marked[0] != "a" || !strings.Contains(h.err.String(), "ccm: a hit its limit — continuing on b\n") {
		t.Fatalf("marked=%v err=%q", h.marked, h.err.String())
	}
}

func TestRunEscalatesSignals(t *testing.T) {
	h := &harness{steps: []step{{limit: true, exitOn: syscall.Signal(0)}, {code: 0}}, pick: pickFrom("b"), reset: noReset}

	Run(context.Background(), h.deps(), acct("a"), nil, sid)

	got := h.procs[0].sent()
	want := []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("signals %v", got)
	}
	if fmt.Sprint(h.waits[:2]) != fmt.Sprint([]time.Duration{interruptGrace, termGrace}) {
		t.Fatalf("waits %v", h.waits)
	}
}

func TestRunChainsAndKeepsSwitchesApart(t *testing.T) {
	h := &harness{
		steps: []step{{limit: true, exitOn: syscall.SIGINT}, {limit: true, exitOn: syscall.SIGINT}, {code: 0}},
		pick:  pickFrom("a", "b", "c"), reset: noReset,
	}

	Run(context.Background(), h.deps(), acct("a"), nil, sid)

	if len(h.launches) != 3 || h.launches[2].acct != "c" {
		t.Fatalf("launches %+v", h.launches)
	}
	// After the first switch the clock advanced only by the 0s gap; the
	// second switch must first wait out MinSwitchGap.
	if h.waits[len(h.waits)-1] != MinSwitchGap {
		t.Fatalf("waits %v", h.waits)
	}
}

func TestRunStopsAfterMaxSwitches(t *testing.T) {
	steps := make([]step, MaxSwitches+1)
	for i := range steps {
		steps[i] = step{limit: true, exitOn: syscall.SIGINT}
	}
	n := 0
	h := &harness{steps: steps, reset: noReset, pick: func([]string) (registry.Account, bool) {
		n++
		return acct(fmt.Sprintf("x%d", n)), true
	}}

	code := Run(context.Background(), h.deps(), acct("a"), nil, sid)

	if len(h.launches) != MaxSwitches+1 || code != 128+int(syscall.SIGINT) {
		t.Fatalf("launches=%d code=%d", len(h.launches), code)
	}
	if !strings.Contains(h.err.String(), "auto-switch stopped after 10 switches") {
		t.Fatalf("err=%q", h.err.String())
	}
}

func TestRunAllLimitedWaitsForResetThenResumes(t *testing.T) {
	h := &harness{steps: []step{{limit: true, exitOn: syscall.SIGINT}, {code: 0}}, pick: pickFrom()}
	h.now = time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	resetAt := h.now.Add(2 * time.Hour)
	h.reset = func() (registry.Account, time.Time, bool) { return acct("b"), resetAt, true }

	code := Run(context.Background(), h.deps(), acct("a"), nil, sid)

	if code != 0 || len(h.launches) != 2 || h.launches[1].acct != "b" {
		t.Fatalf("code=%d launches=%+v", code, h.launches)
	}
	if h.waits[len(h.waits)-1] != 2*time.Hour+ResetSlack {
		t.Fatalf("waits %v", h.waits)
	}
	if h.launches[1].args[2] != ResetResumePrompt {
		t.Fatalf("prompt %q", h.launches[1].args[2])
	}
	if !strings.Contains(h.err.String(), "ccm: all accounts are limited — next reset ") {
		t.Fatalf("err=%q", h.err.String())
	}
}

func TestRunAllLimitedUnknownResetGivesUp(t *testing.T) {
	h := &harness{steps: []step{{limit: true, exitOn: syscall.SIGINT}}, pick: pickFrom(), reset: noReset}

	code := Run(context.Background(), h.deps(), acct("a"), nil, sid)

	if code != 128+int(syscall.SIGINT) || !strings.Contains(h.err.String(), "next reset unknown") {
		t.Fatalf("code=%d err=%q", code, h.err.String())
	}
}

func TestRunWaitCancelledExits130(t *testing.T) {
	h := &harness{steps: []step{{limit: true, exitOn: syscall.SIGINT}}, pick: pickFrom()}
	h.reset = func() (registry.Account, time.Time, bool) { return acct("a"), h.now.Add(time.Hour), true }
	d := h.deps()
	d.After = func(d time.Duration) <-chan time.Time {
		if d > termGrace {
			return make(chan time.Time) // never fires: only cancel ends the wait
		}
		return h.after(d)
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.Mark = func(registry.Account, Limit, time.Time) error { cancel(); return nil }

	code := Run(ctx, d, acct("a"), nil, sid)

	if code != exitInterrupted || len(h.launches) != 1 {
		t.Fatalf("code=%d launches=%d", code, len(h.launches))
	}
}

func TestRunPromptOverride(t *testing.T) {
	h := &harness{steps: []step{{limit: true, exitOn: syscall.SIGINT}, {code: 0}}, pick: pickFrom("b"), reset: noReset}
	d := h.deps()
	d.PromptOverride = "carry on"

	Run(context.Background(), d, acct("a"), nil, sid)

	if h.launches[1].args[2] != "carry on" {
		t.Fatalf("args %q", h.launches[1].args)
	}
}

func TestRunStartErrorReported(t *testing.T) {
	h := &harness{startErr: errors.New("boom"), pick: pickFrom(), reset: noReset}
	if code := Run(context.Background(), h.deps(), acct("a"), nil, sid); code != exitError || !strings.Contains(h.err.String(), "boom") {
		t.Fatalf("code=%d err=%q", code, h.err.String())
	}
}

func TestRunMarkErrorIsWarningOnly(t *testing.T) {
	h := &harness{steps: []step{{limit: true, exitOn: syscall.SIGINT}, {code: 0}}, pick: pickFrom("b"), reset: noReset}
	d := h.deps()
	d.Mark = func(registry.Account, Limit, time.Time) error { return errors.New("locked") }

	if code := Run(context.Background(), d, acct("a"), nil, sid); code != 0 || !strings.Contains(h.err.String(), "warning") {
		t.Fatalf("code=%d err=%q", code, h.err.String())
	}
}

func TestTranscriptPath(t *testing.T) {
	if got := TranscriptPath("/h/.claude", "/Users/me/my proj", "abc"); got != "/h/.claude/projects/-Users-me-my-proj/abc.jsonl" {
		t.Fatalf("got %s", got)
	}
}

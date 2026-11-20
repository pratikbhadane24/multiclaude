package autoswitch

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"syscall"
	"time"

	"multiclaude/internal/handoff"
	"multiclaude/internal/paths"
	"multiclaude/internal/registry"
	"multiclaude/internal/status"
)

const (
	// MaxSwitches bounds the account switches of one chain.
	MaxSwitches = 10
	// MinSwitchGap is the least time between two switches.
	MinSwitchGap = 30 * time.Second
	// interruptGrace is how long claude gets after SIGINT before SIGTERM.
	interruptGrace = 2 * time.Second
	// termGrace is how long claude gets after SIGTERM before SIGKILL.
	termGrace = 5 * time.Second
	// ResetSlack is added to a reset time before relaunching on it.
	ResetSlack = 60 * time.Second
	// exitError and exitInterrupted mirror the cli exit codes.
	exitError       = 1
	exitInterrupted = 130
	resetTimeLayout = "Jan 2 15:04"
)

// Proc is a running claude.
type Proc interface {
	// Wait blocks until the process exits (terminal restored) and returns its
	// exit code.
	Wait() int
	// Signal sends sig to the process group.
	Signal(sig syscall.Signal) error
}

// Follower tails a transcript until ctx ends, calling onLimit per limit hit.
type Follower func(ctx context.Context, onLimit func(Limit))

// Deps are the supervisor's collaborators; tests replace every one.
type Deps struct {
	// Start launches claude as acct with the full args.
	Start func(acct registry.Account, args []string) (Proc, error)
	// RelaunchArgs is the full args to resume session id on acct with prompt.
	RelaunchArgs func(acct registry.Account, id, prompt string) []string
	// PromptOverride replaces the built-in resume prompts when non-empty.
	PromptOverride string
	// Transcript is the path of the session transcript seen by acct.
	Transcript func(acct registry.Account) string
	// Tail starts following path from its current end (synchronously).
	Tail func(path string) Follower
	Pick func(exclude []string, now time.Time) (registry.Account, bool)
	// NextReset is the account usable soonest and when.
	NextReset func(now time.Time) (registry.Account, time.Time, bool)
	// Mark records that acct hit l.
	Mark  func(acct registry.Account, l Limit, now time.Time) error
	Now   func() time.Time
	After func(d time.Duration) <-chan time.Time
	Err   io.Writer
}

// NewDeps returns Deps backed by the usage cache in p and real time; the
// caller sets Start, RelaunchArgs, Transcript and Err.
func NewDeps(p paths.Paths) Deps {
	return Deps{
		Tail: func(path string) Follower {
			t := NewTail(path)
			return func(ctx context.Context, onLimit func(Limit)) { t.Follow(ctx, DefaultPoll, onLimit) }
		},
		Pick:      func(ex []string, now time.Time) (registry.Account, bool) { return PickNext(p, ex, now) },
		NextReset: func(now time.Time) (registry.Account, time.Time, bool) { return NextReset(p, now) },
		Mark:      func(a registry.Account, l Limit, now time.Time) error { return MarkLimit(p, a.Name, l, now) },
		Now:       time.Now,
		After:     time.After,
		Err:       io.Discard,
	}
}

// MarkLimit records in the usage cache that name hit l (reset time parsed
// from the message when possible).
func MarkLimit(p paths.Paths, name string, l Limit, now time.Time) error {
	m := status.LimitMark{Name: name, Weekly: l.Weekly}
	if at, ok := ParseReset(l.Reset, now); ok && l.Reset != "" {
		m.ResetsAt = &at
	}
	return status.MarkLimited(p, m)
}

// TranscriptPath is <configDir>/projects/<slug(cwd)>/<id>.jsonl.
func TranscriptPath(configDir, cwd, id string) string {
	return filepath.Join(configDir, "projects", handoff.Slug(cwd), id+".jsonl")
}

// supervisor runs one chain of claude launches for one session.
type supervisor struct {
	d          Deps
	id         string
	chain      []string
	switches   int
	lastSwitch time.Time
}

// outcome is how one claude run ended.
type outcome struct {
	code  int
	limit Limit
	hit   bool
}

// Run launches claude as first with args (which must pin session id) and,
// whenever the transcript shows a usage-limit hit, stops claude and resumes
// the session on the next account, waiting for the soonest reset when every
// account is limited. It returns the exit code of the last claude (130 when
// a wait is interrupted through ctx). A claude that exits on its own ends it.
func Run(ctx context.Context, d Deps, first registry.Account, args []string, id string) int {
	s := &supervisor{d: d, id: id, chain: []string{first.Name}}
	acct := first
	for {
		out, err := s.runOnce(acct, args)
		if err != nil {
			fmt.Fprintf(d.Err, "ccm: run claude as %s: %v\n", acct.Name, err)
			return exitError
		}
		if !out.hit {
			return out.code
		}
		next, prompt, code, ok := s.afterLimit(ctx, acct, out)
		if !ok {
			return code
		}
		acct, args = next, d.RelaunchArgs(next, id, prompt)
	}
}

// runOnce runs claude once while tailing its transcript.
func (s *supervisor) runOnce(acct registry.Account, args []string) (outcome, error) {
	follow := s.d.Tail(s.d.Transcript(acct))
	proc, err := s.d.Start(acct, args)
	if err != nil {
		return outcome{}, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	limits := make(chan Limit, 1)
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		follow(ctx, func(l Limit) {
			select {
			case limits <- l:
			default:
			}
		})
	}()
	defer func() { cancel(); <-watchDone }()
	exited := make(chan int, 1)
	go func() { exited <- proc.Wait() }()
	select {
	case code := <-exited:
		return outcome{code: code}, nil
	case l := <-limits:
		return outcome{code: s.stop(proc, exited), limit: l, hit: true}, nil
	}
}

// stop ends claude: SIGINT, then SIGTERM, then SIGKILL, each after a grace.
func (s *supervisor) stop(p Proc, exited <-chan int) int {
	steps := []struct {
		sig  syscall.Signal
		wait time.Duration
	}{{syscall.SIGINT, interruptGrace}, {syscall.SIGTERM, termGrace}}
	for _, st := range steps {
		_ = p.Signal(st.sig) // an exited process is fine: Wait reports it
		select {
		case code := <-exited:
			return code
		case <-s.d.After(st.wait):
		}
	}
	_ = p.Signal(syscall.SIGKILL)
	return <-exited
}

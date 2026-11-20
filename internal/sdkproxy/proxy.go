// Package sdkproxy runs claude as a supervised child for SDK (stream-json)
// sessions, such as T3 Code, and moves the session to another ccm account
// when the current one hits its usage limit — without the SDK noticing
// anything but a slow turn.
//
// Traffic is proxied line by line, verbatim. The proxy remembers the SDK's
// state-setting control requests (initialize, set_model, ...) and the
// in-flight user message. On a limit it marks the account limited in the
// shared usage cache, picks the freest account from that cache, terminates
// the child and starts a new one as the chosen account with
// "--resume <session id>", replays the remembered requests (swallowing the
// new child's answers to them) and the in-flight message, so the turn reruns.
package sdkproxy

import (
	"fmt"
	"io"
	"os"
	"syscall"
	"time"

	"multiclaude/internal/paths"
	"multiclaude/internal/registry"
	"multiclaude/internal/status"
)

const (
	claudeArgv0     = "claude"
	logPrefix       = "ccm: "
	maxSwitches     = 10
	minSwitchGap    = 30 * time.Second
	killDelay       = 5 * time.Second
	pollInterval    = time.Minute
	exitCannotStart = 127
	// exitAbandoned: the SDK closed stdin while a turn waited for a free account.
	exitAbandoned  = 1
	signalExitBase = 128
)

// Deps are the proxy's side effects, injectable for tests.
type Deps struct {
	Paths       paths.Paths
	Start       Starter
	Clock       Clock
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
	Signals     <-chan os.Signal
	Accounts    func() ([]registry.Account, error)
	ReadCache   func() (status.Cache, error)
	MarkLimited func(account string, l Limit) error
	// ResumePrompt, when set, replaces the built-in resume prompt verbatim.
	ResumePrompt string
	// Prepare readies a non-main account directory (MCP sync); may be nil.
	Prepare func(dir string)
}

type phase int

const (
	phaseRunning  phase = iota // a child is serving the session
	phaseStopping              // the limited child is being terminated
	phaseWaiting               // no child: waiting for an account to free up
)

// session is the proxy state. It is owned by the loop goroutine; other
// goroutines only send events.
type session struct {
	d       Deps
	args    []string
	env     []string // env of the next child
	baseEnv []string

	acct      registry.Account
	acctKnown bool

	events chan event
	quit   chan struct{}
	timer  <-chan time.Time
	phase  phase
	gen    int
	child  *running

	sessionID    string
	inFlight     []byte // the SDK user line whose turn has not finished
	lastUser     []byte // the latest SDK user line (shape for the resume prompt)
	replay       map[string][]byte
	pending      [][]byte
	swallow      map[string]bool
	suppressInit bool
	swallowEcho  int
	stdinClosed  bool

	armed       bool
	armInfo     rateLimitInfo
	held        [][]byte
	avoid       map[string]time.Time
	switches    int
	lastSwitch  time.Time
	gaveUp      bool
	waitLogged  bool
	limitedFrom string
}

// Run proxies one SDK session through supervised claude children and
// returns the exit code to leave with. args excludes the program name.
func Run(args, env []string, d Deps) int {
	s := &session{
		d: d, args: args, env: env, baseEnv: env,
		events: make(chan event), quit: make(chan struct{}),
		replay: map[string][]byte{}, swallow: map[string]bool{}, avoid: map[string]time.Time{},
	}
	defer close(s.quit)
	s.chooseStart()
	go readLines(d.Stdin, func(l []byte) { s.send(evIn{line: l}) }, func() { s.send(evIn{eof: true}) })
	if code, done := s.start(false, ""); done {
		return code
	}
	return s.loop()
}

func (s *session) loop() int {
	for {
		var code int
		var done bool
		select {
		case e := <-s.events:
			code, done = s.handle(e)
		case <-s.timer:
			s.timer = nil
			code, done = s.onTimer()
		case sig := <-s.d.Signals:
			code, done = s.onSignal(sig)
		}
		if done {
			return code
		}
	}
}

// send delivers e to the loop unless the loop has returned.
func (s *session) send(e event) {
	select {
	case s.events <- e:
	case <-s.quit:
	}
}

func (s *session) handle(e event) (int, bool) {
	switch ev := e.(type) {
	case evIn:
		if ev.eof {
			return s.onStdinEOF()
		}
		s.onStdinLine(ev.line)
	case evOut:
		if ev.gen == s.gen {
			s.onChildLine(ev.line)
		}
	case evExit:
		if ev.gen == s.gen {
			return s.onChildExit(ev.code)
		}
	}
	return 0, false
}

func (s *session) onSignal(sig os.Signal) (int, bool) {
	if s.phase == phaseRunning {
		_ = s.child.child.Signal(sig)
		return 0, false
	}
	if s.child != nil {
		_ = s.child.child.Signal(syscall.SIGKILL)
	}
	if n, ok := sig.(syscall.Signal); ok {
		return signalExitBase + int(n), true
	}
	return signalExitBase, true
}

func (s *session) logf(format string, a ...any) {
	if s.d.Stderr != nil {
		_, _ = fmt.Fprintf(s.d.Stderr, logPrefix+format+"\n", a...)
	}
}

// emit writes one child line to the SDK, verbatim.
func (s *session) emit(line []byte) {
	if _, err := s.d.Stdout.Write(line); err != nil {
		s.logf("cannot write to SDK: %v", err)
	}
}

// accountName is the current account's display name.
func (s *session) accountName() string {
	if s.acctKnown {
		return s.acct.Name
	}
	return s.acct.Dir
}

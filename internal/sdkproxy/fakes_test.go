package sdkproxy

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"multiclaude/internal/paths"
	"multiclaude/internal/registry"
	"multiclaude/internal/status"
)

const waitLimit = 3 * time.Second

var t0 = time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)

// fakeChild is an in-memory claude: the test reads what the proxy wrote to
// its stdin and writes its stdout.
type fakeChild struct {
	argv0     string
	argv, env []string
	stdinR    *io.PipeReader
	stdinW    *io.PipeWriter
	outR      *io.PipeReader
	outW      *io.PipeWriter
	in        chan string
	exitCh    chan int
	once      sync.Once
	mu        sync.Mutex
	sigs      []os.Signal
}

func newFakeChild(argv0 string, argv, env []string) *fakeChild {
	c := &fakeChild{argv0: argv0, argv: argv, env: env, in: make(chan string, 64), exitCh: make(chan int, 1)}
	c.stdinR, c.stdinW = io.Pipe()
	c.outR, c.outW = io.Pipe()
	go readLines(c.stdinR, func(l []byte) { c.in <- string(l) }, func() { close(c.in) })
	return c
}

func (c *fakeChild) Stdin() io.WriteCloser { return c.stdinW }
func (c *fakeChild) Stdout() io.Reader     { return c.outR }
func (c *fakeChild) Wait() int             { return <-c.exitCh }

// Signal records sig; SIGTERM and SIGKILL end the fake process.
func (c *fakeChild) Signal(sig os.Signal) error {
	c.mu.Lock()
	c.sigs = append(c.sigs, sig)
	c.mu.Unlock()
	if sig == syscall.SIGTERM || sig == syscall.SIGKILL {
		c.exit(143)
	}
	return nil
}

func (c *fakeChild) signals() []os.Signal {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]os.Signal(nil), c.sigs...)
}

func (c *fakeChild) exit(code int) {
	c.once.Do(func() {
		_ = c.outW.Close()
		c.exitCh <- code
	})
}

// say writes one stdout line (newline added).
func (c *fakeChild) say(t *testing.T, line string) {
	t.Helper()
	// A child the proxy already terminated cannot write any more; like a
	// real process, its last words are simply lost.
	if _, err := c.outW.Write([]byte(line + "\n")); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("child write: %v", err)
	}
}

// read returns the next line the proxy wrote to the child's stdin.
func (c *fakeChild) read(t *testing.T) string {
	t.Helper()
	select {
	case l, ok := <-c.in:
		if !ok {
			t.Fatal("child stdin closed")
		}
		return l
	case <-time.After(waitLimit):
		t.Fatal("timeout reading child stdin")
	}
	return ""
}

func (c *fakeChild) envValue(key string) (string, bool) {
	for _, kv := range c.env {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			return v, true
		}
	}
	return "", false
}

// fakeClock fires After channels only when the test advances it.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []fakeTimer
}

type fakeTimer struct {
	at time.Time
	d  time.Duration
	ch chan time.Time
}

func (k *fakeClock) Now() time.Time {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.now
}

func (k *fakeClock) After(d time.Duration) <-chan time.Time {
	k.mu.Lock()
	defer k.mu.Unlock()
	ch := make(chan time.Time, 1)
	k.timers = append(k.timers, fakeTimer{at: k.now.Add(d), d: d, ch: ch})
	return ch
}

func (k *fakeClock) Advance(d time.Duration) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.now = k.now.Add(d)
	kept := k.timers[:0:0]
	for _, tm := range k.timers {
		if tm.at.After(k.now) {
			kept = append(kept, tm)
			continue
		}
		tm.ch <- k.now
	}
	k.timers = kept
}

// hasTimer reports a pending timer of exactly d.
func (k *fakeClock) hasTimer(d time.Duration) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, tm := range k.timers {
		if tm.d == d {
			return true
		}
	}
	return false
}

type mark struct {
	account string
	limit   Limit
}

// harness runs Run against fakes.
type harness struct {
	t        *testing.T
	sdkIn    *io.PipeWriter
	sdkOut   chan string
	children chan *fakeChild
	clock    *fakeClock
	signals  chan os.Signal
	done     chan int
	mu       sync.Mutex
	marks    []mark
	cache    status.Cache
	accts    []registry.Account
	stderr   *syncBuffer
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

var testPaths = paths.Paths{Home: "/home", ClaudeDir: "/home/.claude", AccountsRoot: "/home/.claude-accounts", RealClaude: "/bin/real-claude"}

var streamArgs = []string{"--output-format", "stream-json", "--verbose", "--input-format", "stream-json"}

func defaultAccounts() []registry.Account {
	return []registry.Account{{Name: "main"}, {Name: "work", Dir: "/acc/work"}, {Name: "alt", Dir: "/acc/alt"}}
}

func newHarness(t *testing.T) *harness {
	return &harness{t: t, sdkOut: make(chan string, 256), children: make(chan *fakeChild, 32),
		clock: &fakeClock{now: t0}, signals: make(chan os.Signal, 1), done: make(chan int, 1),
		accts: defaultAccounts(), stderr: &syncBuffer{}}
}

func (h *harness) deps(stdin io.Reader, stdout io.Writer) Deps {
	return Deps{
		Paths: testPaths, Clock: h.clock, Stdin: stdin, Stdout: stdout, Stderr: h.stderr, Signals: h.signals,
		Start: func(argv0 string, argv, env []string) (Child, error) {
			c := newFakeChild(argv0, argv, env)
			h.children <- c
			return c, nil
		},
		Accounts:  func() ([]registry.Account, error) { return h.accts, nil },
		ReadCache: func() (status.Cache, error) { h.mu.Lock(); defer h.mu.Unlock(); return h.cache, nil },
		MarkLimited: func(a string, l Limit) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.marks = append(h.marks, mark{a, l})
			return nil
		},
	}
}

// run starts the proxy with args and env, returning the first child.
func (h *harness) run(args, env []string, tweak func(*Deps)) *fakeChild {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	h.sdkIn = inW
	go func() {
		br := bufio.NewReader(outR)
		for {
			l, err := br.ReadString('\n')
			if l != "" {
				h.sdkOut <- l
			}
			if err != nil {
				return
			}
		}
	}()
	d := h.deps(inR, outW)
	if tweak != nil {
		tweak(&d)
	}
	go func() { h.done <- Run(args, env, d) }()
	return h.child()
}

func (h *harness) child() *fakeChild {
	h.t.Helper()
	select {
	case c := <-h.children:
		return c
	case <-time.After(waitLimit):
		h.t.Fatal("timeout waiting for a child")
	}
	return nil
}

func (h *harness) noChild() {
	h.t.Helper()
	select {
	case c := <-h.children:
		h.t.Fatalf("unexpected child %q", c.argv)
	case <-time.After(50 * time.Millisecond):
	}
}

// send writes one SDK stdin line (newline added).
func (h *harness) send(line string) {
	h.t.Helper()
	if _, err := h.sdkIn.Write([]byte(line + "\n")); err != nil {
		h.t.Fatalf("sdk write: %v", err)
	}
}

// out returns the next line the SDK received.
func (h *harness) out() string {
	h.t.Helper()
	select {
	case l := <-h.sdkOut:
		return l
	case <-time.After(waitLimit):
		h.t.Fatal("timeout reading SDK output")
	}
	return ""
}

func (h *harness) noOut() {
	h.t.Helper()
	select {
	case l := <-h.sdkOut:
		h.t.Fatalf("unexpected SDK output %q", l)
	case <-time.After(50 * time.Millisecond):
	}
}

func (h *harness) exitCode() int {
	h.t.Helper()
	select {
	case c := <-h.done:
		return c
	case <-time.After(waitLimit):
		h.t.Fatal("proxy did not exit")
	}
	return -1
}

func (h *harness) waitTimer(d time.Duration) {
	h.t.Helper()
	deadline := time.Now().Add(waitLimit)
	for !h.clock.hasTimer(d) {
		if time.Now().After(deadline) {
			h.t.Fatalf("no %v timer", d)
		}
		time.Sleep(time.Millisecond)
	}
}

func (h *harness) getMarks() []mark {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]mark(nil), h.marks...)
}

func (h *harness) setCache(c status.Cache) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cache = c
}

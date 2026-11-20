package sdkproxy

import (
	"bufio"
	"io"
	"sync"
	"time"
)

const (
	// drainTimeout bounds how long a child's exit waits for its stdout to
	// drain (a grandchild could hold the pipe open).
	drainTimeout = 2 * time.Second
)

type event any

// evIn is one SDK stdin line, or its end.
type evIn struct {
	line []byte
	eof  bool
}

// evOut is one line a child wrote; gen tells children apart.
type evOut struct {
	gen  int
	line []byte
}

// evExit is a child's exit, sent after its stdout is drained.
type evExit struct{ gen, code int }

// running is one started child with its stdin writer goroutine.
type running struct {
	child Child
	in    *lineQueue
	done  chan struct{} // closed once the process has exited
}

// lineQueue is an unbounded FIFO of lines for one child's stdin. push never
// blocks, so the loop keeps reading the child's stdout even while the child
// is not reading its stdin.
type lineQueue struct {
	mu     sync.Mutex
	lines  [][]byte
	closed bool
	wake   chan struct{}
}

func newLineQueue() *lineQueue { return &lineQueue{wake: make(chan struct{}, 1)} }

func (q *lineQueue) notify() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// push appends line unless the queue is closed.
func (q *lineQueue) push(line []byte) {
	q.mu.Lock()
	if !q.closed {
		q.lines = append(q.lines, line)
	}
	q.mu.Unlock()
	q.notify()
}

func (q *lineQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.notify()
}

// pop blocks for the next line; ok is false once the queue is closed and
// empty.
func (q *lineQueue) pop() ([]byte, bool) {
	for {
		q.mu.Lock()
		if len(q.lines) > 0 {
			l := q.lines[0]
			q.lines = q.lines[1:]
			q.mu.Unlock()
			return l, true
		}
		closed := q.closed
		q.mu.Unlock()
		if closed {
			return nil, false
		}
		<-q.wake
	}
}

// readLines calls line for every line of r (newline kept; a final partial
// line too), then end. Lines may be any length.
func readLines(r io.Reader, line func([]byte), end func()) {
	br := bufio.NewReader(r)
	for {
		l, err := br.ReadBytes('\n')
		if len(l) > 0 {
			line(l)
		}
		if err != nil {
			end()
			return
		}
	}
}

// attach starts the goroutines serving c as generation gen.
func (s *session) attach(c Child, gen int) *running {
	r := &running{child: c, in: newLineQueue(), done: make(chan struct{})}
	go writeLines(c.Stdin(), r.in)
	drained := make(chan struct{})
	go readLines(c.Stdout(), func(l []byte) { s.send(evOut{gen: gen, line: l}) }, func() { close(drained) })
	go func() {
		code := c.Wait()
		close(r.done)
		select {
		case <-drained:
		case <-time.After(drainTimeout):
		}
		s.send(evExit{gen: gen, code: code})
	}()
	return r
}

// writeLines writes queued lines to w until the queue closes, then closes w.
// After a write error it keeps draining so the queue never grows unread.
func writeLines(w io.WriteCloser, in *lineQueue) {
	failed := false
	for {
		l, ok := in.pop()
		if !ok {
			break
		}
		if !failed {
			_, err := w.Write(l)
			failed = err != nil
		}
	}
	_ = w.Close()
}

func (r *running) write(line []byte) { r.in.push(line) }

func (r *running) closeStdin() { r.in.close() }

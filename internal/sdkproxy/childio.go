package sdkproxy

import (
	"bufio"
	"io"
	"time"
)

const (
	writeQueue = 256
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
	child    Child
	in       chan []byte
	inClosed bool
	done     chan struct{} // closed once the process has exited
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
	r := &running{child: c, in: make(chan []byte, writeQueue), done: make(chan struct{})}
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
// After a write error it keeps draining so the loop never blocks.
func writeLines(w io.WriteCloser, in <-chan []byte) {
	failed := false
	for l := range in {
		if !failed {
			_, err := w.Write(l)
			failed = err != nil
		}
	}
	_ = w.Close()
}

func (r *running) write(line []byte) {
	if !r.inClosed {
		r.in <- line
	}
}

func (r *running) closeStdin() {
	if !r.inClosed {
		r.inClosed = true
		close(r.in)
	}
}

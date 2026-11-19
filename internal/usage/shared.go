package usage

import (
	"sync"
	"time"
)

// sharedReuseFor is how long a finished request is reused by other accounts
// of the same login: long enough to span one collection cycle.
const sharedReuseFor = 15 * time.Second

// SharedFetches makes accounts signed in as the same login (same account
// UUID) share one usage request: a fetch for a login already in flight, or
// finished within sharedReuseFor, reuses that response. Safe for concurrent use.
type SharedFetches struct {
	mu    sync.Mutex
	calls map[string]*sharedCall
}

type sharedCall struct {
	done chan struct{}
	resp response
	at   time.Time
}

// NewSharedFetches returns an empty set of shared requests.
func NewSharedFetches() *SharedFetches {
	return &SharedFetches{calls: map[string]*sharedCall{}}
}

// do returns the response for key, running fn only when no usable request
// for key is in flight or recently finished.
func (s *SharedFetches) do(key string, fn func() response) response {
	s.mu.Lock()
	if c, ok := s.calls[key]; ok && reusable(c) {
		s.mu.Unlock()
		<-c.done
		return c.resp
	}
	c := &sharedCall{done: make(chan struct{})}
	s.calls[key] = c
	s.mu.Unlock()

	resp := fn()
	c.resp, c.at = resp, time.Now()
	close(c.done) // publishes resp and at to every waiter
	return resp
}

// reusable reports whether c is in flight or finished recently.
func reusable(c *sharedCall) bool {
	select {
	case <-c.done:
		return time.Since(c.at) < sharedReuseFor
	default:
		return true
	}
}

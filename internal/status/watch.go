package status

import (
	"context"
	"sync"
	"time"

	"multiclaude/internal/registry"
	"multiclaude/internal/usage"
)

// ErrorRetryAfter is how long an errored account waits before being refetched.
const ErrorRetryAfter = 10 * time.Minute

type watchEntry struct {
	at  time.Time
	row Row
}

// Watcher drives watch mode: ok accounts are refetched on every Step, accounts
// in error only after ErrorRetryAfter (their last row is reused meanwhile).
type Watcher struct {
	now  func() time.Time
	mu   sync.Mutex
	seen map[string]watchEntry
}

// NewWatcher builds a Watcher using the given clock.
func NewWatcher(now func() time.Time) *Watcher {
	return &Watcher{now: now, seen: map[string]watchEntry{}}
}

func isErrState(s usage.State) bool { return s == usage.StateLoggedOut || s == usage.StateAPIError }

func (w *Watcher) cached(name string) (watchEntry, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	e, ok := w.seen[name]
	if ok && isErrState(e.row.State) && w.now().Sub(e.at) < ErrorRetryAfter {
		return e, true
	}
	return watchEntry{}, false
}

// Step runs one refresh cycle, skipping accounts still in error back-off.
func (w *Watcher) Step(ctx context.Context, d Deps) ([]Row, usage.Combined) {
	skip := map[string]usage.AccountUsage{}
	for _, a := range d.Accounts {
		if e, ok := w.cached(a.Name); ok {
			skip[a.Name] = e.row.AccountUsage
		}
	}
	inner := d.Fetch
	d.Fetch = func(ctx context.Context, a registry.Account) usage.AccountUsage {
		if au, ok := skip[a.Name]; ok {
			return au
		}
		return inner(ctx, a)
	}
	raw := collectRaw(ctx, d)
	w.record(d.Accounts, raw, skip)
	return finalize(d, raw)
}

func (w *Watcher) record(accts []registry.Account, rows []Row, skipped map[string]usage.AccountUsage) {
	w.mu.Lock()
	defer w.mu.Unlock()
	at := w.now()
	for i, a := range accts {
		if _, ok := skipped[a.Name]; ok {
			continue
		}
		w.seen[a.Name] = watchEntry{at: at, row: rows[i]}
	}
}

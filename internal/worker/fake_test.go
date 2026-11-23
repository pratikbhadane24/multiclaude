package worker

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/run"
)

type rsyncCall struct {
	local, remote string
	excludes      []string
}

type writeCall struct {
	path string
	mode os.FileMode
}

// fakeRemote records calls and serves files from a map. Scripts are
// answered by the first respond entry whose key is a substring.
type fakeRemote struct {
	mu          sync.Mutex
	files       map[string][]byte
	respond     map[string]run.Result
	delay       time.Duration
	scripts     []string
	interactive []string
	copies      [][2]string
	rsyncs      []rsyncCall
	writes      []writeCall
}

func newFakeRemote() *fakeRemote {
	return &fakeRemote{files: map[string][]byte{}, respond: map[string]run.Result{}}
}

func (f *fakeRemote) Run(ctx context.Context, script string) (run.Result, error) {
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return run.Result{}, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scripts = append(f.scripts, script)
	for k, r := range f.respond {
		if strings.Contains(script, k) {
			return r, nil
		}
	}
	return run.Result{}, nil
}

func (f *fakeRemote) Interactive(_ context.Context, cmd string) (int, error) {
	f.interactive = append(f.interactive, cmd)
	return 0, nil
}

func (f *fakeRemote) Copy(_ context.Context, local, remote string) error {
	f.copies = append(f.copies, [2]string{local, remote})
	return nil
}

func (f *fakeRemote) Rsync(_ context.Context, local, remote string, excludes []string) error {
	f.rsyncs = append(f.rsyncs, rsyncCall{local, remote, excludes})
	return nil
}

func (f *fakeRemote) Read(_ context.Context, path string) ([]byte, bool, error) {
	b, ok := f.files[path]
	return b, ok, nil
}

func (f *fakeRemote) Write(_ context.Context, path string, data []byte, mode os.FileMode) error {
	f.writes = append(f.writes, writeCall{path, mode})
	f.files[path] = data
	return nil
}

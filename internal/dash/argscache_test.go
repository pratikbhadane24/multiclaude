package dash

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestViewDoesNotReloadConfigOnEveryRender(t *testing.T) {
	r := &recorder{snap: twoAccountSnapshot()}
	var loads atomic.Int32
	deps := newTestDeps(r)
	deps.ArgsFor = func(string) ([]string, error) {
		loads.Add(1)
		return []string{"--model", "opus"}, nil
	}
	m := update(t, New(context.Background(), deps), snapshotMsg{snap: r.snap, at: testNow})
	afterSnapshot := loads.Load()

	for i := 0; i < 5; i++ {
		_ = m.View()
		_ = m.detailLines()
	}

	if loads.Load() != afterSnapshot {
		t.Fatalf("config loaded %d more times while rendering", loads.Load()-afterSnapshot)
	}
}

func TestSnapshotRefreshesCachedArgs(t *testing.T) {
	r := &recorder{snap: twoAccountSnapshot()}
	args := []string{"--a"}
	deps := newTestDeps(r)
	deps.ArgsFor = func(string) ([]string, error) { return args, nil }
	m := update(t, New(context.Background(), deps), snapshotMsg{snap: r.snap, at: testNow})
	args = []string{"--b"}

	m = update(t, m, snapshotMsg{snap: r.snap, at: testNow})

	if got, _ := m.argsFor("main"); len(got) != 1 || got[0] != "--b" {
		t.Fatalf("got %v", got)
	}
}

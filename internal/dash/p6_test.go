package dash

import (
	"errors"
	"strings"
	"testing"
)

func TestCacheWriteErrorIsToasted(t *testing.T) {
	m, r := newLoaded(t)
	snap := r.snap
	snap.CacheErr = errors.New("disk full")

	m = update(t, m, snapshotMsg{snap: snap, at: testNow})

	if !strings.Contains(m.toast, "usage cache: disk full") {
		t.Fatalf("toast=%q", m.toast)
	}
}

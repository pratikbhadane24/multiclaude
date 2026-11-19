package dash

import "testing"

func TestKeyAWhileCollectingDefersToOneCollectAfterIt(t *testing.T) {
	m, r := newLoaded(t)
	m, _ = press(t, m, "r") // a collect is now in flight (not drained)
	before := r.count(&r.collects)

	m, cmd := press(t, m, "A")

	if cmd != nil || !m.deps.ShowDuplicates || r.count(&r.collects) != before {
		t.Fatalf("A must not start a concurrent collect: cmd=%v show=%v", cmd != nil, m.deps.ShowDuplicates)
	}

	next, cmd := m.Update(snapshotMsg{snap: r.snap, at: testNow, showDuplicates: false})
	m = next.(Model)
	if cmd == nil || !m.loading {
		t.Fatalf("finishing collect should schedule one more: loading=%v", m.loading)
	}
	drain(cmd)
	if got := r.shows[len(r.shows)-1]; !got || r.count(&r.collects) != before+1 {
		t.Fatalf("shows=%v collects=%d", r.shows, r.count(&r.collects))
	}
}

func TestKeyATwiceWhileCollectingNeedsNoExtraCollect(t *testing.T) {
	m, r := newLoaded(t)
	m, _ = press(t, m, "r")
	m, _ = press(t, m, "A")
	m, _ = press(t, m, "A")

	next, cmd := m.Update(snapshotMsg{snap: r.snap, at: testNow, showDuplicates: false})
	m = next.(Model)

	if m.loading || m.pendingCollect || cmd != nil {
		t.Fatalf("flag is back to what the snapshot used: loading=%v pending=%v", m.loading, m.pendingCollect)
	}
}

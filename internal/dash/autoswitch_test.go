package dash

import (
	"strings"
	"testing"
)

func autoSwitchOn(s Snapshot) Snapshot {
	out := s
	out.AutoSwitch = true
	return out
}

func TestAutoSwitchBadgeShownOnlyWhenOn(t *testing.T) {
	m, r := newLoaded(t)
	if strings.Contains(m.View(), autoBadge) {
		t.Fatal("badge shown while off")
	}

	r.snap = autoSwitchOn(r.snap)
	m = update(t, m, snapshotMsg{snap: r.snap, at: testNow, started: testNow})

	if !strings.Contains(m.View(), autoBadge) {
		t.Fatalf("badge missing:\n%s", m.View())
	}
}

func TestAutoSwitchSkipsContinuePrompt(t *testing.T) {
	// Arrange
	m, r := newLoaded(t)
	r.snap = autoSwitchOn(r.snap)
	m = update(t, m, snapshotMsg{snap: r.snap, at: testNow, started: testNow})

	// Act
	m = exitClaude(t, m, r, autoSwitchOn(limitedMainSnapshot()))

	// Assert
	if m.mode != modeList || strings.Contains(m.View(), "Continue this conversation") {
		t.Fatalf("mode=%v view:\n%s", m.mode, m.View())
	}
}

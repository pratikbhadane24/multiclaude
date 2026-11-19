package dash

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestDemoReplacesEverySideEffect(t *testing.T) {
	r := &recorder{}
	d := Demo(newTestDeps(r))
	ctx := context.Background()

	snap := d.Collect(ctx)
	c, err := d.Launch("main")
	d.Doctor(ctx)
	_ = d.Run(ctx, "/usr/bin/osascript", nil)
	self := d.Self("login", "work")
	syncOut, syncErr := d.Sync(ctx)
	t3Out, t3Err := d.T3Sync(ctx)

	if self.Path != demoEcho || syncErr != nil || t3Err != nil || syncOut == "" || t3Out == "" {
		t.Fatalf("self=%q sync=%q/%v t3=%q/%v", self.Path, syncOut, syncErr, t3Out, t3Err)
	}
	if r.syncs+r.t3syncs != 0 {
		t.Fatalf("real sync called: %+v", r)
	}

	if len(snap.Rows) != 4 || err != nil || c.Path != demoEcho {
		t.Fatalf("rows=%d path=%q err=%v", len(snap.Rows), c.Path, err)
	}
	if r.collects+r.doctors+len(r.runs)+len(r.launched) != 0 {
		t.Fatalf("real deps were called: %+v", r)
	}
}

// TestDemoViewSnapshot renders the demo screen; run with -v to print it.
func TestDemoViewSnapshot(t *testing.T) {
	d := Demo(newTestDeps(&recorder{}))
	m := New(context.Background(), d)
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = update(t, m, snapshotMsg{snap: d.Collect(context.Background()), at: testNow})

	v := m.View()
	t.Log("\n" + v)

	for _, want := range []string{"limited 5h", "logged out", "★", "⚠ skip-permissions"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q", want)
		}
	}
	assertFits(t, v, 100, 30)
}

package doctor

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"multiclaude/internal/history"
	"multiclaude/internal/links"
	"multiclaude/internal/paths"
	"multiclaude/internal/registry"
)

func TestFixReplacedHistoryItemMergesInsteadOfBackup(t *testing.T) {
	s := healthy
	s.items = []links.Item{{Name: "projects", State: links.StateReplaced}}
	d, c := build(s, []registry.Account{workAcct})
	var merged string
	d.MergeHistory = func(_ paths.Paths, _, item string, _ time.Time) (history.MergeResult, error) {
		merged = item
		return history.MergeResult{Moved: 3, Conflicts: 1, Linked: true}, nil
	}

	cs := Run(context.Background(), d, true)

	got, _ := find(cs, "work", "projects")
	if merged != "projects" || c.replaced != 0 || got.Status != StatusFixed || !strings.Contains(got.Detail, "3 chats moved, 1 conflicts") {
		t.Fatalf("merged=%q calls=%+v got=%+v", merged, c, got)
	}
}

func TestFixHistoryNotLinkedIsFailNotFixed(t *testing.T) {
	s := healthy
	s.items = []links.Item{{Name: "projects", State: links.StateReplaced}}
	d, _ := build(s, []registry.Account{workAcct})
	d.MergeHistory = func(paths.Paths, string, string, time.Time) (history.MergeResult, error) {
		return history.MergeResult{}, nil
	}

	cs := Run(context.Background(), d, true)

	if got, _ := find(cs, "work", "projects"); got.Status != StatusFail || !strings.Contains(got.Detail, "did not link") {
		t.Fatalf("%+v", got)
	}
}

func TestFixHistoryErrorIsFailWithReason(t *testing.T) {
	s := healthy
	s.items = []links.Item{{Name: "projects", State: links.StateReplaced}}
	d, _ := build(s, []registry.Account{workAcct})
	d.MergeHistory = func(paths.Paths, string, string, time.Time) (history.MergeResult, error) {
		return history.MergeResult{}, errors.New("main projects is not a directory")
	}

	cs := Run(context.Background(), d, true)

	if got, _ := find(cs, "work", "projects"); got.Status != StatusFail || !strings.Contains(got.Detail, "main projects is not a directory") {
		t.Fatalf("%+v", got)
	}
}

func TestHistoryItemWithoutFixOnlyReportsFail(t *testing.T) {
	s := healthy
	s.items = []links.Item{{Name: "projects", State: links.StateReplaced}}
	d, _ := build(s, []registry.Account{workAcct})
	d.MergeHistory = func(paths.Paths, string, string, time.Time) (history.MergeResult, error) {
		t.Fatal("must not merge without --fix")
		return history.MergeResult{}, nil
	}

	cs := Run(context.Background(), d, false)

	if got, _ := find(cs, "work", "projects"); got.Status != StatusFail {
		t.Fatalf("%+v", got)
	}
}

package autoswitch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/paths"
	"github.com/pratikbhadane24/multiclaude/internal/registry"
	"github.com/pratikbhadane24/multiclaude/internal/status"
)

var pickNow = time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)

func pf(v float64) *float64 { return &v }

func tp(d time.Duration) *time.Time {
	t := pickNow.Add(d)
	return &t
}

// setup registers main plus names and writes the given cache entries.
func setup(t *testing.T, names []string, entries ...status.CacheAccount) paths.Paths {
	t.Helper()
	root := t.TempDir()
	p := paths.Paths{AccountsRoot: filepath.Join(root, "accts"), ClaudeDir: filepath.Join(root, "claude")}
	accts := []registry.Account{{Name: "main"}}
	for _, n := range names {
		accts = append(accts, registry.Account{Name: n, Dir: filepath.Join(p.AccountsRoot, n)})
	}
	if err := registry.Save(p, accts); err != nil {
		t.Fatal(err)
	}
	fetched := pickNow.Add(-time.Minute)
	for i := range entries {
		if entries[i].FetchedAt == nil && entries[i].State != "logged-out" {
			entries[i].FetchedAt = &fetched
		}
	}
	data, err := json.Marshal(status.Cache{Accounts: entries})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(status.CachePath(p)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(status.CachePath(p), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPickNextChoosesMostHeadroomExcludingCurrent(t *testing.T) {
	// Arrange
	p := setup(t, []string{"a", "b"},
		status.CacheAccount{Name: "main", FiveHourUsed: pf(5)},
		status.CacheAccount{Name: "a", FiveHourUsed: pf(60)},
		status.CacheAccount{Name: "b", FiveHourUsed: pf(20)},
	)

	// Act
	got, ok := PickNext(p, []string{"main"}, pickNow)

	// Assert
	if !ok || got.Name != "b" {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
}

func TestPickNextSkipsLimitedLoggedOutAndUnknown(t *testing.T) {
	p := setup(t, []string{"a", "b", "c", "d"},
		status.CacheAccount{Name: "main", FiveHourUsed: pf(10)},
		status.CacheAccount{Name: "a", FiveHourUsed: pf(100), State: status.StateLimited},
		status.CacheAccount{Name: "b", State: "logged-out"},
		status.CacheAccount{Name: "d", SevenDayUsed: pf(30)},
	)

	got, ok := PickNext(p, []string{"main"}, pickNow)

	if !ok || got.Name != "d" {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
	if _, ok := PickNext(p, []string{"main", "d"}, pickNow); ok {
		t.Fatal("want none")
	}
}

func TestPickNextExcludesAliasesOfExcludedLogin(t *testing.T) {
	p := setup(t, []string{"admin", "work"},
		status.CacheAccount{Name: "main", LoginID: "u1", FiveHourUsed: pf(90)},
		status.CacheAccount{Name: "admin", LoginID: "u1", FiveHourUsed: pf(10)},
		status.CacheAccount{Name: "work", LoginID: "u2", FiveHourUsed: pf(50)},
	)

	got, ok := PickNext(p, []string{"main"}, pickNow)

	if !ok || got.Name != "work" {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
}

func TestPickNextWithoutCache(t *testing.T) {
	root := t.TempDir()
	p := paths.Paths{AccountsRoot: filepath.Join(root, "accts")}
	if _, ok := PickNext(p, nil, pickNow); ok {
		t.Fatal("want none")
	}
}

func TestNextResetPicksSoonestBindingReset(t *testing.T) {
	p := setup(t, []string{"a", "b", "c"},
		// main: 5h resets soon but the weekly window binds later.
		status.CacheAccount{Name: "main", FiveHourUsed: pf(100), FiveHourResetsAt: tp(time.Hour),
			SevenDayUsed: pf(100), SevenDayResetsAt: tp(48 * time.Hour)},
		status.CacheAccount{Name: "a", FiveHourUsed: pf(100), FiveHourResetsAt: tp(3 * time.Hour)},
		status.CacheAccount{Name: "b", FiveHourUsed: pf(100)},
		status.CacheAccount{Name: "c", State: "logged-out"},
	)

	got, at, ok := NextReset(p, pickNow)

	if !ok || got.Name != "a" || !at.Equal(*tp(3 * time.Hour)) {
		t.Fatalf("got %+v at=%v ok=%v", got, at, ok)
	}
}

func TestNextResetPastResetIsNow(t *testing.T) {
	p := setup(t, nil, status.CacheAccount{Name: "main", FiveHourUsed: pf(100), FiveHourResetsAt: tp(-time.Hour)})
	got, at, ok := NextReset(p, pickNow)
	if !ok || got.Name != "main" || !at.Equal(pickNow) {
		t.Fatalf("got %+v at=%v ok=%v", got, at, ok)
	}
}

func TestNextResetUnknown(t *testing.T) {
	p := setup(t, nil, status.CacheAccount{Name: "main", FiveHourUsed: pf(100)})
	if _, _, ok := NextReset(p, pickNow); ok {
		t.Fatal("want unknown")
	}
}

package status

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/paths"
	"github.com/pratikbhadane24/multiclaude/internal/usage"
)

func fp(v float64) *float64 { return &v }

func cacheRows(reset time.Time) []Row {
	return []Row{
		{AccountUsage: usage.AccountUsage{Account: "main", Email: "m@x.com", Plan: "max", State: usage.StateOK,
			Usage: &usage.Usage{FiveHour: usage.Window{UsedPct: fp(76), ResetsAt: &reset}, SevenDay: usage.Window{UsedPct: fp(12)}}}},
		{AccountUsage: usage.AccountUsage{Account: "work", State: usage.StateOK,
			Usage: &usage.Usage{FiveHour: usage.Window{UsedPct: fp(100)}, SevenDay: usage.Window{UsedPct: fp(5)}}}},
		{AccountUsage: usage.AccountUsage{Account: "out", State: usage.StateLoggedOut}},
	}
}

func TestWriteCacheRoundTripAndModes(t *testing.T) {
	p := paths.Paths{AccountsRoot: filepath.Join(t.TempDir(), "accts")}
	now := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	reset := now.Add(time.Hour)

	if err := WriteCache(p, cacheRows(reset), usage.Combined{}, now); err != nil {
		t.Fatal(err)
	}
	c, err := ReadCache(p)

	if err != nil {
		t.Fatal(err)
	}
	if !c.WrittenAt.Equal(now) || len(c.Accounts) != 3 {
		t.Fatalf("cache=%+v", c)
	}
	m := c.Accounts[0]
	if m.Name != "main" || m.Email != "m@x.com" || m.Plan != "max" || *m.FiveHourUsed != 76 || *m.SevenDayUsed != 12 || !m.FiveHourResetsAt.Equal(reset) {
		t.Fatalf("main=%+v", m)
	}
	if c.Accounts[1].State != "limited" {
		t.Fatalf("state=%q want limited", c.Accounts[1].State)
	}
	if c.Accounts[2].State != "logged-out" || c.Accounts[2].FiveHourUsed != nil {
		t.Fatalf("out=%+v", c.Accounts[2])
	}
	file := filepath.Join(p.AccountsRoot, "cache", "usage.json")
	if st, _ := os.Stat(file); st.Mode().Perm() != 0o600 {
		t.Fatalf("file mode %v", st.Mode())
	}
	if st, _ := os.Stat(filepath.Dir(file)); st.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v", st.Mode())
	}
	entries, _ := os.ReadDir(filepath.Dir(file))
	for _, e := range entries {
		if e.Name() != "usage.json" && e.Name() != "usage.lock" {
			t.Fatalf("temp files left: %v", entries)
		}
	}
}

func TestWriteCacheNeverContainsTokens(t *testing.T) {
	p := paths.Paths{AccountsRoot: t.TempDir()}
	rows := cacheRows(time.Now())
	rows[0].Err = "sk-ant-secret"

	if err := WriteCache(p, rows, usage.Combined{}, time.Now()); err != nil {
		t.Fatal(err)
	}

	b, _ := os.ReadFile(filepath.Join(p.AccountsRoot, "cache", "usage.json"))
	if strings.Contains(string(b), "sk-ant") || strings.Contains(strings.ToLower(string(b)), "token") {
		t.Fatalf("cache leaks: %s", b)
	}
}

func TestReadCacheMissingAndCorrupt(t *testing.T) {
	p := paths.Paths{AccountsRoot: t.TempDir()}
	if _, err := ReadCache(p); err == nil {
		t.Fatal("want error for missing cache")
	}
	_ = os.MkdirAll(filepath.Join(p.AccountsRoot, "cache"), 0o700)
	_ = os.WriteFile(filepath.Join(p.AccountsRoot, "cache", "usage.json"), []byte("{nope"), 0o600)
	if _, err := ReadCache(p); err == nil {
		t.Fatal("want error for corrupt cache")
	}
}

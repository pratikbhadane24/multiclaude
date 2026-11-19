// Package status gathers and renders per-account and combined usage.
package status

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	"multiclaude/internal/links"
	"multiclaude/internal/paths"
	"multiclaude/internal/registry"
	"multiclaude/internal/tokens"
	"multiclaude/internal/usage"
)

const projectsDirName = "projects"

// Deps are the injected collaborators of Collect.
type Deps struct {
	Accounts []registry.Account
	Fetch    func(ctx context.Context, a registry.Account) usage.AccountUsage
	Today    func(projectsDir string, now time.Time) (tokens.Tokens, error)
	Inspect  func(p paths.Paths, dir string) ([]links.Item, error)
	Paths    paths.Paths
	Now      func() time.Time
	// EmailFallback (optional) supplies an email when neither the usage result
	// nor the registry has one (e.g. main). Failures return "".
	EmailFallback func(ctx context.Context, a registry.Account) string
	// ShowDuplicates keeps accounts that share a login as separate rows
	// (labelled SameAs); by default they collapse into the first one.
	ShowDuplicates bool
}

// Row is one rendered account line.
type Row struct {
	usage.AccountUsage
	LinkErr      string
	LinkWarnings []string
	// Aliases are later accounts signed in as the same login, merged into this row.
	Aliases []string
	// SameAs names the canonical account when this row is a shown duplicate.
	SameAs string
}

func accountDir(d Deps, a registry.Account) string {
	if a.IsMain() {
		return d.Paths.ClaudeDir
	}
	return a.Dir
}

func collectOne(ctx context.Context, d Deps, a registry.Account) Row {
	au := d.Fetch(ctx, a)
	if au.Account == "" {
		au.Account = a.Name
	}
	if au.Email == "" {
		au.Email = a.Email
	}
	if au.Email == "" && d.EmailFallback != nil {
		au.Email = d.EmailFallback(ctx, a)
	}
	row := Row{AccountUsage: au}
	dir := accountDir(d, a)
	if d.Inspect != nil {
		items, err := d.Inspect(d.Paths, dir)
		if err != nil {
			row.LinkErr = err.Error()
		}
		row.LinkWarnings = replacedItems(items)
	}
	return row
}

func replacedItems(items []links.Item) []string {
	var out []string
	for _, it := range items {
		if it.State == links.StateReplaced {
			out = append(out, it.Name)
		}
	}
	return out
}

// collectRaw fetches every account concurrently, preserving d.Accounts order.
func collectRaw(ctx context.Context, d Deps) []Row {
	rows := make([]Row, len(d.Accounts))
	var wg sync.WaitGroup
	for i, a := range d.Accounts {
		wg.Add(1)
		go func(i int, a registry.Account) {
			defer wg.Done()
			rows[i] = collectOne(ctx, d, a)
		}(i, a)
	}
	wg.Wait()
	return rows
}

// finalize merges rows that are the same login (recomputed on every call, so
// they split as soon as the logins differ) and aggregates the result.
func finalize(d Deps, rows []Row) ([]Row, usage.Combined) {
	list := make([]usage.AccountUsage, len(rows))
	for i, r := range rows {
		list[i] = r.AccountUsage
	}
	canon := usage.CanonicalIndex(list)
	aliases := map[int][]string{}
	for i, r := range rows {
		if canon[i] != i {
			aliases[canon[i]] = append(aliases[canon[i]], r.Account)
		}
	}
	out := make([]Row, 0, len(rows))
	for i, r := range rows {
		switch {
		case canon[i] == i:
			r.Aliases = aliases[i]
			out = append(out, r)
		case d.ShowDuplicates:
			r.SameAs = rows[canon[i]].Account
			out = append(out, r)
		}
	}
	c := usage.Aggregate(list)
	c.Today, c.TodayErr = scanToday(d)
	return out, c
}

// Collect gathers every account concurrently, preserving d.Accounts order.
// Accounts signed in as the same login collapse into the first one (see
// Row.Aliases) unless d.ShowDuplicates is set.
func Collect(ctx context.Context, d Deps) ([]Row, usage.Combined) {
	return finalize(d, collectRaw(ctx, d))
}

// scanToday totals today's tokens once, from main's projects dir: history is
// shared by every account, so a per-account scan would count it repeatedly.
func scanToday(d Deps) (tokens.Tokens, string) {
	if d.Today == nil {
		return tokens.Tokens{}, ""
	}
	t, err := d.Today(filepath.Join(d.Paths.ClaudeDir, projectsDirName), d.Now())
	if err != nil {
		return t, err.Error()
	}
	return t, ""
}

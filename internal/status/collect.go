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
}

// Row is one rendered account line.
type Row struct {
	usage.AccountUsage
	Today        tokens.Tokens
	TodayErr     string
	LinkErr      string
	LinkWarnings []string
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
	if d.Today != nil {
		t, err := d.Today(filepath.Join(dir, projectsDirName), d.Now())
		row.Today = t
		if err != nil {
			row.TodayErr = err.Error()
		}
	}
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

// Collect gathers every account concurrently, preserving d.Accounts order.
func Collect(ctx context.Context, d Deps) ([]Row, usage.Combined) {
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
	list := make([]usage.AccountUsage, len(rows))
	for i, r := range rows {
		list[i] = r.AccountUsage
	}
	return rows, usage.Aggregate(list)
}

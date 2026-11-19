// Package doctor checks the ccm setup and, with fix=true, repairs only the
// shared-file problems (missing/replaced links, MCP drift). All side effects
// are injected through Deps.
package doctor

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"multiclaude/internal/consts"
	"multiclaude/internal/history"
	"multiclaude/internal/links"
	"multiclaude/internal/paths"
	"multiclaude/internal/registry"
)

// Status is the outcome of one check.
type Status string

const (
	StatusOK    Status = "ok"
	StatusWarn  Status = "warn"
	StatusFail  Status = "fail"
	StatusFixed Status = "fixed"
	// StatusInfo is a note, not a problem: it never affects the exit code.
	StatusInfo Status = "info"
)

const (
	execBits    = 0o111
	t3Binary    = "t3"
	hintInstall = "run: ccm install"
)

// Check is one doctor result. Account "" means a global check.
type Check struct {
	Account string
	Name    string
	Status  Status
	Detail  string
}

// Deps holds every side effect doctor needs.
type Deps struct {
	Paths        paths.Paths
	Accounts     []registry.Account
	Now          func() time.Time
	Stat         func(string) (os.FileInfo, error)
	EvalSymlinks func(string) (string, error)
	LookPath     func(string) (string, error)
	Inspect      func(p paths.Paths, dir string) ([]links.Item, error)
	LinkMissing  func(p paths.Paths, dir string) ([]string, error)
	FixReplaced  func(p paths.Paths, dir, item string, now time.Time) (links.FixResult, error)
	// MergeHistory (optional) migrates a replaced history item into main
	// instead of FixReplaced's backup-and-relink.
	MergeHistory    func(p paths.Paths, dir, item string, now time.Time) (history.MergeResult, error)
	MCPDrift        func(p paths.Paths, dir string) (bool, error)
	MCPSync         func(p paths.Paths, dir string) (bool, error)
	KeychainPresent func(ctx context.Context, a registry.Account) (bool, error)
	T3HasInstance   func(a registry.Account) (bool, error)
	// Identity (optional) returns an account's login identity: the keychain
	// account UUID and the email; either may be empty when unknown.
	Identity func(ctx context.Context, a registry.Account) (uuid, email string)
}

// Run performs all checks in order. With fix=true it repairs missing and
// replaced shared links and MCP drift; nothing else is ever modified.
func Run(ctx context.Context, d Deps, fix bool) []Check {
	out := globalChecks(d)
	for _, a := range d.Accounts {
		if !a.IsMain() {
			out = append(out, linkChecks(d, a, fix)...)
			out = append(out, mcpCheck(d, a, fix))
		}
		out = append(out, keychainCheck(ctx, d, a), t3Check(d, a))
	}
	return append(out, duplicateChecks(ctx, d)...)
}

func okCheck(account, name string) Check {
	return Check{Account: account, Name: name, Status: StatusOK}
}

func execProblem(d Deps, path string) string {
	info, err := d.Stat(path)
	if err != nil {
		return fmt.Sprintf("%s: %v", path, err)
	}
	if info.IsDir() || info.Mode()&execBits == 0 {
		return path + " is not an executable file"
	}
	return ""
}

func globalChecks(d Deps) []Check {
	const realName, shimName = "real claude", "shim"
	var out []Check

	realProblem := execProblem(d, d.Paths.RealClaude)
	out = append(out, statusFrom("", realName, StatusFail, realProblem))

	shimProblem := execProblem(d, d.Paths.ShimPath)
	if shimProblem != "" {
		shimProblem += " (" + hintInstall + ")"
	}
	out = append(out, statusFrom("", shimName, StatusFail, shimProblem))

	out = append(out, sameTargetCheck(d, realProblem == "" && shimProblem == ""))

	if _, err := d.LookPath(t3Binary); err != nil {
		out = append(out, Check{Name: "t3 on PATH", Status: StatusWarn, Detail: "t3 CLI not found on PATH"})
	} else {
		out = append(out, okCheck("", "t3 on PATH"))
	}
	return out
}

func statusFrom(account, name string, bad Status, problem string) Check {
	if problem == "" {
		return okCheck(account, name)
	}
	return Check{Account: account, Name: name, Status: bad, Detail: problem}
}

func sameTargetCheck(d Deps, bothExist bool) Check {
	const name = "real claude is not the shim"
	if !bothExist {
		return okCheck("", name)
	}
	realPath, err1 := d.EvalSymlinks(d.Paths.RealClaude)
	shimPath, err2 := d.EvalSymlinks(d.Paths.ShimPath)
	if err1 != nil || err2 != nil {
		return Check{Name: name, Status: StatusFail, Detail: fmt.Sprintf("cannot resolve symlinks: %v", firstErr(err1, err2))}
	}
	if realPath == shimPath {
		return Check{Name: name, Status: StatusFail, Detail: "real claude resolves to the shim; set CCM_CLAUDE_BIN"}
	}
	return okCheck("", name)
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

func linkChecks(d Deps, a registry.Account, fix bool) []Check {
	items, err := d.Inspect(d.Paths, a.Dir)
	if err != nil {
		return []Check{{Account: a.Name, Name: "shared links", Status: StatusFail, Detail: err.Error()}}
	}
	var out, missing []Check
	for _, it := range items {
		name := "link " + it.Name
		switch it.State {
		case links.StateOK, links.StateNoSource:
			// nothing to report
		case links.StateMissing:
			missing = append(missing, Check{Account: a.Name, Name: name, Status: StatusWarn, Detail: it.Name + " is not linked to the shared copy"})
		case links.StateReplaced:
			out = append(out, replacedCheck(d, a, it.Name, fix))
		case links.StateWrongTarget:
			out = append(out, Check{Account: a.Name, Name: name, Status: StatusWarn, Detail: it.Name + " links to the wrong target"})
		}
	}
	return append(out, fixMissing(d, a, missing, fix)...)
}

func replacedCheck(d Deps, a registry.Account, item string, fix bool) Check {
	c := Check{Account: a.Name, Name: "link " + item, Status: StatusFail, Detail: item + " is no longer shared"}
	if !fix {
		return c
	}
	if d.MergeHistory != nil && slices.Contains(consts.HistoryItems, item) {
		return mergeHistoryCheck(d, a, item, c)
	}
	res, err := d.FixReplaced(d.Paths, a.Dir, item, d.Now())
	if err != nil {
		c.Detail = fmt.Sprintf("%s is no longer shared; fix failed: %v", item, err)
		return c
	}
	c.Status = StatusFixed
	c.Detail = item + " re-linked (" + res.Action + ")"
	if res.BackupPath != "" {
		c.Detail += ", backup: " + res.BackupPath
	}
	return c
}

func mergeHistoryCheck(d Deps, a registry.Account, item string, c Check) Check {
	res, err := d.MergeHistory(d.Paths, a.Dir, item, d.Now())
	if err != nil {
		c.Detail = fmt.Sprintf("%s is no longer shared; merge failed: %v", item, err)
		return c
	}
	if !res.Linked {
		c.Detail = item + " is no longer shared; merge did not link it (nothing to merge)"
		return c
	}
	c.Status = StatusFixed
	c.Detail = fmt.Sprintf("%s merged into shared history (%d chats moved, %d conflicts kept as *.from-%s-*)", item, res.Moved, res.Conflicts, a.Name)
	return c
}

func fixMissing(d Deps, a registry.Account, missing []Check, fix bool) []Check {
	if !fix || len(missing) == 0 {
		return missing
	}
	_, err := d.LinkMissing(d.Paths, a.Dir)
	out := make([]Check, 0, len(missing))
	for _, c := range missing {
		if err != nil {
			c.Status, c.Detail = StatusFail, c.Detail+"; fix failed: "+err.Error()
		} else {
			c.Status, c.Detail = StatusFixed, strings.Replace(c.Detail, "is not linked to", "linked to", 1)
		}
		out = append(out, c)
	}
	return out
}

func mcpCheck(d Deps, a registry.Account, fix bool) Check {
	const name = "MCP drift"
	drift, err := d.MCPDrift(d.Paths, a.Dir)
	if err != nil {
		return Check{Account: a.Name, Name: name, Status: StatusFail, Detail: err.Error()}
	}
	if !drift {
		return okCheck(a.Name, name)
	}
	c := Check{Account: a.Name, Name: name, Status: StatusWarn, Detail: "MCP servers out of sync"}
	if !fix {
		return c
	}
	if _, err := d.MCPSync(d.Paths, a.Dir); err != nil {
		c.Status, c.Detail = StatusFail, c.Detail+"; fix failed: "+err.Error()
		return c
	}
	c.Status, c.Detail = StatusFixed, "MCP servers synced"
	return c
}

func keychainCheck(ctx context.Context, d Deps, a registry.Account) Check {
	const name = "keychain"
	present, err := d.KeychainPresent(ctx, a)
	if err != nil {
		return Check{Account: a.Name, Name: name, Status: StatusFail, Detail: err.Error()}
	}
	if !present {
		return Check{Account: a.Name, Name: name, Status: StatusWarn, Detail: "logged out → ccm login " + a.Name}
	}
	return okCheck(a.Name, name)
}

func t3Check(d Deps, a registry.Account) Check {
	const name = "T3 instance"
	has, err := d.T3HasInstance(a)
	if err != nil {
		return Check{Account: a.Name, Name: name, Status: StatusFail, Detail: err.Error()}
	}
	if !has {
		return Check{Account: a.Name, Name: name, Status: StatusWarn, Detail: "not in T3 → ccm t3-sync"}
	}
	return okCheck(a.Name, name)
}

// Summary returns the exit code (0 iff no fail/warn remain) and report text.
func Summary(checks []Check) (int, string) {
	var b strings.Builder
	problems := 0
	for _, c := range checks {
		prefix := ""
		switch c.Status {
		case StatusFail:
			prefix = "✗ "
			problems++
		case StatusWarn:
			prefix = "! "
			problems++
		case StatusFixed:
			prefix = "✓ fixed "
		case StatusInfo:
			b.WriteString("i " + infoLabel(c) + c.Detail + "\n")
			continue
		default:
			continue
		}
		b.WriteString(prefix + label(c) + ": " + c.Detail + "\n")
	}
	if problems == 0 {
		b.WriteString("doctor: all good\n")
		return 0, b.String()
	}
	fmt.Fprintf(&b, "doctor: %d problems\n", problems)
	return 1, b.String()
}

func label(c Check) string {
	if c.Account == "" {
		return c.Name
	}
	return "[" + c.Account + "] " + c.Name
}

// infoLabel prefixes an info line with its account, e.g. "[admin] ".
func infoLabel(c Check) string {
	if c.Account == "" {
		return ""
	}
	return "[" + c.Account + "] "
}

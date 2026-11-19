package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"multiclaude/internal/keychain"
	"multiclaude/internal/paths"
	"multiclaude/internal/registry"
)

const securityBin = "/usr/bin/security"

// cmdRemove handles `ccm remove <name> [--purge]`.
func (a *App) cmdRemove(ctx context.Context, args []string) int {
	fs := a.newFlags("remove")
	purge := fs.Bool("purge", false, "also delete the account dir and its Keychain item")
	pos, code, ok := a.parseArgs(fs, args, 1, "remove <name> [--purge]")
	if !ok {
		return code
	}
	accts, err := registry.Load(a.Paths)
	if err != nil {
		return a.errorf("%v", err)
	}
	acct, found := registry.Find(accts, pos[0])
	switch {
	case !found:
		return a.errorf("unknown account %q", pos[0])
	case acct.IsMain():
		return a.errorf("the main account cannot be removed")
	}
	if *purge {
		if err := purgeAllowed(a.Paths, acct.Dir); err != nil {
			return a.errorf("refusing to purge: %v", err)
		}
	}
	yes, interrupted := a.confirm(ctx, fmt.Sprintf("Remove account %q? [y/N] ", acct.Name))
	if !yes {
		a.printf("Cancelled.\n")
		if interrupted {
			return ExitInterrupted
		}
		return ExitError
	}
	if err := registry.Save(a.Paths, registry.Without(accts, acct.Name)); err != nil {
		return a.errorf("%v", err)
	}
	a.printf("Removed account %q.\n", acct.Name)
	if !*purge {
		a.printf("Its files are kept in %s (use --purge to delete them).\n", acct.Dir)
	} else if code := a.purge(ctx, acct); code != ExitOK {
		return code
	}
	a.printf("Its T3 Code instance (claude-%s) is not removed; delete it in T3 Code, then run ccm t3-sync.\n", acct.Name)
	return ExitOK
}

// confirm asks a y/N question; interrupted reports a Ctrl-C while waiting.
func (a *App) confirm(ctx context.Context, prompt string) (yes, interrupted bool) {
	answer, err := a.promptLine(ctx, prompt)
	if err != nil {
		a.printf("\n")
		return false, ctx.Err() != nil
	}
	answer = strings.ToLower(answer)
	return answer == "y" || answer == "yes", false
}

// resolved returns p with symlinks evaluated; a missing tail is kept as-is
// after resolving its deepest existing ancestor.
func resolved(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	parent := filepath.Dir(p)
	if parent == p {
		return filepath.Clean(p)
	}
	return filepath.Join(resolved(parent), filepath.Base(p))
}

// within reports whether path is dir itself or lies inside it.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// purgeAllowed accepts only a real (non-symlink) direct child of the accounts
// root that is neither the shim dir, the Claude dir, nor home or an ancestor of them.
func purgeAllowed(p paths.Paths, dir string) error {
	if dir == "" || !filepath.IsAbs(dir) {
		return fmt.Errorf("account dir %q is not an absolute path", dir)
	}
	clean, root := filepath.Clean(dir), filepath.Clean(p.AccountsRoot)
	if filepath.Dir(clean) != root {
		return fmt.Errorf("%s is outside %s (must be a direct child)", clean, root)
	}
	if resolved(filepath.Dir(clean)) != resolved(root) {
		return fmt.Errorf("%s resolves outside %s", clean, root)
	}
	if fi, err := os.Lstat(clean); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink", clean)
	}
	target := filepath.Join(resolved(filepath.Dir(clean)), filepath.Base(clean))
	if target == resolved(filepath.Dir(p.ShimPath)) {
		return fmt.Errorf("%s is the shim directory", clean)
	}
	for _, protected := range []string{p.ClaudeDir, p.Home} {
		if protected != "" && within(target, resolved(protected)) {
			return fmt.Errorf("%s contains %s", clean, protected)
		}
	}
	return nil
}

// purge deletes the account dir and its Keychain item.
func (a *App) purge(ctx context.Context, acct registry.Account) int {
	code := ExitOK
	if err := os.RemoveAll(acct.Dir); err != nil {
		code = a.errorf("could not fully delete %s: %v\nLeftover files remain; remove them with: rm -rf %q", acct.Dir, err, acct.Dir)
	} else {
		a.printf("Deleted %s.\n", acct.Dir)
	}
	svc := keychain.Service(acct.Dir)
	res, err := a.Runner.Run(ctx, securityBin, []string{"delete-generic-password", "-s", svc}, nil, false)
	if err != nil {
		return a.errorf("delete Keychain item %q: %v", svc, err)
	}
	if res.Code != 0 {
		a.warnf("no Keychain item %q deleted (security exited %d)", svc, res.Code)
		return code
	}
	a.printf("Deleted Keychain item %q.\n", svc)
	return code
}

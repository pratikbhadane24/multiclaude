package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"multiclaude/internal/keychain"
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
		if err := purgeAllowed(a.Paths.AccountsRoot, filepath.Dir(a.Paths.ShimPath), acct.Dir); err != nil {
			return a.errorf("refusing to purge: %v", err)
		}
	}
	if !a.confirm(fmt.Sprintf("Remove account %q? [y/N] ", acct.Name)) {
		a.printf("Cancelled.\n")
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

func (a *App) confirm(prompt string) bool {
	answer, err := a.promptLine(prompt)
	if err != nil {
		a.printf("\n")
		return false
	}
	answer = strings.ToLower(answer)
	return answer == "y" || answer == "yes"
}

// purgeAllowed accepts only a dir strictly inside root that is not the shim dir.
func purgeAllowed(root, shimDir, dir string) error {
	if dir == "" || !filepath.IsAbs(dir) {
		return fmt.Errorf("account dir %q is not an absolute path", dir)
	}
	clean := filepath.Clean(dir)
	rel, err := filepath.Rel(filepath.Clean(root), clean)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s is outside %s", clean, root)
	}
	if clean == filepath.Clean(shimDir) {
		return fmt.Errorf("%s is the shim directory", clean)
	}
	return nil
}

// purge deletes the account dir and its Keychain item.
func (a *App) purge(ctx context.Context, acct registry.Account) int {
	if err := os.RemoveAll(acct.Dir); err != nil {
		return a.errorf("delete %s: %v", acct.Dir, err)
	}
	a.printf("Deleted %s.\n", acct.Dir)
	svc := keychain.Service(acct.Dir)
	res, err := a.Runner.Run(ctx, securityBin, []string{"delete-generic-password", "-s", svc}, nil, false)
	if err != nil {
		return a.errorf("delete Keychain item %q: %v", svc, err)
	}
	if res.Code != 0 {
		a.warnf("no Keychain item %q deleted (security exited %d)", svc, res.Code)
		return ExitOK
	}
	a.printf("Deleted Keychain item %q.\n", svc)
	return ExitOK
}

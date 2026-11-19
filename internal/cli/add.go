package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"multiclaude/internal/auth"
	"multiclaude/internal/links"
	"multiclaude/internal/mcp"
	"multiclaude/internal/registry"
)

const accountDirMode = 0o700

// resolveEmail picks the email from the argument, the existing entry, or a prompt.
func (a *App) resolveEmail(name, email string, existing registry.Account) (string, error) {
	if email == "" {
		email = existing.Email
	}
	if email != "" {
		return email, nil
	}
	email, err := a.promptLine(fmt.Sprintf("Email for account %q: ", name))
	if err != nil || email == "" {
		return "", fmt.Errorf("an email is required for account %q", name)
	}
	return email, nil
}

func emailOwner(accts []registry.Account, name, email string) (string, bool) {
	for _, o := range accts {
		if o.Name != name && o.Email != "" && strings.EqualFold(o.Email, email) {
			return o.Name, true
		}
	}
	return "", false
}

// Add creates (or resumes) an account, links shared config, syncs MCP and logs in.
func (a *App) Add(ctx context.Context, name, email string) int {
	if err := registry.ValidateName(name); err != nil {
		return a.errorf("%v", err)
	}
	accts, err := registry.Load(a.Paths)
	if err != nil {
		return a.errorf("%v", err)
	}
	existing, _ := registry.Find(accts, name)
	email, err = a.resolveEmail(name, email, existing)
	if err != nil {
		return a.errorf("%v", err)
	}
	if owner, dup := emailOwner(accts, name, email); dup {
		return a.errorf("email %s is already used by account %q", email, owner)
	}
	dir := registry.NormalizeDir(filepath.Join(a.Paths.AccountsRoot, name), a.Paths.Home)
	acct := registry.Account{Name: name, Dir: dir, Email: email}
	if code := a.setupAccount(acct, accts); code != ExitOK {
		return code
	}
	if existing.Email != "" {
		if st, err := auth.GetStatus(ctx, a.Runner, a.Paths, acct); err == nil && st.LoggedIn && strings.EqualFold(st.Email, acct.Email) {
			a.printf("Account %q is already logged in as %s.\n", name, st.Email)
			return ExitOK
		}
	}
	return a.loginOne(ctx, acct)
}

// setupAccount creates the dir, links shared items, syncs MCP and saves the registry.
func (a *App) setupAccount(acct registry.Account, accts []registry.Account) int {
	if err := os.MkdirAll(acct.Dir, accountDirMode); err != nil {
		return a.errorf("create %s: %v", acct.Dir, err)
	}
	if _, err := links.LinkMissing(a.Paths, acct.Dir); err != nil {
		return a.errorf("link shared config: %v", err)
	}
	if _, err := mcp.Sync(a.Paths, acct.Dir); err != nil {
		a.warnf("mcp sync: %v", err)
	}
	if err := registry.Save(a.Paths, registry.With(accts, acct)); err != nil {
		return a.errorf("%v", err)
	}
	return ExitOK
}

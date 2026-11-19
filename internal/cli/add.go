package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"multiclaude/internal/auth"
	"multiclaude/internal/browser"
	"multiclaude/internal/links"
	"multiclaude/internal/mcp"
	"multiclaude/internal/registry"
)

const accountDirMode = 0o700

// resolveEmail picks the email from the argument, the existing entry, or a prompt.
func (a *App) resolveEmail(ctx context.Context, name, email string, existing registry.Account) (string, error) {
	if email == "" {
		email = existing.Email
	}
	if email != "" {
		return email, nil
	}
	email, err := a.promptLine(ctx, fmt.Sprintf("Email for account %q: ", name))
	if err != nil || email == "" {
		return "", fmt.Errorf("an email is required for account %q", name)
	}
	return email, nil
}

// emailOwner finds another account already signed in with email. An account
// with no registry email (main) is resolved through its login status.
func (a *App) emailOwner(ctx context.Context, accts []registry.Account, name, email string) (string, bool) {
	for _, o := range accts {
		if o.Name == name {
			continue
		}
		if strings.EqualFold(a.knownEmail(ctx, o), email) {
			return o.Name, true
		}
	}
	return "", false
}

// knownEmail is the registry email, else the logged-in one ("" when unknown).
func (a *App) knownEmail(ctx context.Context, o registry.Account) string {
	if o.Email != "" {
		return o.Email
	}
	return a.authEmail(ctx, o)
}

// Add creates (or resumes) an account, links shared config, syncs MCP and logs in.
func (a *App) Add(ctx context.Context, name, email string) int {
	return a.AddWith(ctx, name, email, "")
}

// AddWith is Add with an optional login browser mode ("" keeps the existing one).
func (a *App) AddWith(ctx context.Context, name, email, open string) int {
	mode, err := browser.ParseMode(open)
	if err != nil {
		return a.errorf("%v", err)
	}
	if err := registry.ValidateName(name); err != nil {
		return a.errorf("%v", err)
	}
	accts, err := registry.Load(a.Paths)
	if err != nil {
		return a.errorf("%v", err)
	}
	existing, _ := registry.Find(accts, name)
	email, err = a.resolveEmail(ctx, name, email, existing)
	if ctx.Err() != nil {
		a.printf("\nCancelled.\n")
		return ExitInterrupted
	}
	if err != nil {
		return a.errorf("%v", err)
	}
	if owner, dup := a.emailOwner(ctx, accts, name, email); dup {
		fmt.Fprintf(a.Err, "ccm: email %s is already used by account %q\n", email, owner)
		return ExitMismatch
	}
	dir := registry.NormalizeDir(filepath.Join(a.Paths.AccountsRoot, name), a.Paths.Home)
	acct := registry.Account{Name: name, Dir: dir, Email: email, Browser: existing.Browser}
	if open != "" {
		acct.Browser = mode.String()
	}
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
	if !a.migrateHistory(acct) {
		return ExitError
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

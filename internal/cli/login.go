package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/pratikbhadane24/multiclaude/internal/auth"
	"github.com/pratikbhadane24/multiclaude/internal/browser"
	"github.com/pratikbhadane24/multiclaude/internal/registry"
)

const allFlag = "--all"

func (a *App) signInHint(email string) {
	if email == "" {
		return
	}
	a.printf("Sign in as %s in the browser. If it's signed in to another Claude account, open the printed URL in a private window.\n", email)
}

// loginOne runs the login for one account and returns an exit code.
func (a *App) loginOne(ctx context.Context, acct registry.Account) int {
	if a.SelfExe == "" {
		return a.errorf("cannot resolve ccm path; refusing to start login (it could open your default browser)")
	}
	a.signInHint(acct.Email)
	mode, err := browser.ParseMode(acct.Browser)
	if err != nil {
		a.warnf("%s: %v; using copy", acct.Name, err)
	}
	// Detach from ctx so Ctrl-C reaches claude (foreground) instead of killing it mid-OAuth.
	st, err := auth.LoginWith(context.WithoutCancel(ctx), a.Runner, a.Paths, acct, acct.Email, auth.LoginOptions{SelfExe: a.SelfExe, Mode: mode})
	var mm *auth.MismatchError
	if errors.As(err, &mm) {
		fmt.Fprintf(a.Err, "ccm: signed in as %s but account %q expects %s. Run `ccm login %s` again using the right Claude account.\n", mm.Got, acct.Name, mm.Want, acct.Name)
		return ExitMismatch
	}
	if err != nil {
		return a.errorf("%v", err)
	}
	a.printf("Logged in %s as %s.\n", acct.Name, st.Email)
	return ExitOK
}

// Login logs in the named account, or every logged-out account for "--all".
func (a *App) Login(ctx context.Context, target string) int {
	accts, err := registry.Load(a.Paths)
	if err != nil {
		return a.errorf("%v", err)
	}
	if target == allFlag {
		return a.loginAll(ctx, accts)
	}
	acct, ok := registry.Find(accts, target)
	if !ok {
		return a.errorf("unknown account %q", target)
	}
	return a.loginOne(ctx, acct)
}

func (a *App) loginAll(ctx context.Context, accts []registry.Account) int {
	code := ExitOK
	attempted := false
	for _, acct := range accts {
		if ctx.Err() != nil {
			return a.cancelled()
		}
		st, err := auth.GetStatus(ctx, a.Runner, a.Paths, acct)
		if ctx.Err() != nil {
			return a.cancelled()
		}
		if err != nil {
			a.warnf("%s: status check failed", acct.Name)
		} else if st.LoggedIn && (acct.Email == "" || strings.EqualFold(st.Email, acct.Email)) {
			continue
		}
		attempted = true
		if c := a.loginOne(ctx, acct); c > code {
			code = c
		}
		if ctx.Err() != nil {
			return a.cancelled()
		}
	}
	if !attempted {
		a.printf("All accounts are logged in.\n")
	}
	return code
}

// cancelled reports an interrupted multi-account login.
func (a *App) cancelled() int {
	a.printf("Cancelled.\n")
	return ExitInterrupted
}

package cli

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"text/tabwriter"

	"github.com/pratikbhadane24/multiclaude/internal/auth"
	"github.com/pratikbhadane24/multiclaude/internal/registry"
)

const listPad = 2

// cmdList prints every account with its dir, email and login state.
func (a *App) cmdList(ctx context.Context, args []string) int {
	if _, code, ok := a.parseArgs(a.newFlags("list"), args, 0, "list"); !ok {
		return code
	}
	accts, err := registry.Load(a.Paths)
	if err != nil {
		return a.errorf("%v", err)
	}
	results := a.loginResults(ctx, accts)
	tw := tabwriter.NewWriter(a.Out, 0, 0, listPad, ' ', 0)
	fmt.Fprintln(tw, "NAME\tDIR\tEMAIL\tPLAN\tLOGGED IN")
	for i, acct := range accts {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", acct.Name, a.accountDir(acct), dash(listEmail(acct, results[i])),
			dash(planText(results[i])), loginText(acct, results[i]))
	}
	if err := tw.Flush(); err != nil {
		return a.errorf("write list: %v", err)
	}
	return ExitOK
}

// loginResults checks every account concurrently, preserving order.
func (a *App) loginResults(ctx context.Context, accts []registry.Account) []loginResult {
	out := make([]loginResult, len(accts))
	var wg sync.WaitGroup
	for i, acct := range accts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = a.statusOf(ctx, acct)
		}()
	}
	wg.Wait()
	return out
}

type loginResult struct {
	st  auth.Status
	err error
}

func (a *App) statusOf(ctx context.Context, acct registry.Account) loginResult {
	st, err := auth.GetStatus(ctx, a.Runner, a.Paths, acct)
	return loginResult{st, err}
}

// listEmail prefers the registry email and falls back to the logged-in one.
func listEmail(acct registry.Account, r loginResult) string {
	if acct.Email != "" || r.err != nil || !r.st.LoggedIn {
		return acct.Email
	}
	return r.st.Email
}

func planText(r loginResult) string {
	if r.err != nil || !r.st.LoggedIn {
		return ""
	}
	return r.st.SubscriptionType
}

func loginText(acct registry.Account, r loginResult) string {
	switch {
	case r.err != nil:
		return "unknown"
	case !r.st.LoggedIn:
		return "no"
	case acct.Email != "" && !strings.EqualFold(acct.Email, r.st.Email):
		return "yes (as " + r.st.Email + ")"
	}
	return "yes"
}

// accountDir is the account's config dir (main lives in ~/.claude).
func (a *App) accountDir(acct registry.Account) string {
	if acct.IsMain() {
		return a.Paths.ClaudeDir
	}
	return acct.Dir
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"

	"github.com/pratikbhadane24/multiclaude/internal/consts"
	"github.com/pratikbhadane24/multiclaude/internal/handoff"
	"github.com/pratikbhadane24/multiclaude/internal/registry"
	"github.com/pratikbhadane24/multiclaude/internal/status"
	"github.com/pratikbhadane24/multiclaude/internal/usage"
)

// getwd is the working directory source; tests replace it.
var getwd = os.Getwd

// sessionIDRE restricts --session to a plain transcript id.
var sessionIDRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// cmdContinue resumes the latest conversation of this folder on another
// account. History is shared by all accounts, so nothing is copied.
func (a *App) cmdContinue(ctx context.Context, args []string) int {
	fs := a.newFlags("continue")
	fromName := fs.String("from", "", "account the conversation was running on (default: $CCM_ACCOUNT, else the most limited)")
	toName := fs.String("to", "", "account to continue on (default: the one with most headroom)")
	sessionID := fs.String("session", "", "session id (default: latest for this folder)")
	if _, code, ok := a.parseArgs(fs, args, 0, "continue [--from a] [--to b] [--session id]"); !ok {
		return code
	}
	if *sessionID != "" && !sessionIDRE.MatchString(*sessionID) {
		return a.usageErr("continue: --session must be a session id")
	}
	accts, err := registry.Load(a.Paths)
	if err != nil {
		return a.errorf("%v", err)
	}
	if *fromName == "" {
		*fromName = os.Getenv("CCM_ACCOUNT")
	}
	rows := a.collectUsage(ctx, accts, *fromName == "" || *toName == "")
	from, code, ok := a.continueFrom(accts, rows, *fromName)
	if !ok {
		return code
	}
	id, code, ok := a.continueSession(accts, from, *sessionID)
	if !ok {
		return code
	}
	to, code, ok := a.continueTarget(accts, rows, from, *toName)
	if !ok {
		return code
	}
	a.printf("Continuing %s on %s (from %s)\n", id, to.Name, from.Name)
	return a.runAccount(ctx, to, []string{"--resume", id})
}

// collectUsage fetches usage for every account when need is true.
func (a *App) collectUsage(ctx context.Context, accts []registry.Account, need bool) []usage.AccountUsage {
	if !need {
		return nil
	}
	cf := a.cachedFetch(false)
	d := status.Deps{Accounts: accts, Fetch: cf.Fetch, Paths: a.Paths, Now: a.Now, EmailFallback: a.authEmail, ShowDuplicates: true}
	rows, _ := status.Collect(ctx, d)
	a.warnCacheErr(cf)
	list := make([]usage.AccountUsage, len(rows))
	for i, r := range rows {
		list[i] = r.AccountUsage
	}
	return list
}

func (a *App) continueFrom(accts []registry.Account, list []usage.AccountUsage, name string) (registry.Account, int, bool) {
	if name == "" {
		name = mostLimited(list)
	}
	from, ok := registry.Find(accts, name)
	if !ok {
		return registry.Account{}, a.errorf("unknown account %q", name), false
	}
	return from, ExitOK, true
}

// mostLimited names the account with the highest known usage (default main).
func mostLimited(list []usage.AccountUsage) string {
	name, worst := consts.MainName, -1.0
	for _, u := range list {
		if u.Usage == nil || (u.State != usage.StateOK && u.State != usage.StateExpiredRefreshed) {
			continue
		}
		for _, w := range []usage.Window{u.Usage.FiveHour, u.Usage.SevenDay} {
			if w.UsedPct != nil && *w.UsedPct > worst {
				name, worst = u.Account, *w.UsedPct
			}
		}
	}
	return name
}

// continueSession returns the explicit session id or the newest one for cwd,
// looked up in main's projects dir (shared by every account).
func (a *App) continueSession(accts []registry.Account, from registry.Account, explicit string) (string, int, bool) {
	if explicit != "" {
		return explicit, ExitOK, true
	}
	id, err := a.latestSessionID(accts)
	if err != nil {
		return "", a.errorf("%v", err), false
	}
	return id, ExitOK, true
}

// errNoConversation is returned when shared history has no session for cwd.
var errNoConversation = errors.New("no conversation found for this folder in shared history")

// latestSessionID is the newest session for the working directory, looked up
// in main's projects dir (shared by every account). Used by `ccm continue`
// and the dashboard's continue prompt.
func (a *App) latestSessionID(accts []registry.Account) (string, error) {
	cwd, err := getwd()
	if err != nil {
		return "", fmt.Errorf("current directory: %w", err)
	}
	mainAcct, found := registry.Find(accts, consts.MainName)
	if !found {
		mainAcct = registry.Account{Name: consts.MainName}
	}
	live := handoff.LiveSessions(a.Paths, accts)
	s, err := handoff.FindLatestExcluding(a.Paths, mainAcct, cwd, live)
	var liveErr *handoff.LiveError
	if errors.As(err, &liveErr) {
		return "", liveErr
	}
	if errors.Is(err, handoff.ErrNoSession) {
		return "", errNoConversation
	}
	if err != nil {
		return "", fmt.Errorf("find latest session: %w", err)
	}
	return s.ID, nil
}

// dashLatestSession is latestSessionID with a fresh registry read.
func (a *App) dashLatestSession() (string, error) {
	accts, err := registry.Load(a.Paths)
	if err != nil {
		return "", err
	}
	return a.latestSessionID(accts)
}

// loginGroup names the accounts signed in as the same login as from (from
// excluded); usage identity decides, so it is empty without usage data.
func loginGroup(list []usage.AccountUsage, from string) map[string]bool {
	canon := usage.CanonicalIndex(list)
	fromCanon := -1
	for i, u := range list {
		if u.Account == from {
			fromCanon = canon[i]
		}
	}
	group := map[string]bool{}
	for i, u := range list {
		if fromCanon >= 0 && canon[i] == fromCanon && u.Account != from {
			group[u.Account] = true
		}
	}
	return group
}

func (a *App) continueTarget(accts []registry.Account, list []usage.AccountUsage, from registry.Account, name string) (registry.Account, int, bool) {
	sameGroup := loginGroup(list, from.Name)
	if name != "" {
		to, ok := registry.Find(accts, name)
		if !ok {
			return registry.Account{}, a.errorf("unknown account %q", name), false
		}
		if to.Name == from.Name {
			return registry.Account{}, a.errorf("--to and --from are the same account (%s)", from.Name), false
		}
		if sameGroup[to.Name] {
			return registry.Account{}, a.errorf("--to %s is signed in as the same login as --from %s", to.Name, from.Name), false
		}
		return to, ExitOK, true
	}
	others := make([]usage.AccountUsage, 0, len(list))
	for _, u := range list {
		if u.Account != from.Name && !sameGroup[u.Account] {
			others = append(others, u)
		}
	}
	best, found := status.PickBest(others, a.Now())
	if !found {
		fmt.Fprintln(a.Err, "ccm: no free account to continue on")
		return registry.Account{}, exitAllLimited, false
	}
	to, _ := registry.Find(accts, best.Account)
	return to, ExitOK, true
}

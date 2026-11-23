package cli

import (
	"fmt"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/consts"
	"github.com/pratikbhadane24/multiclaude/internal/history"
	"github.com/pratikbhadane24/multiclaude/internal/paths"
	"github.com/pratikbhadane24/multiclaude/internal/registry"
)

// nowOrReal is the app clock, or time.Now when unset.
func (a *App) nowOrReal() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// migrateHistory merges the account's own history dirs into the shared copy in
// main and links them. An account with a running Claude session is skipped
// with a warning. It prints a summary when anything was merged and reports
// whether every item succeeded.
func (a *App) migrateHistory(acct registry.Account) bool {
	if !history.NeedsMerge(acct.Dir) {
		return true
	}
	if why := history.LiveReason(acct.Dir, a.nowOrReal()); why != "" {
		a.warnf("%s has a running Claude session (%s); close it and run ccm sync to share its history", acct.Name, why)
		return true
	}
	ok := true
	var total history.MergeResult
	for _, item := range consts.HistoryItems {
		res, err := history.MergeIntoMain(a.Paths, acct.Dir, item, a.nowOrReal())
		total.Moved += res.Moved
		total.Conflicts += res.Conflicts
		if err != nil {
			a.warnf("%s: merge %s into shared history: %v", acct.Name, item, err)
			ok = false
		}
	}
	if total.Moved > 0 || total.Conflicts > 0 {
		a.printf("%s: merged %d chats into shared history (%d conflicts kept as *.from-%s-*)\n",
			acct.Name, total.Moved, total.Conflicts, acct.Name)
	}
	return ok
}

// guardedMerge is the doctor --fix merge: same as MergeIntoMain, but refuses
// while the account has a running Claude session.
func (a *App) guardedMerge(p paths.Paths, dir, item string, now time.Time) (history.MergeResult, error) {
	if why := history.LiveReason(dir, now); why != "" {
		return history.MergeResult{}, fmt.Errorf("account has a running Claude session (%s); close it and retry", why)
	}
	return history.MergeIntoMain(p, dir, item, now)
}

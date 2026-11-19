package cli

import (
	"time"

	"multiclaude/internal/consts"
	"multiclaude/internal/history"
	"multiclaude/internal/registry"
)

// migrateHistory merges the account's own history dirs into the shared copy in
// main and links them. It prints a summary when anything was merged and
// reports whether every item succeeded.
func (a *App) migrateHistory(acct registry.Account) bool {
	ok := true
	now := time.Now
	if a.Now != nil {
		now = a.Now
	}
	var total history.MergeResult
	for _, item := range consts.HistoryItems {
		res, err := history.MergeIntoMain(a.Paths, acct.Dir, item, now())
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

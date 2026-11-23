package doctor

import (
	"context"

	"github.com/pratikbhadane24/multiclaude/internal/usage"
)

const duplicateCheckName = "duplicate login"

// duplicateChecks reports accounts signed in as the same login as an earlier
// one. They are informational: ccm merges them into one row by itself.
func duplicateChecks(ctx context.Context, d Deps) []Check {
	if d.Identity == nil {
		return nil
	}
	list := make([]usage.AccountUsage, len(d.Accounts))
	for i, a := range d.Accounts {
		uuid, email := d.Identity(ctx, a)
		state := usage.StateOK
		if uuid == "" && email == "" {
			state = usage.StateLoggedOut // unknown login never merges
		}
		list[i] = usage.AccountUsage{Account: a.Name, Email: email, AccountUUID: uuid, State: state}
	}
	var out []Check
	for i, canon := range usage.CanonicalIndex(list) {
		if canon == i {
			continue
		}
		out = append(out, Check{
			Account: list[i].Account,
			Name:    duplicateCheckName,
			Status:  StatusInfo,
			Detail:  "same login as " + list[canon].Account + " — shown as one row",
		})
	}
	return out
}

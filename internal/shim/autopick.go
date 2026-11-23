package shim

import (
	"context"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/config"
	"github.com/pratikbhadane24/multiclaude/internal/keychain"
	"github.com/pratikbhadane24/multiclaude/internal/paths"
	"github.com/pratikbhadane24/multiclaude/internal/registry"
	"github.com/pratikbhadane24/multiclaude/internal/run"
	"github.com/pratikbhadane24/multiclaude/internal/status"
)

const accountEnv = "CCM_ACCOUNT"

// presentFn reports whether an account has a login (Keychain or file).
// A variable so tests can stub it.
var presentFn = func(a registry.Account) bool {
	ok, err := keychain.Present(context.Background(), run.OSRunner{}, a)
	return err == nil && ok
}

// AutoPick returns env with CLAUDE_CONFIG_DIR and CCM_ACCOUNT set to the
// non-main account with the most headroom, when the config asks for it
// ("defaultAccount": "best") and no CLAUDE_CONFIG_DIR is set. It decides
// from the usage cache only (no network); with no usable cache it takes
// the first logged-in account. Otherwise, or on any error, env is returned
// unchanged. The input slice is never modified.
func AutoPick(p paths.Paths, env []string, now time.Time) []string {
	if lookupEnv(env, configDirEnv) != "" {
		return env
	}
	cfg, err := config.Load(p)
	if err != nil || cfg.DefaultAccount != config.DefaultAccountBest {
		return env
	}
	accts, err := registry.Load(p)
	if err != nil {
		return env
	}
	a, ok := pick(p, nonMain(accts), now)
	if !ok {
		return env
	}
	out := make([]string, 0, len(env)+2)
	out = append(out, env...)
	return append(out, configDirEnv+"="+a.Dir, accountEnv+"="+a.Name)
}

// pick chooses from the cache, falling back to the first logged-in account.
func pick(p paths.Paths, accts []registry.Account, now time.Time) (registry.Account, bool) {
	if c, err := status.ReadCache(p); err == nil {
		if choice := status.PickSwitch(accts, c, nil, now); choice.Found && presentFn(choice.Account) {
			return choice.Account, true
		}
	}
	for _, a := range accts {
		if presentFn(a) {
			return a, true
		}
	}
	return registry.Account{}, false
}

func nonMain(accts []registry.Account) []registry.Account {
	out := make([]registry.Account, 0, len(accts))
	for _, a := range accts {
		if !a.IsMain() && a.Dir != "" {
			out = append(out, a)
		}
	}
	return out
}

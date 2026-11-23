package shim

import (
	"context"
	"strings"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/config"
	"github.com/pratikbhadane24/multiclaude/internal/keychain"
	"github.com/pratikbhadane24/multiclaude/internal/paths"
	"github.com/pratikbhadane24/multiclaude/internal/registry"
	"github.com/pratikbhadane24/multiclaude/internal/run"
	"github.com/pratikbhadane24/multiclaude/internal/status"
)

const accountEnv = "CCM_ACCOUNT"

// presentTimeout bounds each login check so a stalled securityd can
// never hold up launching claude.
const presentTimeout = 2 * time.Second

// presentFn reports whether an account has a login (Keychain or file).
// A variable so tests can stub it.
var presentFn = func(a registry.Account) bool {
	return presentWith(run.OSRunner{}, a, presentTimeout)
}

// presentWith checks a's login with r, treating a timeout as no login.
func presentWith(r run.Runner, a registry.Account, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ok, err := keychain.Present(ctx, r, a)
	return err == nil && ok
}

// AutoPick returns env with CLAUDE_CONFIG_DIR and CCM_ACCOUNT set to the
// non-main account with the most headroom, when the config asks for it
// ("defaultAccount": "best") and neither CLAUDE_CONFIG_DIR nor CCM_ACCOUNT
// is set (ccm sets CCM_ACCOUNT whenever it chose the account, main too). It decides
// from the usage cache only (no network); with no usable cache it takes
// the first logged-in account. Otherwise, or on any error, env is returned
// unchanged. The input slice is never modified.
func AutoPick(p paths.Paths, env []string, now time.Time) []string {
	if lookupEnv(env, configDirEnv) != "" || lookupEnv(env, accountEnv) != "" {
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
	for _, e := range env {
		if !strings.HasPrefix(e, configDirEnv+"=") && !strings.HasPrefix(e, accountEnv+"=") {
			out = append(out, e)
		}
	}
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

package cli

import (
	"github.com/pratikbhadane24/multiclaude/internal/browser"
	"github.com/pratikbhadane24/multiclaude/internal/registry"
)

// SetBrowser stores how login links are presented for the named account.
func (a *App) SetBrowser(name, mode string) int {
	parsed, err := browser.ParseMode(mode)
	if err != nil {
		return a.errorf("%v", err)
	}
	accts, err := registry.Load(a.Paths)
	if err != nil {
		return a.errorf("%v", err)
	}
	acct, ok := registry.Find(accts, name)
	if !ok {
		return a.errorf("unknown account %q", name)
	}
	if acct.IsMain() {
		return a.errorf("the main account has no registry entry; login browser can only be set for added accounts")
	}
	acct.Browser = parsed.String()
	if err := registry.Save(a.Paths, registry.With(accts, acct)); err != nil {
		return a.errorf("%v", err)
	}
	a.printf("Login browser for %q: %s\n", name, parsed)
	return ExitOK
}

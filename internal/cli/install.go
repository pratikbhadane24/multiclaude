package cli

import (
	"context"
	"os"
	"path/filepath"

	"github.com/pratikbhadane24/multiclaude/internal/install"
)

// localBinDir is where the ccm symlink goes, relative to the home dir.
const localBinDir = ".local/bin"

// cmdInstall copies the shim into place and links ccm into ~/.local/bin.
func (a *App) cmdInstall(_ context.Context, args []string) int {
	if _, code, ok := a.parseArgs(a.newFlags("install"), args, 0, "install"); !ok {
		return code
	}
	exe, err := os.Executable()
	if err != nil {
		return a.errorf("locate ccm executable: %v", err)
	}
	res, err := install.Install(a.Paths, exe, filepath.Join(a.Paths.Home, localBinDir))
	if err != nil {
		return a.errorf("install: %v", err)
	}
	a.printf("Shim: %s (%s)\n", res.ShimPath, choose(res.ShimUpdated, "installed", "up to date"))
	a.printf("Link: %s (%s)\n", res.CCMLink, choose(res.LinkCreated, "created", "already present"))
	a.printf("%s", install.NextSteps(a.Paths.AccountsRoot))
	return ExitOK
}

func choose(cond bool, yes, no string) string {
	if cond {
		return yes
	}
	return no
}

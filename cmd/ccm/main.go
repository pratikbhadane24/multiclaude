// Command ccm manages multiple Claude Code accounts.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/browser"
	"github.com/pratikbhadane24/multiclaude/internal/cli"
	"github.com/pratikbhadane24/multiclaude/internal/paths"
	"github.com/pratikbhadane24/multiclaude/internal/run"
)

const httpTimeout = 10 * time.Second

// selfExe resolves the running ccm binary; "" when it cannot be determined.
func selfExe() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return ""
	}
	return resolved
}

func main() {
	// Invoked by claude as $BROWSER with a login URL: handle before anything else.
	if browser.IsOpenURLInvocation(os.Args[1:], os.Getenv) {
		os.Exit(browser.HandleOpenURL(os.Args[1], os.Getenv, run.OS, os.Stderr))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ccm: home dir: %v\n", err)
		os.Exit(cli.ExitError)
	}
	ctx, stop := context.Background(), func() {}
	if cli.HandlesInterrupt(os.Args[1:]) {
		ctx, stop = signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	}
	app := &cli.App{
		Paths:  paths.Resolve(os.Getenv, home),
		Runner: run.OS,
		HTTP:   &http.Client{Timeout: httpTimeout},
		Now:    time.Now,
		Out:    os.Stdout,
		Err:    os.Stderr,
		In:     os.Stdin,

		SelfExe: selfExe(),
	}
	code := app.Main(ctx, os.Args[1:])
	stop()
	os.Exit(code)
}

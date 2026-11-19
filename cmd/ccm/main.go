// Command ccm manages multiple Claude Code accounts.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"multiclaude/internal/cli"
	"multiclaude/internal/paths"
	"multiclaude/internal/run"
)

const httpTimeout = 10 * time.Second

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ccm: home dir: %v\n", err)
		os.Exit(cli.ExitError)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	app := &cli.App{
		Paths:  paths.Resolve(os.Getenv, home),
		Runner: run.OS,
		HTTP:   &http.Client{Timeout: httpTimeout},
		Now:    time.Now,
		Out:    os.Stdout,
		Err:    os.Stderr,
		In:     os.Stdin,
	}
	code := app.Main(ctx, os.Args[1:])
	stop()
	os.Exit(code)
}

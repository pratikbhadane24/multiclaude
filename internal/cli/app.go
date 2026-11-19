// Package cli implements the ccm subcommands.
package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"multiclaude/internal/paths"
	"multiclaude/internal/run"
)

// Exit codes.
const (
	ExitOK       = 0
	ExitError    = 1
	ExitMismatch = 2
	// ExitInterrupted is the shell convention for SIGINT (128+2).
	ExitInterrupted = 130
)

// App holds shared dependencies for all commands.
type App struct {
	Paths  paths.Paths
	Runner run.Runner
	HTTP   *http.Client
	Now    func() time.Time
	Out    io.Writer
	Err    io.Writer
	In     io.Reader
	// SelfExe is the ccm binary used as $BROWSER during login; empty disables link handling.
	SelfExe string
}

func (a *App) errorf(format string, args ...any) int {
	fmt.Fprintf(a.Err, "ccm: "+format+"\n", args...)
	return ExitError
}

func (a *App) warnf(format string, args ...any) {
	fmt.Fprintf(a.Err, "ccm: warning: "+format+"\n", args...)
}

func (a *App) printf(format string, args ...any) {
	fmt.Fprintf(a.Out, format, args...)
}

// promptLine writes prompt to Out and reads one trimmed line from In.
// It returns ctx.Err() as soon as ctx is cancelled, even while blocked on input.
func (a *App) promptLine(ctx context.Context, prompt string) (string, error) {
	fmt.Fprint(a.Out, prompt)
	if a.In == nil {
		return "", io.EOF
	}
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := bufio.NewReader(a.In).ReadString('\n')
		ch <- result{line, err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-ch:
		if r.err != nil && r.line == "" {
			return "", r.err
		}
		return strings.TrimSpace(r.line), nil
	}
}

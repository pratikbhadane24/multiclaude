// Package cli implements the ccm subcommands.
package cli

import (
	"bufio"
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
func (a *App) promptLine(prompt string) (string, error) {
	fmt.Fprint(a.Out, prompt)
	if a.In == nil {
		return "", io.EOF
	}
	line, err := bufio.NewReader(a.In).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

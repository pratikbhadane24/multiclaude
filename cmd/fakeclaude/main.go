// Command fakeclaude is a stand-in for the claude binary used by ccm's
// end-to-end tests. It never touches the network or the Keychain.
//
// Environment:
//
//	FAKECLAUDE_LOG             JSON-lines file recording every invocation
//	FAKECLAUDE_LIMIT_ACCOUNTS  comma list of account dir basenames ("main" for
//	                           unset CLAUDE_CONFIG_DIR) that hit the limit
//	FAKECLAUDE_LIMIT_AFTER     seconds before an interactive session is limited (default 2)
package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	defaultLimitAfter = 2 * time.Second
	okRunTime         = time.Second
	exitSigint        = 0
	exitSigterm       = 143
	exitFailure       = 1
	fileMode          = 0o600
	dirMode           = 0o700
	mainAccount       = "main"
)

// env is the process environment fakeclaude reacts to.
type env struct {
	Log, ConfigDir, Home, Cwd, Account string
	Limited                            bool
	LimitAfter                         time.Duration
}

func accountName(configDir string) string {
	if configDir == "" {
		return mainAccount
	}
	return filepath.Base(filepath.Clean(configDir))
}

func loadEnv(getenv func(string) string, home, cwd string) env {
	cfg := getenv("CLAUDE_CONFIG_DIR")
	e := env{
		Log: getenv("FAKECLAUDE_LOG"), ConfigDir: cfg, Home: home, Cwd: cwd,
		Account: accountName(cfg), LimitAfter: defaultLimitAfter,
	}
	for _, a := range strings.Split(getenv("FAKECLAUDE_LIMIT_ACCOUNTS"), ",") {
		if a = strings.TrimSpace(a); a != "" && a == e.Account {
			e.Limited = true
		}
	}
	if s, err := strconv.ParseFloat(getenv("FAKECLAUDE_LIMIT_AFTER"), 64); err == nil && s >= 0 {
		e.LimitAfter = time.Duration(s * float64(time.Second))
	}
	return e
}

type invocation struct {
	Time      string   `json:"time"`
	PID       int      `json:"pid"`
	Argv      []string `json:"argv"`
	ConfigDir string   `json:"config_dir"`
	Account   string   `json:"account"`
	Cwd       string   `json:"cwd"`
	Mode      string   `json:"mode"`
	SessionID string   `json:"session_id"`
	Resume    bool     `json:"resume"`
	Prompt    string   `json:"prompt"`
}

// appendLine appends one JSON line to path, creating parent directories.
func appendLine(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, fileMode)
	if err != nil {
		return err
	}
	if _, err := f.Write(marshalLine(v)); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

const (
	modeInteractive = "interactive"
	modeStream      = "stream-json"
	modeSubcommand  = "subcommand"
)

// subcommands are claude commands that are not sessions (ccm may call them
// for housekeeping, e.g. `claude auth status`); they exit immediately.
var subcommands = map[string]bool{
	"auth": true, "mcp": true, "config": true, "doctor": true, "update": true,
	"--version": true, "-v": true,
}

func modeOf(args []string, o options) string {
	switch {
	case len(args) > 0 && subcommands[args[0]]:
		return modeSubcommand
	case o.isStream():
		return modeStream
	}
	return modeInteractive
}

// recordUserMessage logs a user message received in stream-json mode so tests
// can verify what was delivered.
func recordUserMessage(e env, id, text string, now time.Time) error {
	if e.Log == "" {
		return nil
	}
	return appendLine(e.Log, map[string]any{
		"event": "user_message", "time": stamp(now), "pid": os.Getpid(),
		"account": e.Account, "session_id": id, "text": text,
	})
}

// run executes fakeclaude and returns its exit code.
func run(args []string, e env, stdin io.Reader, stdout, stderr io.Writer, sigs <-chan os.Signal, now func() time.Time) int {
	o := parseArgs(args)
	if o.SessionID == "" {
		o.SessionID = newUUID()
	}
	mode := modeOf(args, o)
	if e.Log != "" {
		err := appendLine(e.Log, invocation{
			Time: stamp(now()), PID: os.Getpid(), Argv: args, ConfigDir: e.ConfigDir,
			Account: e.Account, Cwd: e.Cwd, Mode: mode, SessionID: o.SessionID,
			Resume: o.IsResume, Prompt: o.Prompt,
		})
		if err != nil {
			fmt.Fprintf(stderr, "fakeclaude: log: %v\n", err)
			return exitFailure
		}
	}
	switch mode {
	case modeSubcommand:
		fmt.Fprintln(stdout, "fakeclaude 0.0.0 (fake)")
		return 0
	case modeStream:
		onUser := func(text string) error { return recordUserMessage(e, o.SessionID, text, now()) }
		if err := runStream(stdin, stdout, e, o.SessionID, now, onUser); err != nil {
			fmt.Fprintf(stderr, "fakeclaude: stream: %v\n", err)
			return exitFailure
		}
		return 0
	}
	code, err := runInteractive(o, e, stdout, sigs, now)
	if err != nil {
		fmt.Fprintf(stderr, "fakeclaude: %v\n", err)
	}
	return code
}

func exitForSignal(s os.Signal) int {
	if s == syscall.SIGTERM {
		return exitSigterm
	}
	return exitSigint
}

// runInteractive mimics a claude TUI session: it records the prompt, then
// either succeeds after a second or hits the limit and waits for a signal.
func runInteractive(o options, e env, stdout io.Writer, sigs <-chan os.Signal, now func() time.Time) (int, error) {
	path := transcriptPath(e.ConfigDir, e.Home, e.Cwd, o.SessionID)
	if err := appendLine(path, userLine(o.Prompt, o.SessionID, e.Cwd, now())); err != nil {
		return exitFailure, fmt.Errorf("transcript: %w", err)
	}
	if !e.Limited {
		return interactiveOK(o, e, path, stdout, sigs, now)
	}
	select {
	case s := <-sigs:
		return exitForSignal(s), nil
	case <-time.After(e.LimitAfter):
	}
	if err := appendLine(path, limitLine(o.SessionID, e.Cwd, now())); err != nil {
		return exitFailure, fmt.Errorf("transcript: %w", err)
	}
	fmt.Fprintln(stdout, limitText)
	return exitForSignal(<-sigs), nil
}

func interactiveOK(o options, e env, path string, stdout io.Writer, sigs <-chan os.Signal, now func() time.Time) (int, error) {
	text := fmt.Sprintf("fakeclaude ok on %s, prompt: %s", e.Account, o.Prompt)
	fmt.Fprintln(stdout, text)
	if err := appendLine(path, assistantLine(text, o.SessionID, e.Cwd, now())); err != nil {
		return exitFailure, fmt.Errorf("transcript: %w", err)
	}
	select {
	case s := <-sigs:
		return exitForSignal(s), nil
	case <-time.After(okRunTime):
		return 0, nil
	}
}

func main() {
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	e := loadEnv(os.Getenv, home, cwd)
	os.Exit(run(os.Args[1:], e, os.Stdin, os.Stdout, os.Stderr, sigs, time.Now))
}

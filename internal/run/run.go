// Package run abstracts process execution so it can be faked in tests.
package run

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
)

// Result is the outcome of a process that ran to completion.
type Result struct {
	Code           int
	Stdout, Stderr string
}

// Runner executes external commands. A non-zero exit is not an error.
type Runner interface {
	Run(ctx context.Context, name string, args []string, env []string, inherit bool) (Result, error)
}

// OSRunner runs real processes via os/exec.
type OSRunner struct{}

// OS is the real Runner.
var OS Runner = OSRunner{}

// Run executes name. env == nil inherits the process env; inherit wires stdio to the terminal.
func (OSRunner) Run(ctx context.Context, name string, args []string, env []string, inherit bool) (Result, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	if inherit {
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	} else {
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
	}
	err := cmd.Run()
	res := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		res.Code = exitCode(exitErr)
		return res, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("run %s: %w", name, err)
	}
	return res, nil
}

// signalExitBase is the shell convention for a child killed by a signal.
const signalExitBase = 128

func exitCode(e *exec.ExitError) int {
	if ws, ok := e.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return signalExitBase + int(ws.Signal())
	}
	return e.ExitCode()
}

// Call records one invocation made through Fake.
type Call struct {
	Name    string
	Args    []string
	Env     []string
	Inherit bool
}

// Fake is a test Runner that records calls and answers via Respond.
type Fake struct {
	Calls   []Call
	Respond func(Call) (Result, error)
	mu      sync.Mutex
}

// Run records the call and returns Respond(call), or a zero Result if Respond is nil.
func (f *Fake) Run(_ context.Context, name string, args []string, env []string, inherit bool) (Result, error) {
	call := Call{Name: name, Args: append([]string(nil), args...), Env: append([]string(nil), env...), Inherit: inherit}
	f.mu.Lock()
	f.Calls = append(f.Calls, call)
	f.mu.Unlock()
	if f.Respond == nil {
		return Result{}, nil
	}
	return f.Respond(call)
}

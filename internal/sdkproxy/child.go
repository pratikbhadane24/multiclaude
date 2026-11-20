package sdkproxy

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Child is a running claude process as the proxy sees it.
type Child interface {
	Stdin() io.WriteCloser
	Stdout() io.Reader
	Signal(sig os.Signal) error
	// Wait blocks until the process exits and returns its exit code
	// (128+signal when killed by a signal).
	Wait() int
}

// Starter starts claude at argv0 with argv (argv[0] is the program name)
// and env.
type Starter func(argv0 string, argv, env []string) (Child, error)

// Clock is the time source; tests inject a fake.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// RealClock is the wall clock.
type RealClock struct{}

// Now returns time.Now().
func (RealClock) Now() time.Time { return time.Now() }

// After returns time.After(d).
func (RealClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// execChild is a Child backed by os/exec. Stdout is a plain os.Pipe so Wait
// may run while the reader is still draining.
type execChild struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *os.File
}

// ExecStarter starts real processes; stderr is inherited.
func ExecStarter(stderr io.Writer) Starter {
	return func(argv0 string, argv, env []string) (Child, error) {
		pr, pw, err := os.Pipe()
		if err != nil {
			return nil, err
		}
		cmd := &exec.Cmd{Path: argv0, Args: argv, Env: env, Stdout: pw, Stderr: stderr}
		stdin, err := cmd.StdinPipe()
		if err == nil {
			err = cmd.Start()
		}
		_ = pw.Close()
		if err != nil {
			_ = pr.Close()
			return nil, err
		}
		return &execChild{cmd: cmd, stdin: stdin, stdout: pr}, nil
	}
}

func (c *execChild) Stdin() io.WriteCloser { return c.stdin }
func (c *execChild) Stdout() io.Reader     { return c.stdout }

func (c *execChild) Signal(sig os.Signal) error { return c.cmd.Process.Signal(sig) }

func (c *execChild) Wait() int {
	err := c.cmd.Wait()
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return 1
	}
	if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return ee.ExitCode()
}

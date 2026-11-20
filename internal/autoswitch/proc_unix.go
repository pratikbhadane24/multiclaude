//go:build unix

package autoswitch

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"unsafe"

	"github.com/charmbracelet/x/term"
)

const (
	signalExitBase = 128
	// showCursor undoes a hidden cursor left by a killed TUI.
	showCursor = "\x1b[?25h"
)

// Stdio is the terminal the child inherits.
type Stdio struct {
	In       *os.File
	Out, Err io.Writer
}

// OSStdio is the process's own stdio.
func OSStdio() Stdio { return Stdio{In: os.Stdin, Out: os.Stdout, Err: os.Stderr} }

// osProc is a real child in its own process group.
type osProc struct {
	cmd     *exec.Cmd
	restore func()
}

// StartProcess runs bin in its own process group with std's stdio. When stdin
// is the terminal and ccm is in its foreground, the child's group becomes the
// foreground (so Ctrl-C reaches only claude); Wait gives the terminal back and
// restores its saved mode.
func StartProcess(std Stdio, bin string, args, env []string) (Proc, error) {
	if std.In == nil {
		return nil, errors.New("start: no stdin")
	}
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = std.In, std.Out, std.Err
	fd := int(std.In.Fd())
	owns := ownsTerminal(fd)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Foreground: owns, Ctty: fd}
	var state *term.State
	if owns {
		state, _ = term.GetState(uintptr(fd)) // nil: nothing to restore
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", bin, err)
	}
	restore := func() {}
	if owns {
		restore = func() { reclaimTerminal(fd, state, std.Out) }
	}
	return &osProc{cmd: cmd, restore: restore}, nil
}

// Wait waits for the child, restores the terminal and returns the exit code
// (128+signal when killed by a signal).
func (p *osProc) Wait() int {
	err := p.cmd.Wait()
	p.restore()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return signalExitBase + int(ws.Signal())
		}
		return exitErr.ExitCode()
	}
	if err != nil {
		return 1
	}
	return 0
}

// Signal sends sig to the child's whole process group.
func (p *osProc) Signal(sig syscall.Signal) error {
	return syscall.Kill(-p.cmd.Process.Pid, sig)
}

// ownsTerminal reports whether fd is a terminal whose foreground process
// group is ours.
func ownsTerminal(fd int) bool {
	if !term.IsTerminal(uintptr(fd)) {
		return false
	}
	var pgrp int32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TIOCGPGRP), uintptr(unsafe.Pointer(&pgrp))); errno != 0 {
		return false
	}
	return int(pgrp) == syscall.Getpgrp()
}

// reclaimTerminal makes our process group the foreground again (SIGTTOU is
// ignored meanwhile, as we are in the background) and restores the saved
// terminal mode, in case claude was killed before restoring it.
func reclaimTerminal(fd int, state *term.State, out io.Writer) {
	signal.Ignore(syscall.SIGTTOU)
	defer signal.Reset(syscall.SIGTTOU)
	pgrp := int32(syscall.Getpgrp())
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TIOCSPGRP), uintptr(unsafe.Pointer(&pgrp)))
	if state != nil {
		_ = term.Restore(uintptr(fd), state)
	}
	if out != nil {
		fmt.Fprint(out, showCursor)
	}
}

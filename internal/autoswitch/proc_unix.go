//go:build unix

package autoswitch

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
)

const signalExitBase = 128

// Stdio is the terminal the child inherits.
type Stdio struct {
	In       *os.File
	Out, Err io.Writer
}

// OSStdio is the process's own stdio.
func OSStdio() Stdio { return Stdio{In: os.Stdin, Out: os.Stdout, Err: os.Stderr} }

// jobControl handles terminal ownership around a child in its own process
// group.
type jobControl interface {
	// Stopped runs when the child's process group stopped (Ctrl-Z, SIGTTIN,
	// SIGTTOU); it returns once the child may be continued.
	Stopped(pgid int)
	// Done runs once the child has exited.
	Done()
}

// osProc is a real child in its own process group.
type osProc struct {
	pid    int
	jobs   jobControl
	copies sync.WaitGroup
}

// StartProcess runs bin in its own process group with std's stdio. When stdin
// is the terminal and ccm is in its foreground, the child's group becomes the
// foreground (so Ctrl-C reaches only claude). Wait passes job control through
// (a stopped claude stops ccm, and fg continues both) and finally gives the
// terminal back and restores its saved mode.
func StartProcess(std Stdio, bin string, args, env []string) (Proc, error) {
	if std.In == nil {
		return nil, errors.New("start: no stdin")
	}
	fd := int(std.In.Fd())
	owns := ownsTerminal(fd)
	return startProcess(std, bin, args, env, owns, newJobControl(fd, owns, std.Out))
}

// startProcess starts bin (the foreground group when foreground) with jobs
// handling its stops.
func startProcess(std Stdio, bin string, args, env []string, foreground bool, jobs jobControl) (Proc, error) {
	path, err := resolveBin(bin)
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", bin, err)
	}
	p := &osProc{jobs: jobs}
	out, closeOut := p.childWriter(std.Out)
	errW, closeErr := out, func() {}
	if !sameWriter(std.Err, std.Out) { // one writer, one copier (as os/exec)
		errW, closeErr = p.childWriter(std.Err)
	}
	attr := &os.ProcAttr{
		Env:   env,
		Files: []*os.File{std.In, out, errW},
		Sys:   &syscall.SysProcAttr{Setpgid: true, Foreground: foreground, Ctty: int(std.In.Fd())},
	}
	proc, err := os.StartProcess(path, append([]string{bin}, args...), attr)
	closeOut()
	closeErr()
	if err != nil {
		p.copies.Wait()
		return nil, fmt.Errorf("start %s: %w", bin, err)
	}
	p.pid = proc.Pid
	_ = proc.Release() // reaped by Wait through wait4
	return p, nil
}

// sameWriter is a == b, false when they are not comparable.
func sameWriter(a, b io.Writer) (same bool) {
	defer func() {
		if recover() != nil {
			same = false
		}
	}()
	return a == b
}

// resolveBin looks bin up in PATH when it has no slash.
func resolveBin(bin string) (string, error) {
	if strings.Contains(bin, "/") {
		return bin, nil
	}
	return exec.LookPath(bin)
}

// childWriter returns the file the child writes w through: w itself when it
// is a file, /dev/null for nil, else a pipe copied into w until the child
// and its descendants close it. The returned func closes the parent's copy
// of the child's end after the start.
func (p *osProc) childWriter(w io.Writer) (*os.File, func()) {
	if w == nil {
		null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return nil, func() {}
		}
		return null, func() { null.Close() }
	}
	if f, ok := w.(*os.File); ok {
		return f, func() {}
	}
	r, wr, err := os.Pipe()
	if err != nil {
		return nil, func() {}
	}
	p.copies.Add(1)
	go func() {
		defer p.copies.Done()
		defer r.Close()
		_, _ = io.Copy(w, r)
	}()
	return wr, func() { wr.Close() }
}

// Wait waits for the child, continuing it after each stop (once jobs lets
// it), then restores the terminal and returns the exit code (128+signal when
// killed by a signal).
func (p *osProc) Wait() int {
	code := p.waitExit()
	p.jobs.Done()
	p.copies.Wait()
	return code
}

// waitExit reaps the child, handing every stop to jobs.
func (p *osProc) waitExit() int {
	for {
		var ws syscall.WaitStatus
		_, err := syscall.Wait4(p.pid, &ws, syscall.WUNTRACED, nil)
		switch {
		case errors.Is(err, syscall.EINTR):
			continue
		case err != nil:
			return 1
		case isStopped(ws):
			p.jobs.Stopped(p.pid)
			_ = syscall.Kill(-p.pid, syscall.SIGCONT)
			continue
		case ws.Signaled():
			return signalExitBase + int(ws.Signal())
		}
		return ws.ExitStatus()
	}
}

// isStopped reports a stopped child. WaitStatus.Stopped is not used: on the
// BSDs (macOS) Go reports a SIGSTOP stop as not stopped.
func isStopped(ws syscall.WaitStatus) bool {
	const statusMask, statusStopped = 0x7f, 0x7f
	return ws&statusMask == statusStopped
}

// Signal sends sig to the child's whole process group.
func (p *osProc) Signal(sig syscall.Signal) error {
	return syscall.Kill(-p.pid, sig)
}

//go:build unix

package autoswitch

import (
	"fmt"
	"io"
	"os/signal"
	"syscall"
	"unsafe"

	"github.com/charmbracelet/x/term"
)

// showCursor undoes a hidden cursor left by a stopped or killed TUI.
const showCursor = "\x1b[?25h"

// newJobControl is terminal job control when fd is a terminal, else none.
func newJobControl(fd int, owns bool, out io.Writer) jobControl {
	if !term.IsTerminal(uintptr(fd)) {
		return noJobs{}
	}
	state, _ := term.GetState(uintptr(fd)) // nil: nothing to restore
	return &ttyJobs{fd: fd, state: state, out: out, childHasTTY: owns}
}

// noJobs is used without a terminal: a stopped child is simply continued.
type noJobs struct{}

func (noJobs) Stopped(int) {}
func (noJobs) Done()       {}

// ttyJobs passes job control through to the shell: when claude stops, ccm
// takes the terminal back, restores its mode and suspends itself (SIGTSTP),
// so the shell regains control; when the shell continues ccm (fg), ccm hands
// the terminal and claude's mode back before claude is continued.
type ttyJobs struct {
	fd    int
	state *term.State
	out   io.Writer
	// childHasTTY is whether claude's group was made the foreground.
	childHasTTY bool
}

// Stopped implements jobControl.
func (j *ttyJobs) Stopped(pgid int) {
	var childState *term.State
	if j.childHasTTY {
		childState, _ = term.GetState(uintptr(j.fd))
		j.reclaim()
	}
	// SIGTSTP rather than SIGSTOP: the kernel discards it when no shell can
	// continue ccm (orphaned process group), so claude just carries on.
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGTSTP)
	if foregroundPgrp(j.fd) != syscall.Getpgrp() {
		return // continued in the background (bg): claude may stop again
	}
	if childState != nil {
		_ = term.Restore(uintptr(j.fd), childState)
	}
	j.childHasTTY = setForeground(j.fd, pgid)
}

// Done implements jobControl.
func (j *ttyJobs) Done() {
	if j.childHasTTY {
		j.reclaim()
	}
}

// reclaim makes our process group the foreground again and restores the
// saved terminal mode, in case claude did not restore it.
func (j *ttyJobs) reclaim() {
	setForeground(j.fd, syscall.Getpgrp())
	if j.state != nil {
		_ = term.Restore(uintptr(j.fd), j.state)
	}
	if j.out != nil {
		fmt.Fprint(j.out, showCursor)
	}
	j.childHasTTY = false
}

// setForeground makes pgid the terminal's foreground group (SIGTTOU is
// ignored meanwhile, in case we are in the background).
func setForeground(fd, pgid int) bool {
	signal.Ignore(syscall.SIGTTOU)
	defer signal.Reset(syscall.SIGTTOU)
	pg := int32(pgid)
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TIOCSPGRP), uintptr(unsafe.Pointer(&pg)))
	return errno == 0
}

// foregroundPgrp is the terminal's foreground process group (-1 on error).
func foregroundPgrp(fd int) int {
	var pgrp int32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TIOCGPGRP), uintptr(unsafe.Pointer(&pgrp))); errno != 0 {
		return -1
	}
	return int(pgrp)
}

// ownsTerminal reports whether fd is a terminal whose foreground process
// group is ours.
func ownsTerminal(fd int) bool {
	return term.IsTerminal(uintptr(fd)) && foregroundPgrp(fd) == syscall.Getpgrp()
}

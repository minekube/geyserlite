// SPDX-License-Identifier: MIT
//go:build unix

package geyserlite

import (
	"errors"
	"os"
	"syscall"
)

// sysProcAttrNewGroup returns SysProcAttr that places the child in a new
// process group, so signals to the parent don't auto-cascade and we can
// signal the whole group explicitly.
func sysProcAttrNewGroup() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// signalProcess sends SIGTERM to the entire process group led by pid.
// The child is started with Setpgid, so its PID equals its process-group
// ID; the negative pid tells kill(2) to target the whole group, which
// terminates grandchildren (e.g. Geyser-spawned workers) that would
// otherwise survive and keep the UDP port bound.
//
// Returns os.ErrProcessDone if the process has already exited (ESRCH),
// which exec.Cmd treats as "nothing left to signal."
func signalProcess(pid int) error {
	err := syscall.Kill(-pid, syscall.SIGTERM)
	if err == nil || errors.Is(err, syscall.ESRCH) {
		// Either we signaled the group, or it's already gone. exec.Cmd
		// accepts os.ErrProcessDone to short-circuit further signaling.
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return nil
	}
	return err
}

// signalFromExitError extracts the terminating signal from an exec.ExitError
// on Unix, if the process was killed by a signal. Returns nil for ordinary
// exits or when the wait status is unavailable.
func signalFromExitError(exitErr *os.ProcessState) os.Signal {
	if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return ws.Signal()
	}
	return nil
}

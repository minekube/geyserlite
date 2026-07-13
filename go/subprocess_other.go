// SPDX-License-Identifier: MIT
//go:build !unix

package geyserlite

import (
	"errors"
	"os"
	"syscall"
)

func sysProcAttrNewGroup() *syscall.SysProcAttr { return nil }

// signalProcess on non-Unix falls back to signaling the direct child.
// Windows has no kill(-pid) equivalent; Job Object / process-tree
// termination could be added here later if Windows grandchildren become
// a concern, but exec.Cmd's WaitDelay + kill_on_drop covers the common
// case for now.
func signalProcess(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return os.ErrProcessDone
	}
	if err := p.Signal(syscall.SIGTERM); err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return os.ErrProcessDone
		}
		return err
	}
	return nil
}

// signalFromExitError on non-Unix cannot extract a signal from the wait
// status (no syscall.WaitStatus equivalent). Returns nil so
// formatExitError falls back to a generic signal-death message.
func signalFromExitError(_ *os.ProcessState) os.Signal { return nil }

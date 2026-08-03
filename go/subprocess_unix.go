// SPDX-License-Identifier: MIT
//go:build unix

package geyserlite

import (
	"fmt"
	"os"
	"syscall"
)

// sysProcAttrNewGroup returns SysProcAttr that places the child in a new
// process group, so signals to the parent don't auto-cascade and we can
// signal the whole group explicitly.
func sysProcAttrNewGroup() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

func ingressSubprocessSupported() bool { return true }

func newIngressSocketpair() (*os.File, *os.File, error) {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_SEQPACKET, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("geyserlite: create verified ingress socketpair: %w", err)
	}
	syscall.CloseOnExec(fds[0])
	syscall.CloseOnExec(fds[1])
	return os.NewFile(uintptr(fds[0]), "geyserlite-ingress-parent"), os.NewFile(uintptr(fds[1]), "geyserlite-ingress-child"), nil
}

// SPDX-License-Identifier: MIT
//go:build unix

package geyserlite

import "syscall"

// sysProcAttrNewGroup returns SysProcAttr that places the child in a new
// process group, so signals to the parent don't auto-cascade and we can
// signal the whole group explicitly.
func sysProcAttrNewGroup() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

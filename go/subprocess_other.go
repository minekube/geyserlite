// SPDX-License-Identifier: MIT
//go:build !unix

package geyserlite

import "syscall"

func sysProcAttrNewGroup() *syscall.SysProcAttr { return nil }

// SPDX-License-Identifier: MIT
//go:build !unix

package geyserlite

import (
	"os"
	"syscall"
)

func sysProcAttrNewGroup() *syscall.SysProcAttr { return nil }

func newIngressSocketpair() (*os.File, *os.File, error) { return nil, nil, ErrIngressABI }

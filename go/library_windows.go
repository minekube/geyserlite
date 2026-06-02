// SPDX-License-Identifier: MIT
//go:build windows

package geyserlite

import "syscall"

func openLibrary(path string) (uintptr, error) {
	handle, err := syscall.LoadLibrary(path)
	return uintptr(handle), err
}

// SPDX-License-Identifier: MIT
//go:build !(darwin || freebsd || linux || netbsd || windows)

package geyserlite

import (
	"fmt"
	"runtime"
)

func openLibrary(string) (uintptr, error) {
	return 0, fmt.Errorf("dynamic library loading is not supported on %s", runtime.GOOS)
}

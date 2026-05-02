// SPDX-License-Identifier: MIT
package geyserlite

import (
	"time"
	"unsafe"
)

// ptrOf returns the unsafe.Pointer of the first byte of b as a uintptr.
// libgeyserlite reads the pointed-to bytes synchronously during the call
// that takes it, so the slice cannot be GC'd during use.
func ptrOf(b []byte) uintptr {
	return uintptr(unsafe.Pointer(&b[0]))
}

func pollSleep() <-chan time.Time {
	return time.After(250 * time.Millisecond)
}

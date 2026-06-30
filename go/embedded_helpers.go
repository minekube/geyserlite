// SPDX-License-Identifier: MIT
package geyserlite

import (
	"runtime"
	"time"
	"unsafe"
)

// pinString allocates a NUL-terminated copy of s in Go memory, pins its
// backing array so the GC cannot move or reclaim it while native code
// reads it, and returns the raw pointer plus a release function.
//
// Passing the address of a Go object to native code as a uintptr is
// GC-unsafe unless the object is pinned: a uintptr is invisible to the
// garbage collector, so without pinning the backing array could be
// moved or freed before libgeyserlite reads it (fails under checkptr,
// memory pressure, or a moving GC). The returned cleanup MUST be
// called after the native call that consumes the pointer returns, and
// not deferred across any goroutine boundary that the runtime can't
// see the pointer through.
func pinString(s string) (uintptr, func()) {
	b := append([]byte(s), 0)

	var pinner runtime.Pinner
	pinner.Pin(&b[0])

	return uintptr(unsafe.Pointer(&b[0])), func() {
		pinner.Unpin()
		// Keep the slice reachable until the release runs so the GC
		// never observes b as garbage between Pin and Unpin.
		runtime.KeepAlive(b)
	}
}

func pollSleep() <-chan time.Time {
	return time.After(250 * time.Millisecond)
}

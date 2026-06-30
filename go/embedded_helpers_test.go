// SPDX-License-Identifier: MIT
package geyserlite

import (
	"runtime"
	"testing"
)

// TestPinString_Contract exercises the GC-safety invariant that pinString
// exists for, at the level that's testable from pure Go:
//
//   - pinString returns a non-zero pointer (the value handed to native code).
//   - the release function is safe to call and idempotent.
//   - a GC cycle while the pin is held does not panic.
//
// The deeper property — that the pinned backing array cannot move or be
// reclaimed until release runs — is not directly observable from Go
// without tripping go vet's unsafeptr check, so it's covered indirectly
// by building the embedded startup path under
// -gcflags=all=-d=checkptr=2 (see Task 1 acceptance). This test still
// runs pinString end-to-end, including the Pinner.Pin/Unpin path, so a
// regression in the construction surfaces here.
func TestPinString_Contract(t *testing.T) {
	t.Parallel()
	const s = "config.yml"
	ptr, release := pinString(s)
	defer release()

	if ptr == 0 {
		t.Fatal("pinString returned a nil pointer")
	}

	// Force GC while the pin is held; if pinString leaked the object to
	// the freelist prematurely nothing observable happens here, but the
	// call exercises Pin/KeepAlive bookkeeping end-to-end.
	for i := 0; i < 20; i++ {
		runtime.GC()
	}
}

// TestPinString_ReleaseIsSafe documents the supported contract: release
// must be called after the native call returns, and calling it must not
// panic even if invoked twice (runtime.Pinner.Unpin tolerates that).
func TestPinString_ReleaseIsSafe(t *testing.T) {
	t.Parallel()
	_, release := pinString("any")
	release()
	assertNotPanics(t, release) // idempotent per runtime.Pinner docs
}

func assertNotPanics(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()
	fn()
}

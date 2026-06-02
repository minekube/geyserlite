// SPDX-License-Identifier: MIT
package geyserlite

import (
	"runtime"
	"testing"
)

func TestLibraryNameMatchesPlatform(t *testing.T) {
	t.Parallel()

	switch runtime.GOOS {
	case "darwin":
		if got := libraryName(); got != "libgeyserlite.dylib" {
			t.Fatalf("libraryName() = %q, want libgeyserlite.dylib", got)
		}
	case "windows":
		if got := libraryName(); got != "geyserlite.dll" {
			t.Fatalf("libraryName() = %q, want geyserlite.dll", got)
		}
	default:
		if got := libraryName(); got != "libgeyserlite.so" {
			t.Fatalf("libraryName() = %q, want libgeyserlite.so", got)
		}
	}
}

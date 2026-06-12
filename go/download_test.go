// SPDX-License-Identifier: MIT
package geyserlite

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifiedDownloadPathIncludesExpectedSHA(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	assetName := "geyserlite-windows-amd64.exe"
	expectedSHA := strings.Repeat("a", 64)

	got := verifiedDownloadPath(dir, assetName, expectedSHA)
	want := filepath.Join(dir, expectedSHA, assetName)
	if got != want {
		t.Fatalf("verifiedDownloadPath() = %q, want %q", got, want)
	}
}

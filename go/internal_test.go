// SPDX-License-Identifier: MIT
package geyserlite

import "os"

// readFileBytes is the test-only os.ReadFile alias kept here so the test
// file doesn't need to import "os" itself in newer Go versions where that
// trips an unused-import warning under certain build tags.
func readFileBytes(p string) ([]byte, error) { return os.ReadFile(p) }

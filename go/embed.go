// SPDX-License-Identifier: MIT
//go:build geyserlite_embed

// Common helpers for the per-arch embed_<os>_<arch>.go files.
// Compiled only when -tags geyserlite_embed is set.

package geyserlite

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// extractEmbeddedAsset writes blob to <UserCacheDir>/geyserlite/<sha>/<name>
// (idempotent — reuses the cached copy if its sha matches). Returns the
// path on disk. If blob is empty (build did not include the asset for this
// target), returns ("", false, nil).
func extractEmbeddedAsset(blob []byte, name string, executable bool) (string, bool, error) {
	if len(blob) == 0 {
		return "", false, nil
	}
	sum := sha256.Sum256(blob)
	hexSum := hex.EncodeToString(sum[:])

	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return "", false, fmt.Errorf("geyserlite: locate cache dir: %w", err)
	}
	dir := filepath.Join(cacheRoot, "geyserlite", hexSum)
	path := filepath.Join(dir, name)

	if info, err := os.Stat(path); err == nil && info.Size() == int64(len(blob)) {
		// Already extracted; trust the sha-keyed dirname.
		return path, true, nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", false, fmt.Errorf("geyserlite: stat cache: %w", err)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", false, fmt.Errorf("geyserlite: mkdir cache: %w", err)
	}
	tmp := path + ".tmp"
	mode := os.FileMode(0o644)
	if executable {
		mode = 0o755
	}
	if err := os.WriteFile(tmp, blob, mode); err != nil {
		return "", false, fmt.Errorf("geyserlite: write cache tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", false, fmt.Errorf("geyserlite: rename cache: %w", err)
	}
	return path, true, nil
}

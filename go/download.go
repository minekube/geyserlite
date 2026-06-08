// SPDX-License-Identifier: MIT
package geyserlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// downloadAsset fetches the named release asset matching the runtime
// GOOS/GOARCH into the user's cache dir, verifying its sha256 against
// the release's checksums.txt manifest.
//
// Returns the on-disk path of the cached file. Idempotent: a previously
// cached file with the right sha is returned without re-downloading.
//
// Used by locate.go when no path / env / embedded asset / system match
// has been found, and Options.Offline is false.
func downloadAsset(ctx context.Context, opts Options, kind assetKind) (string, error) {
	version := opts.Version
	if version == "" {
		version = DefaultVersion
	}
	base := opts.Mirror
	if base == "" {
		base = DefaultDownloadBase
	}
	assetName, err := assetForRuntime(kind)
	if err != nil {
		return "", err
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("geyserlite: locate cache: %w", err)
	}
	dir := filepath.Join(cacheDir, "geyserlite", version)
	cachedPath := filepath.Join(dir, assetName)

	// Manifest fetch + sha lookup.
	expectedSha, err := fetchExpectedSha(ctx, base, version, assetName)
	if err != nil {
		return "", err
	}

	// Reuse cached if matching.
	if existing, err := os.Open(cachedPath); err == nil {
		sum, hashErr := streamSha(existing)
		_ = existing.Close()
		if hashErr == nil && sum == expectedSha {
			return cachedPath, nil
		}
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("geyserlite: mkdir cache: %w", err)
	}
	url := fmt.Sprintf("%s/%s/%s", strings.TrimSuffix(base, "/"), version, assetName)
	tmp := cachedPath + ".tmp"
	if err := downloadFile(ctx, url, tmp); err != nil {
		return "", err
	}
	gotSha, err := shaFile(tmp)
	if err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if gotSha != expectedSha {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("geyserlite: sha256 mismatch for %s: got %s, want %s", assetName, gotSha, expectedSha)
	}
	if kind == assetKindBinary {
		_ = os.Chmod(tmp, 0o755)
	}
	if err := os.Rename(tmp, cachedPath); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("geyserlite: rename %s: %w", cachedPath, err)
	}
	return cachedPath, nil
}

type assetKind int

const (
	assetKindBinary assetKind = iota
	assetKindLibrary
)

func assetForRuntime(kind assetKind) (string, error) {
	return assetFor(kind, runtime.GOOS, runtime.GOARCH)
}

func assetFor(kind assetKind, goos, goarch string) (string, error) {
	var arch string
	switch goarch {
	case "amd64":
		arch = "amd64"
	case "arm64":
		arch = "arm64"
	default:
		return "", fmt.Errorf("geyserlite: auto-download supports amd64/arm64 only (got %s)", goarch)
	}

	if goos == "windows" {
		if kind == assetKindBinary && arch == "amd64" {
			return "geyserlite-windows-amd64.exe", nil
		}
		return "", fmt.Errorf("geyserlite: auto-download supports windows/amd64 subprocess binaries only (got %s/%s); set BinaryPath/LibraryPath manually", goos, goarch)
	}
	if goos != "linux" {
		return "", fmt.Errorf("geyserlite: auto-download supports linux amd64/arm64 and windows amd64 subprocess binaries only (got %s/%s); set BinaryPath/LibraryPath manually", goos, goarch)
	}

	switch kind {
	case assetKindBinary:
		return fmt.Sprintf("geyserlite-linux-%s", arch), nil
	case assetKindLibrary:
		return fmt.Sprintf("libgeyserlite-linux-%s.so", arch), nil
	default:
		return "", errors.New("geyserlite: unknown asset kind")
	}
}

func fetchExpectedSha(ctx context.Context, base, version, assetName string) (string, error) {
	url := fmt.Sprintf("%s/%s/checksums.txt", strings.TrimSuffix(base, "/"), version)
	body, err := httpGet(ctx, url)
	if err != nil {
		return "", fmt.Errorf("geyserlite: fetch checksums for %s: %w", version, err)
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, 1<<20))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		// format: "<sha256>  <filename>" (sha256sum -b emits "*<filename>" — strip the asterisk)
		name := strings.TrimPrefix(fields[1], "*")
		if name == assetName || strings.HasSuffix(name, "/"+assetName) {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("geyserlite: %s not listed in checksums.txt for %s", assetName, version)
}

func downloadFile(ctx context.Context, url, dest string) error {
	body, err := httpGet(ctx, url)
	if err != nil {
		return fmt.Errorf("geyserlite: get %s: %w", url, err)
	}
	defer body.Close()
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, body); err != nil {
		_ = f.Close()
		return fmt.Errorf("geyserlite: copy %s: %w", dest, err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	return nil
}

func httpGet(ctx context.Context, url string) (io.ReadCloser, error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("http %d for %s", resp.StatusCode, url)
	}
	return resp.Body, nil
}

func shaFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return streamSha(f)
}

func streamSha(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

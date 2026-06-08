// SPDX-License-Identifier: MIT
package geyserlite

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// locateBinary finds the geyserlite native binary for [ModeSubprocess].
//
// Resolution order:
//  1. opts.BinaryPath if set (must exist + be executable)
//  2. $GEYSERLITE_BINARY env var
//  3. embedded blob (when built with -tags geyserlite_embed)
//  4. $PATH lookup for "geyserlite"
//  5. auto-download from GitHub Release (skipped if Options.Offline)
//  6. ErrNoBinary
func locateBinary(ctx context.Context, opts Options) (string, error) {
	if opts.BinaryPath != "" {
		if err := executableAt(opts.BinaryPath); err != nil {
			return "", fmt.Errorf("geyserlite: BinaryPath %q: %w", opts.BinaryPath, err)
		}
		return opts.BinaryPath, nil
	}
	if env := os.Getenv("GEYSERLITE_BINARY"); env != "" {
		if err := executableAt(env); err != nil {
			return "", fmt.Errorf("geyserlite: $GEYSERLITE_BINARY %q: %w", env, err)
		}
		return env, nil
	}
	if path, ok, err := extractEmbeddedBinary(); err != nil {
		return "", err
	} else if ok {
		return path, nil
	}
	if path, err := exec.LookPath("geyserlite"); err == nil {
		return path, nil
	}
	if !opts.Offline {
		if path, err := downloadAsset(ctx, opts, assetKindBinary); err == nil {
			return path, nil
		} else {
			return "", fmt.Errorf("%w: auto-download failed: %v", ErrNoBinary, err)
		}
	}
	return "", ErrNoBinary
}

// locateLibrary finds libgeyserlite.so for [ModeEmbedded].
//
// Resolution order:
//  1. opts.LibraryPath if set
//  2. $GEYSERLITE_LIBRARY env var
//  3. embedded blob (when built with -tags geyserlite_embed)
//  4. system search: /usr/lib, /usr/local/lib, $LD_LIBRARY_PATH
//  5. auto-download from GitHub Release (skipped if Options.Offline)
//  6. ErrNoLibrary
func locateLibrary(ctx context.Context, opts Options) (string, error) {
	if opts.LibraryPath != "" {
		if err := fileAt(opts.LibraryPath); err != nil {
			return "", fmt.Errorf("geyserlite: LibraryPath %q: %w", opts.LibraryPath, err)
		}
		return opts.LibraryPath, nil
	}
	if env := os.Getenv("GEYSERLITE_LIBRARY"); env != "" {
		if err := fileAt(env); err != nil {
			return "", fmt.Errorf("geyserlite: $GEYSERLITE_LIBRARY %q: %w", env, err)
		}
		return env, nil
	}
	if path, ok, err := extractEmbeddedLibrary(); err != nil {
		return "", err
	} else if ok {
		return path, nil
	}
	for _, dir := range systemLibDirs() {
		p := filepath.Join(dir, libraryName())
		if fileAt(p) == nil {
			return p, nil
		}
	}
	if !opts.Offline {
		if path, err := downloadAsset(ctx, opts, assetKindLibrary); err == nil {
			return path, nil
		} else {
			return "", fmt.Errorf("%w: auto-download failed: %v", ErrNoLibrary, err)
		}
	}
	return "", ErrNoLibrary
}

func libraryName() string {
	switch runtime.GOOS {
	case "darwin":
		return "libgeyserlite.dylib"
	case "windows":
		return "geyserlite.dll"
	default:
		return "libgeyserlite.so"
	}
}

func systemLibDirs() []string {
	dirs := []string{"/usr/local/lib", "/usr/lib"}
	if env := os.Getenv("LD_LIBRARY_PATH"); env != "" {
		dirs = append(filepath.SplitList(env), dirs...)
	}
	return dirs
}

func executableAt(p string) error {
	info, err := os.Stat(p)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return errors.New("is a directory")
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		return errors.New("not executable (chmod +x)")
	}
	return nil
}

func fileAt(p string) error {
	info, err := os.Stat(p)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return errors.New("is a directory")
	}
	return nil
}

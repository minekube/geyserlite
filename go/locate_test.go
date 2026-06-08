// SPDX-License-Identifier: MIT
package geyserlite

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

func TestLocateBinaryReportsAutoDownloadFailure(t *testing.T) {
	t.Setenv("GEYSERLITE_BINARY", "")
	t.Setenv("PATH", "")

	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)

	_, err := locateBinary(context.Background(), Options{Mirror: server.URL})
	if !errors.Is(err, ErrNoBinary) {
		t.Fatalf("got %v, want ErrNoBinary", err)
	}
	if !strings.Contains(err.Error(), "auto-download failed") {
		t.Fatalf("error %q does not report auto-download failure", err)
	}
	if !strings.Contains(err.Error(), expectedBinaryDownloadCause()) {
		t.Fatalf("error %q does not preserve download cause", err)
	}
}

func TestLocateLibraryReportsAutoDownloadFailure(t *testing.T) {
	t.Setenv("GEYSERLITE_LIBRARY", "")
	t.Setenv("LD_LIBRARY_PATH", t.TempDir())

	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)

	_, err := locateLibrary(context.Background(), Options{Mirror: server.URL})
	if !errors.Is(err, ErrNoLibrary) {
		t.Fatalf("got %v, want ErrNoLibrary", err)
	}
	if !strings.Contains(err.Error(), "auto-download failed") {
		t.Fatalf("error %q does not report auto-download failure", err)
	}
	if !strings.Contains(err.Error(), expectedLibraryDownloadCause()) {
		t.Fatalf("error %q does not preserve download cause", err)
	}
}

func TestAssetForRuntimeTargets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		kind    assetKind
		goos    string
		goarch  string
		want    string
		wantErr string
	}{
		{
			name:   "linux amd64 binary",
			kind:   assetKindBinary,
			goos:   "linux",
			goarch: "amd64",
			want:   "geyserlite-linux-amd64",
		},
		{
			name:   "linux arm64 library",
			kind:   assetKindLibrary,
			goos:   "linux",
			goarch: "arm64",
			want:   "libgeyserlite-linux-arm64.so",
		},
		{
			name:   "windows amd64 binary",
			kind:   assetKindBinary,
			goos:   "windows",
			goarch: "amd64",
			want:   "geyserlite-windows-amd64.exe",
		},
		{
			name:    "windows library unsupported",
			kind:    assetKindLibrary,
			goos:    "windows",
			goarch:  "amd64",
			wantErr: "windows/amd64 subprocess binaries only",
		},
		{
			name:    "windows arm64 unsupported",
			kind:    assetKindBinary,
			goos:    "windows",
			goarch:  "arm64",
			wantErr: "windows/amd64 subprocess binaries only",
		},
		{
			name:    "darwin unsupported",
			kind:    assetKindBinary,
			goos:    "darwin",
			goarch:  "arm64",
			wantErr: "linux amd64/arm64 and windows amd64 subprocess binaries only",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := assetFor(tt.kind, tt.goos, tt.goarch)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("assetFor() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("assetFor() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("assetFor() = %q, want %q", got, tt.want)
			}
		})
	}
}

func expectedBinaryDownloadCause() string {
	switch runtime.GOOS {
	case "linux", "windows":
		return "http 404"
	default:
		return "auto-download supports linux amd64/arm64 and windows amd64 subprocess binaries only"
	}
}

func expectedLibraryDownloadCause() string {
	switch runtime.GOOS {
	case "linux":
		return "http 404"
	case "windows":
		return "windows/amd64 subprocess binaries only"
	default:
		return "auto-download supports linux amd64/arm64 and windows amd64 subprocess binaries only"
	}
}

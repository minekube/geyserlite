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
	if !strings.Contains(err.Error(), expectedDownloadCause()) {
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
	if !strings.Contains(err.Error(), expectedDownloadCause()) {
		t.Fatalf("error %q does not preserve download cause", err)
	}
}

func expectedDownloadCause() string {
	if runtime.GOOS != "linux" {
		return "auto-download supports linux only"
	}
	return "http 404"
}

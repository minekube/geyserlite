//go:build geyserlite_acceptance && linux && amd64

package geyserlite

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"go.minekube.com/geyserlite/internal/synthetic"
)

// TestAutoDownloadColdAndWarmCacheBedrockReadiness is an opt-in acceptance
// test for the self-hosted Gate Lite default: the subprocess binary is fetched
// into an empty cache, starts Geyser, and serves a real RakNet status reply.
// The second start reuses that exact cache entry; its mirror deliberately has
// no binary endpoint, so an asset re-download fails the test.
//
// The released native binary is intentionally used here. It exercises the
// consumer path that unit tests cannot cover, so it is excluded from normal CI
// unless explicitly enabled with -tags geyserlite_acceptance.
func TestAutoDownloadColdAndWarmCacheBedrockReadiness(t *testing.T) {
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	t.Setenv("GEYSERLITE_BINARY", "")
	t.Setenv("GEYSERLITE_LIBRARY", "")
	// Do not let a developer-installed geyserlite binary turn a cold-cache test
	// into a PATH lookup. The helper is already invoked by absolute path.
	t.Setenv("PATH", "/usr/bin:/bin")

	runAcceptanceServer(t, cacheHome, "", 5*time.Minute)
	asset, checksum := cachedBinary(t, cacheHome)
	if checksum == "" || asset == "" {
		t.Fatal("cold start did not leave a content-addressed geyserlite binary cache entry")
	}

	var assetRequests atomic.Int32
	warmMirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + DefaultVersion + "/checksums.txt":
			_, _ = fmt.Fprintf(w, "%s  %s\n", checksum, filepath.Base(asset))
		default:
			assetRequests.Add(1)
			http.Error(w, "warm cache must not download the binary", http.StatusGone)
		}
	}))
	defer warmMirror.Close()

	runAcceptanceServer(t, cacheHome, warmMirror.URL, 2*time.Minute)
	if got := assetRequests.Load(); got != 0 {
		t.Fatalf("warm cache requested the binary %d times", got)
	}
}

// TestAutoDownloadAcceptanceHelper runs one real server in a child process.
// Native Geyser owns process-global state, so two cold/warm starts cannot share
// a Go test process. The parent drives readiness with the public RakNet probe.
func TestAutoDownloadAcceptanceHelper(t *testing.T) {
	if os.Getenv("GEYSERLITE_ACCEPTANCE_HELPER") != "1" {
		return
	}
	listen := os.Getenv("GEYSERLITE_ACCEPTANCE_LISTEN")
	upstream := os.Getenv("GEYSERLITE_ACCEPTANCE_UPSTREAM")
	if listen == "" || upstream == "" {
		os.Exit(2)
	}
	ctx, stop := signalContext()
	defer stop()

	srv, err := New(Options{
		Listen:   listen,
		Upstream: upstream,
		// Gate-managed GeyserLite always uses the Floodgate handoff. A status
		// probe does not expose an identity, but this still validates that the
		// generated Floodgate configuration reaches a serving UDP listener.
		AuthType:     Floodgate,
		FloodgateKey: []byte("0123456789abcdef"),
		Mode:         ModeSubprocess,
		Mirror:       os.Getenv("GEYSERLITE_ACCEPTANCE_MIRROR"),
	})
	if err != nil {
		os.Exit(1)
	}
	if err := srv.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
		os.Exit(1)
	}
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func runAcceptanceServer(t *testing.T, cacheHome, mirror string, budget time.Duration) {
	t.Helper()
	listen := reserveUDPAddr(t)
	upstream := reserveTCPAddr(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestAutoDownloadAcceptanceHelper$")
	cmd.Env = append(os.Environ(),
		"GEYSERLITE_ACCEPTANCE_HELPER=1",
		"GEYSERLITE_ACCEPTANCE_LISTEN="+listen,
		"GEYSERLITE_ACCEPTANCE_UPSTREAM="+upstream,
		"GEYSERLITE_ACCEPTANCE_MIRROR="+mirror,
		"XDG_CACHE_HOME="+cacheHome,
		"GEYSERLITE_BINARY=",
		"GEYSERLITE_LIBRARY=",
		"PATH=/usr/bin:/bin",
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s acceptance child: %v", acceptancePhase(mirror), err)
	}

	probeCtx, cancelProbe := context.WithTimeout(context.Background(), budget)
	_, probeErr := synthetic.Wait(probeCtx, listen, 250*time.Millisecond)
	cancelProbe()
	if probeErr != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		_ = cmd.Wait()
		t.Fatalf("%s cache did not reach Bedrock listener readiness within %s", acceptancePhase(mirror), budget)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("stop %s acceptance child: %v", acceptancePhase(mirror), err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("%s acceptance child stopped uncleanly: %v", acceptancePhase(mirror), err)
		}
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("%s acceptance child did not stop within 30s", acceptancePhase(mirror))
	}
}

func acceptancePhase(mirror string) string {
	if mirror == "" {
		return "cold"
	}
	return "warm"
}

func reserveUDPAddr(t *testing.T) string {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("reserve Bedrock UDP port: %v", err)
	}
	addr := conn.LocalAddr().String()
	if err := conn.Close(); err != nil {
		t.Fatalf("release Bedrock UDP port: %v", err)
	}
	return addr
}

func reserveTCPAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve Java upstream port: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release Java upstream port: %v", err)
	}
	return addr
}

func cachedBinary(t *testing.T, cacheHome string) (string, string) {
	t.Helper()
	root := filepath.Join(cacheHome, "geyserlite", DefaultVersion)
	var asset, checksum string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() != "geyserlite-linux-amd64" {
			return nil
		}
		candidate := filepath.Base(filepath.Dir(path))
		if len(candidate) != 64 || strings.Trim(candidate, "0123456789abcdef") != "" {
			return fmt.Errorf("cache binary is not under a sha256 directory")
		}
		asset, checksum = path, candidate
		return nil
	})
	if err != nil {
		t.Fatalf("inspect cold cache: %v", err)
	}
	return asset, checksum
}

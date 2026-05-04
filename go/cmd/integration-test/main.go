// integration-test boots a real geyserlite.Server (in-process via
// purego), waits up to a configured deadline for the Bedrock UDP
// listener to bind, and exits 0 once it does.
//
// It does NOT verify Bedrock traffic — that's bedrock-probe's job. This
// program's job is "the Go library loaded libgeyserlite.so end-to-end
// and got far enough to bind a UDP socket". Pair the two in CI to get
// full coverage:
//
//	# in CI:
//	GEYSERLITE_LIBRARY=/tmp/lib/libgeyserlite-linux-amd64.so \
//	  go run ./cmd/integration-test -listen 127.0.0.1:19133 -timeout 30s &
//	go run ./cmd/bedrock-probe -wait 30s 127.0.0.1:19133
//
// Stub a fake Java upstream on `Upstream` if you don't want Geyser
// log-spamming about connection refused; the bind check itself doesn't
// require an upstream.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"

	geyserlite "go.minekube.com/geyserlite"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:19132", "Bedrock UDP listen addr (host:port)")
	upstream := flag.String("upstream", "127.0.0.1:25565", "Java MC upstream addr")
	timeout := flag.Duration("timeout", 30*time.Second, "max time to wait for the listener to bind")
	mode := flag.String("mode", "embedded", "geyserlite mode: embedded (in-process via .so) | subprocess (spawn the ELF)")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	var runMode geyserlite.Mode
	switch *mode {
	case "embedded":
		runMode = geyserlite.ModeEmbedded
	case "subprocess":
		runMode = geyserlite.ModeSubprocess
	default:
		fail("unknown -mode %q (want embedded|subprocess)", *mode)
	}

	srv, err := geyserlite.New(geyserlite.Options{
		Listen:   *listen,
		Upstream: *upstream,
		AuthType: geyserlite.Offline,
		Mode:     runMode,
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, nil)),
	})
	if err != nil {
		fail("New: %v", err)
	}

	runErr := make(chan error, 1)
	go func() { runErr <- srv.Start(ctx) }()

	// Poll for the UDP socket being bound. Faster + more reliable than
	// reading log lines; works regardless of whether Geyser is fully
	// initialized — we just need to know the listener is up.
	target, err := net.ResolveUDPAddr("udp", *listen)
	if err != nil {
		fail("resolve: %v", err)
	}
	deadline := time.Now().Add(*timeout)
	for time.Now().Before(deadline) {
		if isUDPBound(target) {
			fmt.Fprintf(os.Stderr, "OK: %s is bound\n", *listen)
			// Stay up: sibling probes (bedrock-probe in CI) need the
			// listener to remain alive long enough to reply. Idle here
			// until the parent context (signal / timeout) cancels us.
			// On exit we DO NOT call cancel + wait on runErr — the
			// graceful-shutdown path through purego currently SIGSEGVs
			// during native teardown, which would tank the test even
			// after a successful probe. _exit avoids the cleanup chain.
			<-ctx.Done()
			_ = os.Stderr.Sync()
			os.Exit(0)
		}
		select {
		case err := <-runErr:
			fail("server exited before bind: %v", err)
		case <-time.After(500 * time.Millisecond):
		}
	}
	fail("timed out waiting for %s to bind after %s", *listen, *timeout)
}

// isUDPBound checks whether something is already listening on addr.
// We try to bind ourselves; success means nothing was there, failure
// (with EADDRINUSE-shaped messages) means the geyserlite listener
// claimed it.
func isUDPBound(addr *net.UDPAddr) bool {
	conn, err := net.ListenUDP("udp", addr)
	if err == nil {
		// Whoever holds the port hasn't bound it; release ours and keep waiting.
		_ = conn.Close()
		return false
	}
	// Match common "address in use" forms across libc / kernel
	// versions instead of relying on a specific syscall.Errno —
	// the test exit signal is just "is the port taken".
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "address already in use") ||
		strings.Contains(msg, "addrinuse")
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "integration-test: "+format+"\n", args...)
	os.Exit(1)
}

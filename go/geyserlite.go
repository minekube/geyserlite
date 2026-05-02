// SPDX-License-Identifier: MIT
package geyserlite

// Public-facing types and constants for the geyserlite Go library.
//
// The implementation (subprocess supervisor, purego dlopen, config rendering,
// binary auto-location) is split across server.go, options.go, manage.go,
// embed_*.go, locate.go.
//
// This file is a SCAFFOLDING placeholder for the v0.2 milestone. The signatures
// are the intended public API; the bodies are TODO.

import (
	"context"
	"errors"
	"log/slog"
)

// AuthType controls how Geyser authenticates Bedrock players to the upstream
// Java server.
type AuthType int

const (
	// Floodgate uses an AES-128 shared key + Bedrock UUID.
	// Recommended when the upstream Java side is Gate or a Floodgate-aware backend.
	Floodgate AuthType = iota

	// Online forwards the player's Microsoft account; the upstream Java side
	// must do its own auth and accept Bedrock-Microsoft players.
	Online

	// Offline trusts the Bedrock-supplied username and connects to the
	// upstream Java server in offline mode.
	Offline
)

// Mode selects how the Go library invokes Geyser.
type Mode int

const (
	// ModeEmbedded loads libgeyserlite.so via purego and calls @CEntryPoint
	// methods directly. No subprocess. Lowest overhead. Native crash kills the host.
	ModeEmbedded Mode = iota

	// ModeSubprocess spawns the geyserlite ELF via os/exec. Crash-isolated.
	ModeSubprocess
)

// Options configures a geyserlite Server.
type Options struct {
	// Listen address for incoming Bedrock UDP. Defaults to ":19132".
	// Use FlyGlobalServices() on Fly.io.
	Listen string

	// Upstream Java MC address (e.g. "127.0.0.1:25567" for Gate's bedrock listener).
	// Required.
	Upstream string

	// AuthType for forwarding to upstream. Default: Floodgate.
	AuthType AuthType

	// FloodgateKey is 16 raw bytes (AES-128). Required when AuthType == Floodgate.
	// Generate via GenerateFloodgateKey.
	FloodgateKey []byte

	// MOTD shown to Bedrock clients.
	MOTD MOTD

	// Mode selects in-process vs subprocess. Default: ModeEmbedded.
	Mode Mode

	// LibraryPath overrides the auto-located libgeyserlite.so. ModeEmbedded only.
	LibraryPath string

	// BinaryPath overrides the auto-located geyserlite ELF. ModeSubprocess only.
	BinaryPath string

	// JVMArgs overrides the default tuned JVM args. nil = use [DefaultJVMArgs].
	// Has no effect in ModeEmbedded — the args are baked into libgeyserlite.so at build time.
	JVMArgs []string

	// Logger receives Geyser stdout/stderr as structured records. Defaults to slog.Default.
	Logger *slog.Logger
}

// MOTD is the Bedrock client-visible server description (two lines).
type MOTD struct {
	Line1, Line2 string
}

// Server is a managed geyserlite instance.
type Server struct {
	// unexported fields
}

// New constructs a Server from Options. Does not start it.
func New(opts Options) (*Server, error) {
	return nil, errors.New("geyserlite: not implemented (v0.2 placeholder)")
}

// Start runs the server until ctx is cancelled or an unrecoverable error occurs.
// In ModeEmbedded, this calls geyser_run via purego on a goroutine and blocks
// until that returns. In ModeSubprocess, it manages the supervised lifecycle.
func (s *Server) Start(ctx context.Context) error {
	return errors.New("geyserlite: not implemented (v0.2 placeholder)")
}

// Stop requests a graceful shutdown.
func (s *Server) Stop(ctx context.Context) error {
	return errors.New("geyserlite: not implemented (v0.2 placeholder)")
}

// Healthy reports whether Geyser is currently accepting connections.
func (s *Server) Healthy() bool {
	return false
}

// Wait blocks until Start returns. Returns the same error.
func (s *Server) Wait() error {
	return errors.New("geyserlite: not implemented (v0.2 placeholder)")
}

// GenerateFloodgateKey returns 16 random bytes suitable as a Floodgate
// AES-128 key. The upstream Geyser README's openssl example using
// `genpkey -algorithm RSA` is wrong; that produces an RSA private key, but
// Floodgate uses AES-128.
func GenerateFloodgateKey() ([]byte, error) {
	return nil, errors.New("geyserlite: not implemented (v0.2 placeholder)")
}

// FlyGlobalServices returns "fly-global-services" if running on a Fly.io
// machine (Fly's UDP edge NATs external traffic to this hostname inside the
// container). Returns "0.0.0.0" otherwise.
func FlyGlobalServices() string {
	return "0.0.0.0"
}

// DefaultJVMArgs returns the tuned argument list used by libgeyserlite.so
// at build time (and applied to ModeSubprocess). Useful for Options.JVMArgs.
func DefaultJVMArgs() []string {
	return []string{
		"-Xmx64m",
		"-XX:MaxHeapFree=4m",
		"-XX:+CollectYoungGenerationSeparately",
		"-XX:ActiveProcessorCount=1",
		"-Dio.netty.maxDirectMemory=16777216",
		"-XX:MaxDirectMemorySize=16m",
		"-Dio.netty.allocator.type=unpooled",
		"-Dio.netty.allocator.numHeapArenas=1",
		"-Dio.netty.allocator.numDirectArenas=1",
		"-Dio.netty.eventLoopThreads=2",
		"-Dio.netty.recycler.maxCapacityPerThread=0",
		"-Dio.netty.leakDetection.level=disabled",
		"-Djava.util.concurrent.ForkJoinPool.common.parallelism=1",
		"-Dlog4j2.disableJmx=true",
	}
}

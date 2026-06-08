// SPDX-License-Identifier: MIT
package geyserlite

import (
	"errors"
	"log/slog"
	"time"
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

// String renders the AuthType as the Geyser config.yml value.
func (a AuthType) String() string {
	switch a {
	case Floodgate:
		return "floodgate"
	case Online:
		return "online"
	case Offline:
		return "offline"
	default:
		return "floodgate"
	}
}

// Mode selects how the Go library invokes Geyser.
type Mode int

const (
	// ModeEmbedded loads libgeyserlite.so via purego and calls @CEntryPoint
	// methods directly. No subprocess. Lowest overhead. Native crash kills the host.
	ModeEmbedded Mode = iota

	// ModeSubprocess spawns the geyserlite native binary via os/exec. Crash-isolated.
	ModeSubprocess
)

// Options configures a [Server].
type Options struct {
	// Listen address for incoming Bedrock UDP. Defaults to ":19132".
	// Use [FlyGlobalServices] on Fly.io.
	Listen string

	// Upstream Java MC address (e.g. "127.0.0.1:25567" for Gate's bedrock listener).
	// Required.
	Upstream string

	// AuthType for forwarding to upstream. Default: [Floodgate].
	AuthType AuthType

	// FloodgateKey is 16 raw bytes (AES-128). Required when AuthType == [Floodgate].
	// Generate via [GenerateFloodgateKey].
	FloodgateKey []byte

	// MOTD shown to Bedrock clients.
	MOTD MOTD

	// Mode selects in-process vs subprocess. Default: [ModeEmbedded].
	Mode Mode

	// LibraryPath overrides the auto-located libgeyserlite.so. [ModeEmbedded] only.
	LibraryPath string

	// BinaryPath overrides the auto-located geyserlite native binary. [ModeSubprocess] only.
	BinaryPath string

	// JVMArgs overrides the default tuned JVM args (see [DefaultJVMArgs]).
	// nil = use the defaults. Has no effect in [ModeEmbedded] — those args
	// are baked into libgeyserlite.so at build time.
	JVMArgs []string

	// Logger receives Geyser stdout/stderr as structured records.
	// Defaults to [slog.Default].
	Logger *slog.Logger

	// RestartPolicy controls subprocess restart on crash.
	// nil = exponential backoff 1s..60s, infinite retries.
	// Has no effect in [ModeEmbedded].
	RestartPolicy *RestartPolicy

	// ShutdownTimeout is how long to wait for graceful shutdown after SIGTERM
	// before SIGKILL. Defaults to 30s.
	ShutdownTimeout time.Duration

	// Version is the geyserlite release tag (e.g. "v0.5.0") to fetch in the
	// auto-download path. Empty = [DefaultVersion]. Ignored if a binary or
	// library is supplied via path / env / embed.
	Version string

	// Mirror overrides the GitHub Release base URL (handy for air-gapped /
	// regulated environments). Empty = [DefaultDownloadBase].
	Mirror string

	// Offline disables the auto-download path. With Offline=true the locator
	// must succeed via path / env / embed / system search, or [Start] returns
	// an error.
	Offline bool

	// ConfigOverrides is an arbitrary YAML structure deep-merged into
	// the generated Geyser config.yml AFTER [Listen], [Upstream],
	// [AuthType], and [MOTD] have been applied. It's the escape hatch
	// for any Geyser setting the typed [Options] surface doesn't model
	// — `mtu`, `xbox-achievements-enabled`, `passthrough-motd`,
	// `max-players`, anything in Geyser's config.yml.
	//
	// Nested maps merge recursively (so you can override e.g. just
	// `bedrock.compression-level` without touching the rest of `bedrock`);
	// leaf values overwrite. Apply your overrides last by passing them
	// here rather than rewriting the whole config — that way the
	// baseline-bumping that ships with new geyserlite versions still
	// reaches you for the keys you didn't touch.
	//
	// Example:
	//
	//	geyserlite.Options{
	//	    Listen:   ":19132",
	//	    Upstream: "127.0.0.1:25567",
	//	    ConfigOverrides: map[string]any{
	//	        "bedrock":          map[string]any{"compression-level": 9},
	//	        "passthrough-motd": true,
	//	        "max-players":      50,
	//	    },
	//	}
	ConfigOverrides map[string]any
}

// MOTD is the Bedrock client-visible server description (two lines).
type MOTD struct {
	Line1, Line2 string
}

// RestartPolicy controls subprocess restart-on-crash behavior in [ModeSubprocess].
type RestartPolicy struct {
	// MinBackoff is the initial wait between restarts. Doubles up to MaxBackoff.
	MinBackoff time.Duration
	// MaxBackoff caps the wait between restarts.
	MaxBackoff time.Duration
	// MaxRetries limits total restarts (0 = infinite).
	MaxRetries int
}

// Sentinel errors returned by this package.
var (
	// ErrNotStarted is returned when Stop or Healthy is called before Start.
	ErrNotStarted = errors.New("geyserlite: server not started")
	// ErrAlreadyStarted is returned when Start is called twice.
	ErrAlreadyStarted = errors.New("geyserlite: server already started")
	// ErrNoBinary is returned when the geyserlite native binary can't be located.
	ErrNoBinary = errors.New("geyserlite: native binary not found (set Options.BinaryPath, $GEYSERLITE_BINARY, or build with -tags geyserlite_embed)")
	// ErrNoLibrary is returned when libgeyserlite.so can't be located.
	ErrNoLibrary = errors.New("geyserlite: libgeyserlite.so not found (set Options.LibraryPath, $GEYSERLITE_LIBRARY, or build with -tags geyserlite_embed)")
	// ErrInvalidFloodgateKey is returned when FloodgateKey is the wrong size.
	// Floodgate uses AES-128 (16 raw bytes). The upstream README's openssl RSA
	// example is wrong.
	ErrInvalidFloodgateKey = errors.New("geyserlite: FloodgateKey must be 16 bytes (AES-128)")
	// ErrUpstreamRequired is returned when Options.Upstream is empty.
	ErrUpstreamRequired = errors.New("geyserlite: Options.Upstream is required")
)

// fly — run on a Fly.io machine. The trick is binding Bedrock UDP to
// "fly-global-services" instead of "0.0.0.0" — Fly's UDP edge NATs
// external traffic to that special internal hostname, and binding
// 0.0.0.0 silently drops external packets. See docs/troubleshooting.md.
//
// fly.toml needs:
//
//	[[services]]
//	  internal_port = 19132
//	  protocol = "udp"
//	  [[services.ports]]
//	    port = 19132
//
// For NetherNet alongside RakNet, also expose TCP 19132 and UDP 19133,
// set GEYSER_SIGNALING_ADDRESS=0.0.0.0 in Fly's [env], and set
// GEYSER_ADVERTISE_ADDRESS to the dedicated public IPv4 address. The signaling
// bind setting works in embedded mode too; JVMArgs only applies to subprocesses.
//
// Plus a dedicated v4 IP (`fly ips allocate-v4` — UDP doesn't ride on
// shared IPs).
//
//	go run ./examples/fly
package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	geyserlite "go.minekube.com/geyserlite"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// FlyGlobalServices returns "fly-global-services" if FLY_APP_NAME is
	// set (always the case on Fly machines), else "0.0.0.0" — so the
	// same code works in dev too.
	listen := fmt.Sprintf("%s:19132", geyserlite.FlyGlobalServices())
	var overrides map[string]any
	if advertised := os.Getenv("GEYSER_ADVERTISE_ADDRESS"); advertised != "" {
		overrides = map[string]any{
			"bedrock": map[string]any{
				"transport": "both", "webrtc-port": 19133,
				"signaling": map[string]any{
					"mode": "builtin", "advertise-addresses": []string{advertised}, "stun-discovery": false,
				},
			},
		}
	}

	// Decode the Floodgate key from a Fly secret. Secrets are env vars,
	// so the 16 random bytes need a base64 wrapping.
	keyB64 := os.Getenv("FLOODGATE_KEY_BASE64")
	if keyB64 == "" {
		log.Fatal("FLOODGATE_KEY_BASE64 not set — fly secrets set FLOODGATE_KEY_BASE64=$(base64 < key.bin)")
	}
	key, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		log.Fatalf("decode floodgate key: %v", err)
	}

	// In a co-located deployment (Gate + Geyser in the same Fly machine),
	// upstream is loopback because Gate's bedrock listener is on 127.0.0.1.
	srv, err := geyserlite.New(geyserlite.Options{
		Listen:          listen,
		Upstream:        "127.0.0.1:25567",
		AuthType:        geyserlite.Floodgate,
		FloodgateKey:    key,
		ConfigOverrides: overrides,
		Logger:          slog.New(slog.NewJSONHandler(os.Stdout, nil)),
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := srv.Start(ctx); err != nil {
		log.Fatal(err)
	}
}

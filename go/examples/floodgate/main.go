// floodgate — connect to a Gate / Floodgate-aware upstream with shared
// AES-128 auth. The Bedrock player's identity is forwarded to the Java
// side cryptographically; the upstream sees a verified Bedrock player
// rather than an offline-mode connection.
//
// Pre-requisites:
//   - Upstream Java MC supports Floodgate (Gate has it built-in via
//     bedrock.enabled: true; Paper needs the Floodgate plugin).
//   - You've shared the same 16-byte AES key with the upstream.
//
//	FLOODGATE_KEY_FILE=/secrets/key.bin go run ./examples/floodgate
package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	geyserlite "go.minekube.com/geyserlite/go"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	keyPath := os.Getenv("FLOODGATE_KEY_FILE")
	if keyPath == "" {
		log.Fatal("set FLOODGATE_KEY_FILE to a 16-byte key (use scripts/floodgate-keygen.sh)")
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		log.Fatalf("read key: %v", err)
	}
	if len(key) != 16 {
		log.Fatalf("Floodgate key must be 16 bytes (AES-128); got %d. The upstream Geyser README's openssl RSA example is wrong.", len(key))
	}

	srv, err := geyserlite.New(geyserlite.Options{
		Listen:       ":19132",
		Upstream:     "gate.internal:25567", // Gate's bedrock listener
		AuthType:     geyserlite.Floodgate,
		FloodgateKey: key,
		MOTD:         geyserlite.MOTD{Line1: "minekube", Line2: "cross-play"},
		Logger:       slog.Default(),
	})
	if err != nil {
		log.Fatal(err)
	}

	if err := srv.Start(ctx); err != nil {
		log.Fatal(err)
	}
}

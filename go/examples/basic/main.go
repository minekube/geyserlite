// basic — minimum viable geyserlite: subprocess mode, offline auth, no
// fancy options. Run a Bedrock listener that forwards to a Java MC server
// that's also in offline mode.
//
//	go run ./examples/basic
package main

import (
	"context"
	"log"
	"log/slog"
	"os/signal"
	"syscall"

	geyserlite "go.minekube.com/geyserlite/go"
)

func main() {
	// Cancel on SIGINT/SIGTERM so the supervisor stops Geyser cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv, err := geyserlite.New(geyserlite.Options{
		Listen:   ":19132",            // public Bedrock UDP
		Upstream: "127.0.0.1:25565",   // Paper / other Java MC server
		AuthType: geyserlite.Offline,  // upstream is online-mode=false
		Logger:   slog.Default(),
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Println("starting geyserlite (subprocess fallback if no libgeyserlite.so)")
	if err := srv.Start(ctx); err != nil {
		log.Fatal(err)
	}
}

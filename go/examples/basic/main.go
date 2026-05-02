// Minimal geyserlite usage. Builds against go.minekube.com/geyserlite/go.
package main

import (
	"context"
	"log"
	"log/slog"
	"os/signal"
	"syscall"

	"go.minekube.com/geyserlite/go"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	key, err := geyserlite.GenerateFloodgateKey()
	if err != nil {
		log.Fatal(err)
	}

	srv, err := geyserlite.New(geyserlite.Options{
		Listen:       ":19132",
		Upstream:     "127.0.0.1:25567",
		AuthType:     geyserlite.Floodgate,
		FloodgateKey: key,
		Logger:       slog.Default(),
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Println("starting geyserlite (in-process mode)")
	if err := srv.Start(ctx); err != nil {
		log.Fatal(err)
	}
}

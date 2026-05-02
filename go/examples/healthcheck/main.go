// healthcheck — expose Server.Healthy() over HTTP for orchestrators
// (Kubernetes liveness/readiness, Fly health checks, plain monitoring).
// The HTTP handler is dumb on purpose: 200 if Geyser is accepting
// connections, 503 if it's still booting or has shut down.
//
// Geyserlite runs in a goroutine; the HTTP server is the foreground.
// SIGTERM/SIGINT cancels both via shared context.
//
//	go run ./examples/healthcheck
package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	geyserlite "go.minekube.com/geyserlite/go"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	key, _ := geyserlite.GenerateFloodgateKey()
	srv, err := geyserlite.New(geyserlite.Options{
		Upstream:     "127.0.0.1:25567",
		AuthType:     geyserlite.Floodgate,
		FloodgateKey: key,
		Logger:       slog.Default(),
	})
	if err != nil {
		log.Fatal(err)
	}

	// Start Geyser in the background; its lifecycle is bound to ctx.
	geyserDone := make(chan error, 1)
	go func() { geyserDone <- srv.Start(ctx) }()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if !srv.Healthy() {
			http.Error(w, "geyser not ready", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		// Same as health here. Distinguish startup-wait if you want.
		if !srv.Healthy() {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	httpSrv := &http.Server{
		Addr:              ":8086",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("healthcheck http: %v", err)
		}
	}()

	// Wait for either Geyser to exit or shutdown signal.
	select {
	case <-ctx.Done():
		log.Println("shutdown signal received")
	case err := <-geyserDone:
		log.Printf("geyser exited: %v", err)
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutCtx)
}

// custom-config — override the tuned defaults: bigger heap + more
// Netty workers for a host with more concurrent Bedrock players.
//
// The shipped defaults target a 256 MB Fly machine with one player.
// If you have more headroom and traffic, this is the example to crib
// from. Numbers below are illustrative; profile your own workload.
//
//	go run ./examples/custom-config
package main

import (
	"context"
	"log"
	"log/slog"
	"os/signal"
	"syscall"
	"time"

	geyserlite "go.minekube.com/geyserlite"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	key, _ := geyserlite.GenerateFloodgateKey()

	// Start from defaults and tweak. Each line is annotated with what it
	// trades off; see docs/tuning.md for the full table.
	jvmArgs := []string{
		"-Xmx256m",                                // 256 MB heap (default 64 MB)
		"-XX:MaxHeapFree=16m",                     // less aggressive heap shrink
		"-XX:+CollectYoungGenerationSeparately",
		"-Dio.netty.maxDirectMemory=64m",          // 4× the default direct buffer cap
		"-XX:MaxDirectMemorySize=64m",
		"-Dio.netty.allocator.type=pooled",        // pooled is faster under load (default unpooled)
		"-Dio.netty.allocator.numHeapArenas=4",    // tune to expected concurrency
		"-Dio.netty.allocator.numDirectArenas=4",
		"-Dio.netty.eventLoopThreads=8",           // 4× default — more cores → more event loops
		"-Dlog4j2.disableJmx=true",
	}

	srv, err := geyserlite.New(geyserlite.Options{
		Upstream:     "127.0.0.1:25567",
		AuthType:     geyserlite.Floodgate,
		FloodgateKey: key,
		JVMArgs:      jvmArgs,

		// Restart faster after crashes since this is a high-traffic deploy.
		RestartPolicy: &geyserlite.RestartPolicy{
			MinBackoff: 500 * time.Millisecond,
			MaxBackoff: 30 * time.Second,
			MaxRetries: 0, // infinite
		},

		// Give Geyser longer to drain players before SIGKILL on shutdown.
		ShutdownTimeout: 60 * time.Second,

		Logger: slog.Default(),

		// Subprocess mode for crash isolation under heavy load.
		Mode: geyserlite.ModeSubprocess,
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := srv.Start(ctx); err != nil {
		log.Fatal(err)
	}
}

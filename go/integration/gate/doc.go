// SPDX-License-Identifier: MIT

// Package gate adapts [geyserlite.Server] to the lifecycle shape Gate
// expects when running an embedded Bedrock listener.
//
// Gate is the Minekube Java MC proxy at https://github.com/minekube/gate.
// Historically Gate ran Geyser as a separate process (moxy's
// `geyser-bedrock/` sidecar pattern). This package replaces that with
// in-process geyserlite, eliminating the second process, the IPC, and
// the dual JVM/native footprint.
//
// # Wiring
//
// Gate's bedrock config is shaped like:
//
//	bedrock:
//	  enabled: true
//	  listen: 0.0.0.0:19132
//	  upstream: 127.0.0.1:25567
//	  authType: floodgate
//	  floodgateKey: <hex>
//	  motd:
//	    line1: My Server
//	    line2: cross-play
//
// Gate calls [New] with that struct and treats the returned [*Bedrock]
// like any other long-running subsystem: [Bedrock.Start] from a
// goroutine, [Bedrock.Stop] on shutdown, [Bedrock.Healthy] for health
// checks.
//
// # Why this lives in geyserlite, not in Gate
//
// Keeping the adapter here means Gate doesn't pin a transitive purego
// dep, doesn't have to track libgeyserlite.so location strategy, and
// doesn't see embedded-vs-subprocess mode plumbing. Gate just imports
// `go.minekube.com/geyserlite/integration/gate` and gets a clean
// lifecycle handle.
package gate

// Package geyserlite embeds GeyserMC's Bedrock-Java translation into Go
// programs as an in-process function call (default) or a managed
// subprocess (opt-in fallback).
//
// The native code lives in libgeyserlite.so, a GraalVM native-image build of
// Geyser that exports a small C ABI. The default mode dlopens the .so via
// purego — no cgo, no C toolchain at build time.
//
// # Quick start
//
//	key, _ := geyserlite.GenerateFloodgateKey()
//	srv, _ := geyserlite.New(geyserlite.Options{
//	    Listen:       ":19132",
//	    Upstream:     "127.0.0.1:25567",
//	    AuthType:     geyserlite.Floodgate,
//	    FloodgateKey: key,
//	})
//	log.Fatal(srv.Start(ctx))
//
// # Modes
//
// In-process (default): dlopen libgeyserlite.so. Lowest overhead. A native
// crash inside Geyser will kill the host process — recover() does not catch
// it. Use this when you've validated the binary and want simplicity.
//
// Subprocess: spawn libgeyserlite.so's executable form via os/exec. Crash
// isolation: a Geyser segfault won't take down your Go process. Use this
// when you want auto-restart-on-crash.
//
// See [Options.Mode] and the project ROADMAP for details.
package geyserlite

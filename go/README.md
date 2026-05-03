# `go.minekube.com/geyserlite`

[![Go Reference](https://pkg.go.dev/badge/go.minekube.com/geyserlite.svg)](https://pkg.go.dev/go.minekube.com/geyserlite)

Go library for embedding [geyserlite](../README.md) in your Go MC proxy.
The library `dlopen`s `libgeyserlite.so` and runs Geyser in the same
address space as your proxy — no separate process, no JVM, ~110 MB idle
RSS for the bedrock side.

## Install

```sh
go get go.minekube.com/geyserlite
```

Resolves through `go.minekube.com`'s Cloudflare Worker module proxy to
`github.com/minekube/geyserlite`'s `go/` subdirectory.

## Modes

| Mode | How | Crash isolation | Pick when |
|---|---|---|---|
| `ModeEmbedded` *(default)* | `purego.Dlopen("libgeyserlite.so")` | ❌ shared address space | normal use; lowest overhead |
| `ModeSubprocess` | `exec.CommandContext(geyserlitePath, …)` | ✅ separate process | hard isolation, dev, debugging |

Same `Server` API across both — switch with `Options.Mode`.

## Quick start

```go
package main

import (
    "context"
    "log"
    "log/slog"
    "os/signal"
    "syscall"

    "go.minekube.com/geyserlite"
)

func main() {
    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer stop()

    key, err := geyserlite.GenerateFloodgateKey()
    if err != nil { log.Fatal(err) }

    srv, err := geyserlite.New(geyserlite.Options{
        Listen:       geyserlite.FlyGlobalServices() + ":19132",
        Upstream:     "127.0.0.1:25567",
        AuthType:     geyserlite.Floodgate,
        FloodgateKey: key,
        Logger:       slog.Default(),
    })
    if err != nil { log.Fatal(err) }

    if err := srv.Start(ctx); err != nil { log.Fatal(err) }
}
```

More patterns under [`./examples/`](./examples/) — Floodgate keying, custom
config rendering, Fly.io UDP NAT helper, healthcheck integration.

## Library acquisition (`ModeEmbedded`)

`libgeyserlite.so` resolution order:

1. `Options.LibraryPath` — explicit override.
2. `$GEYSERLITE_LIBRARY` env var.
3. Embedded blob — built with `-tags geyserlite_embed`, `//go:embed`'d
   and self-extracted to `os.UserCacheDir/geyserlite/<sha>/`.
4. System paths (`/usr/lib`, `LD_LIBRARY_PATH`).
5. Auto-download from the matching GitHub Release with sha256 verify
   against `checksums.txt` (skipped when `Options.Offline`).

Production recipe: `go build -tags geyserlite_embed`. Ships a single
self-contained binary with no runtime acquisition step.

## Crash boundary (read this)

`ModeEmbedded` shares an address space with your host process. A native
segfault in `libgeyserlite.so` kills the entire process. Go's `recover()`
does not save you — it can't catch `SIGSEGV` from native code.
`ModeSubprocess` is the answer when you need OS-level isolation; the
process supervisor restarts on crash with exponential backoff and pipes
stdout/stderr through your `slog.Logger`.

[`../docs/troubleshooting.md`](../docs/troubleshooting.md) has the
common "things go wrong" recipes.

## Gate integration

`go.minekube.com/geyserlite/integration/gate` adapts `geyserlite.Server`
to the lifecycle Gate expects from a managed bedrock subsystem.
Config-driven, nil-receiver-safe lifecycle, hex floodgate-key parsing.

## See also

- [`../ROADMAP.md`](../ROADMAP.md) — phases, decisions, memory budgets
- [`../docs/architecture.md`](../docs/architecture.md) — how the C-ABI bridge works
- [`../docs/floodgate.md`](../docs/floodgate.md) — Floodgate key gotchas (it's AES-128, not RSA)
- [`./examples/`](./examples/) — runnable usage examples

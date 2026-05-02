# `go.minekube.com/geyserlite`

Go library for embedding [geyserlite](../README.md) in your Go MC proxy.

> **Status**: pre-v0.2 scaffolding. Public API in `geyserlite.go` is the
> intended shape; some bodies are still TODO. See [../ROADMAP.md](../ROADMAP.md).

## Install

```sh
go get go.minekube.com/geyserlite
```

The import path stays `go.minekube.com/geyserlite` even though the source
lives in this subdirectory; `go.minekube.com`'s vanity-URL server maps
the path to the right repo + subdir.

## Modes

| Mode | How | Crash isolation | When to pick |
|---|---|---|---|
| `ModeEmbedded` (default) | `purego.Dlopen("libgeyserlite.so")` | ❌ shared address space | production after validation |
| `ModeSubprocess` | `exec.CommandContext(geyserlitePath, …)` | ✅ separate process | dev, untrusted Geyser builds, hard isolation requirements |

Both expose the same `Server` API; switch via `Options.Mode`.

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

## Library acquisition (`ModeEmbedded`)

The default mode needs `libgeyserlite.so` available at runtime. Resolution order:

1. `Options.LibraryPath` — explicit override.
2. `$GEYSERLITE_LIBRARY` — env override.
3. Embedded — if built with `-tags geyserlite_embed`, the `.so` is `//go:embed`'d
   and self-extracts to `os.UserCacheDir/geyserlite/<sha>/libgeyserlite.so`.
4. System search paths (`/usr/lib`, `LD_LIBRARY_PATH`).
5. Auto-download from GitHub Release with checksum verification (skipped if `Options.Offline`).

Recommendation for production builds: `go build -tags geyserlite_embed`.

## See also

- [`../ROADMAP.md`](../ROADMAP.md) — full plan
- [`../docs/architecture.md`](../docs/architecture.md) — architecture overview
- [`../docs/floodgate.md`](../docs/floodgate.md) — Floodgate key gotchas
- [`./examples/`](./examples/) — runnable usage examples

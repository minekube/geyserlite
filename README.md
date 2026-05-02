<!-- markdownlint-disable MD041 -->

# geyserlite

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Discord](https://img.shields.io/discord/633708750032863232.svg?color=%237289da&label=discord)](https://minekube.com/discord)

> A small, fast, embeddable build of [GeyserMC](https://github.com/GeyserMC/Geyser) Bedrock-Java translation
> for **Go and Rust** programs and resource-constrained hosts. Runs in **~110 MB RAM** instead of ~440 MB.

`geyserlite` ships Geyser standalone compiled to a single static native ELF
via GraalVM `native-image`, plus first-class **Go and Rust libraries** that
supervise it (and, in a future release, embed it in-process).

The same native binary the libraries depend on is also a drop-in
replacement for `Geyser-Standalone.jar`, so you can use it without any
language wrapper at all.

> **Status**: pre-v0.1, scaffolding only. See [ROADMAP.md](./ROADMAP.md) for milestones.

## What you get

| Artifact | Use case |
|---|---|
| **Native binary** ([releases](https://github.com/minekube/geyserlite/releases)) | Drop-in replacement for `Geyser-Standalone.jar`. ~107 MB, no JVM, no deps. |
| **Container image** (`ghcr.io/minekube/geyserlite`) | `docker run ghcr.io/minekube/geyserlite`. `FROM scratch`-based. |
| **Go library** (`go.minekube.com/geyserlite/go`) | Embed Geyser in your Go MC proxy in 5 lines. |
| **Rust crate** (`geyserlite` on [crates.io](https://crates.io/crates/geyserlite)) | Embed Geyser in your Rust MC server in 5 lines. |
| **Compose example** (`deploy/compose/`) | Cross-play stack: Geyser + Paper, in 60 seconds. |

## Contributing / local dev

This repo uses [`mise`](https://mise.jdx.dev) to pin all dev tooling
(Go, Rust, GraalVM, `task`, linters) and [`task`](https://taskfile.dev)
as the workflow runner. After cloning:

```sh
mise trust && mise install     # installs everything in mise.toml
task                            # list tasks
task build:native               # GraalVM build of geyserlite + .so via Docker
task test                       # all language tests
task lint                       # all linters
```

`task` itself is installed by `mise`, so the only prerequisite is `mise`.

## Quick starts

### Run the binary directly

```sh
curl -fsSL -o geyserlite https://github.com/minekube/geyserlite/releases/latest/download/geyserlite-linux-amd64
chmod +x geyserlite
./geyserlite        # reads ./config.yml
```

### Docker

```sh
docker run --rm -p 19132:19132/udp -v ./config.yml:/config.yml \
  ghcr.io/minekube/geyserlite:latest
```

### Go

```go
import "go.minekube.com/geyserlite/go"

key, _ := geyserlite.GenerateFloodgateKey()
srv, _ := geyserlite.New(geyserlite.Options{
    Listen:       ":19132",
    Upstream:     "127.0.0.1:25567",
    AuthType:     geyserlite.Floodgate,
    FloodgateKey: key,
})
log.Fatal(srv.Start(ctx))
```

### Rust

```rust
use geyserlite::{Server, Options, AuthType};

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let key = geyserlite::generate_floodgate_key();
    Server::new(Options {
        listen: ":19132".into(),
        upstream: "127.0.0.1:25567".into(),
        auth_type: AuthType::Floodgate,
        floodgate_key: key,
        ..Default::default()
    })?
    .start()
    .await?;
    Ok(())
}
```

## Why does this exist?

JVM Geyser idles around **440 MB RSS**. That's larger than a `shared-cpu-1x`
Fly machine (256 MB), so co-locating Bedrock support with a Java proxy means
running it as its own machine — extra cost, extra IP, extra surface.

GraalVM `native-image` with the right flag set, agent metadata, runtime
tuning, and (eventually) PGO compresses Geyser to **~110 MB idle RSS**,
**~175 MB peak under load**. That fits next to a Go or Rust proxy in 256 MB
comfortably. This repository packages that recipe so anyone can use it.

## Architecture

```
Bedrock client ─UDP 19132─▶ geyserlite (native binary)
                                │
                                │ Floodgate AES-128 (loopback or TCP)
                                ▼
                        Java MC server / proxy
```

The Go and Rust libraries each wrap the binary as a managed subprocess
(v0.x) and, post v0.7, can embed it as an in-process function call via
`purego` (Go) / `libloading` (Rust).

See [docs/architecture.md](./docs/architecture.md) for details.

## Repository layout

```
geyserlite/
├── README.md
├── ROADMAP.md          ← what's planned
├── build/              ← shared native-image build pipeline
├── go/                 ← Go module: go.minekube.com/geyserlite/go
├── rust/               ← Rust crate: geyserlite (crates.io)
├── deploy/compose/     ← docker-compose self-host example
├── docs/
└── .github/workflows/
```

## Project status & roadmap

This is a **fresh scaffolding repo**. v0.1 builds the native binary in CI;
v0.2 ships the Go subprocess library; v0.3 ships the Rust subprocess
crate; v0.7 embeds in-process for both languages. See
[ROADMAP.md](./ROADMAP.md) for the full milestone list, decisions log,
and memory budgets.

## Related projects

- [GeyserMC/Geyser](https://github.com/GeyserMC/Geyser) — upstream Bedrock-Java translator (Java)
- [`geyserite`](https://github.com/...) — Rust port of Geyser (a different effort, not us)
- [`minekube/gate`](https://github.com/minekube/gate) — Go MC proxy (first Go consumer)
- [`minekube/connect-java`](https://github.com/minekube/connect-java) — Connect plugin for backend MC servers
- [valence-rs/valence](https://github.com/valence-rs/valence) — Rust MC server framework (potential Rust consumer)

## License

MIT. Geyser itself is MIT — see upstream for protocol mappings copyright.

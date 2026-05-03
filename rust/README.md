# `geyserlite` (Rust crate)

[![Crates.io](https://img.shields.io/crates/v/geyserlite.svg?color=ff7043)](https://crates.io/crates/geyserlite)
[![docs.rs](https://img.shields.io/docsrs/geyserlite)](https://docs.rs/geyserlite)

Rust crate for embedding [geyserlite](../README.md) in your Rust MC
server or proxy. The crate `dlopen`s `libgeyserlite.so` and runs Geyser
in the same address space — no separate process, no JVM, ~110 MB idle
RSS for the bedrock side.

## Install

```sh
cargo add geyserlite
```

Optional features:

```toml
[dependencies]
geyserlite = { version = "0.1", features = ["embed"] }    # bundle the .so
geyserlite = { version = "0.1", features = ["download"] } # auto-fetch at runtime
```

Default build is "bring your own .so" — point `GEYSERLITE_LIBRARY` or
`Options::library_path` at it. The two features above remove that step.

## Modes

| Mode | How | Crash isolation | Pick when |
|---|---|---|---|
| `Mode::Embedded` *(default)* | `libloading::Library::new("libgeyserlite.so")` | ❌ shared address space | normal use; lowest overhead |
| `Mode::Subprocess` | `tokio::process::Command` | ✅ separate process | hard isolation, dev, debugging |

Same [`Server`] API — switch via `Options::mode`.

## Quick start

```rust,no_run
use geyserlite::{Server, Options, AuthType};

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    Server::new(Options {
        listen: format!("{}:19132", geyserlite::fly_global_services()),
        upstream: "127.0.0.1:25567".into(),
        auth_type: AuthType::Floodgate,
        floodgate_key: geyserlite::generate_floodgate_key(),
        ..Default::default()
    })?
    .start()
    .await?;
    Ok(())
}
```

[`examples/`](./examples/) covers Floodgate keying, custom config,
Fly.io UDP NAT helper, healthcheck integration, and a CI integration
probe.

## Library acquisition (`Mode::Embedded`)

`libgeyserlite.so` resolution order:

1. `Options::library_path` — explicit override.
2. `GEYSERLITE_LIBRARY` env var.
3. Embedded blob — built with `--features embed`, `include_bytes!`'d
   and self-extracted to `dirs::cache_dir()/geyserlite/<sha>/`.
4. System paths (`LD_LIBRARY_PATH`, `/usr/lib`).
5. Auto-download from the matching GitHub Release with sha256 verify
   (`--features download`).

Production recipe: `cargo build --release --features embed`. Single
self-contained binary, no runtime acquisition.

## Crash boundary (read this)

`Mode::Embedded` shares an address space with your host. A native
segfault in `libgeyserlite.so` kills the whole process; `catch_unwind`
does **not** save you — it only catches Rust panics, not `SIGSEGV` from
native code. `Mode::Subprocess` is the answer when you need OS-level
isolation; restart-on-crash with exponential backoff is built in, with
stdout/stderr piped through `tracing`.

[`../docs/troubleshooting.md`](../docs/troubleshooting.md) has common
"things go wrong" recipes.

## MSRV / runtime

- Stable Rust **1.85+**, Edition **2024**. No nightly features.
- Async API uses `tokio`. A runtime-agnostic API is on the
  open-questions list — see [`../ROADMAP.md`](../ROADMAP.md) if you
  have a use case for it.

## See also

- [`../ROADMAP.md`](../ROADMAP.md) — phases, decisions, memory budgets
- [`../docs/architecture.md`](../docs/architecture.md) — how the C-ABI bridge works
- [`../docs/floodgate.md`](../docs/floodgate.md) — Floodgate is AES-128, not RSA
- [`./examples/`](./examples/) — runnable usage examples

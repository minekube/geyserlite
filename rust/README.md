# `geyserlite` (Rust crate)

Rust crate for embedding [geyserlite](../README.md) in your Rust MC server or proxy.

> **Status**: pre-v0.3 scaffolding. Public API in `src/lib.rs` is the
> intended shape; bodies return `not implemented`. See [../ROADMAP.md](../ROADMAP.md).

## Install

```sh
cargo add geyserlite
```

Optional features:

```toml
[dependencies]
geyserlite = { version = "0", features = ["embed"] }    # //go:embed-equivalent
# or
geyserlite = { version = "0", features = ["download"] } # auto-fetch libgeyserlite.so
```

## Modes

| Mode | How | Crash isolation | When to pick |
|---|---|---|---|
| `Mode::Embedded` (default) | `libloading::Library::new("libgeyserlite.so")` | ❌ shared address space | production after validation |
| `Mode::Subprocess` | `tokio::process::Command` | ✅ separate process | dev, untrusted builds, hard isolation requirements |

Both expose the same [`Server`] API; switch via `Options::mode`.

## Quick start

```rust
use geyserlite::{Server, Options, AuthType};

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let key = geyserlite::generate_floodgate_key();
    Server::new(Options {
        listen: format!("{}:19132", geyserlite::fly_global_services()),
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

## Library acquisition (`Mode::Embedded`)

Resolution order:

1. `Options::library_path` — explicit override.
2. `GEYSERLITE_LIBRARY` env var.
3. Embedded — if built with `--features embed`, the `.so` is `include_bytes!`'d
   and self-extracts to `dirs::cache_dir()/geyserlite/<sha>/libgeyserlite.so`.
4. System search paths (`LD_LIBRARY_PATH`, `/usr/lib`).
5. Auto-download from GitHub Release with checksum verification (v0.5+, requires `--features download`).

Recommendation for production builds: `cargo build --features embed`.

## MSRV

Stable Rust 1.85+ (Edition 2024). No nightly features required.

## Async runtime

Uses `tokio` by default. Async-runtime-agnostic API is on the open-questions list;
see [`../ROADMAP.md`](../ROADMAP.md) if you have a use case for it.

## See also

- [`../ROADMAP.md`](../ROADMAP.md) — full plan
- [`../docs/architecture.md`](../docs/architecture.md) — architecture overview
- [`../docs/floodgate.md`](../docs/floodgate.md) — Floodgate key gotchas

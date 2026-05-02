# Rust examples

Self-contained programs demonstrating common geyserlite use cases.
Each is one file, runnable with `cargo run --example <name>`.

| Example | Demonstrates |
|---|---|
| [`basic.rs`](./basic.rs) | Minimum-viable subprocess mode against an offline-mode Java backend. |
| [`floodgate.rs`](./floodgate.rs) | AES-128 Floodgate auth to a Gate-style upstream. |
| [`fly.rs`](./fly.rs) | Fly.io deployment with `fly-global-services` UDP binding + base64 Floodgate secret. |
| [`healthcheck.rs`](./healthcheck.rs) | Bare-bones HTTP server exposing `Server::healthy()` on `:8086`. |
| [`custom_config.rs`](./custom_config.rs) | Override the 256 MB-tuned defaults for a beefier host: bigger heap, more Netty workers, tighter restart backoff, subprocess mode. |

`Mode::Embedded` (the default) needs `libgeyserlite.so` available at
runtime — set it via `Options::library_path`, `$GEYSERLITE_LIBRARY`,
build with `--features embed` after `task embed:fetch`, or
`--features download` to fetch from a GitHub Release.

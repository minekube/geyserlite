# Go examples

Self-contained programs demonstrating common geyserlite use cases.
Each is one folder, runnable with `go run ./examples/<name>`.

| Example | Demonstrates |
|---|---|
| [`basic/`](./basic) | Minimum-viable subprocess mode against an offline-mode Java backend. |
| [`floodgate/`](./floodgate) | AES-128 Floodgate auth to a Gate-style upstream. |
| [`fly/`](./fly) | Fly.io deployment with `fly-global-services` UDP binding + base64-encoded Floodgate secret. |
| [`healthcheck/`](./healthcheck) | HTTP `/healthz` + `/readyz` endpoints exposing `Server.Healthy()` for orchestrators. |
| [`custom-config/`](./custom-config) | Override the 256 MB-tuned defaults for a beefier host: bigger heap, more Netty workers, tighter restart backoff. |

Use `geyserlite.Options.Mode = geyserlite.ModeEmbedded` (the default) to
run the in-process variant via `purego` once `libgeyserlite.so` is
locatable. The embedded path uses the same API; switching is a one-line
change.

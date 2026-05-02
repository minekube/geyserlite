# Troubleshooting

## Boot

### `Unable to find resource: permissions.yml`

The `permissions.yml` file must exist in the working directory Geyser runs
from. The OCI image bundles it at `/permissions.yml`. If you're running the
binary directly, copy it next to your `config.yml`:

```sh
curl -fsSL -o permissions.yml \
  https://raw.githubusercontent.com/GeyserMC/Geyser/master/bootstrap/standalone/src/main/resources/permissions.yml
```

### Substrate VM error: `Defining hidden classes at runtime is not supported`

Comes from log4j-core's `WatchEventService` ServiceLoader hitting
`LambdaMetafactory` at runtime. Fixed in shipped builds via
`--initialize-at-build-time=org.apache.logging.log4j` — if you see it on a
locally-built binary, your build flags are missing that.

### `failed to create cipher: invalid key length for AES: must be 16, 24, or 32 bytes`

Your Floodgate key is the wrong shape. Floodgate uses **AES-128** (16 raw
bytes), not RSA. See [`floodgate.md`](./floodgate.md).

## Network

### Bedrock client can't connect on Fly.io but TCP services work

Fly's UDP edge NATs external traffic to a special internal IPv4 address.
Your Geyser config must bind to it, not `0.0.0.0`:

```yaml
bedrock:
  address: fly-global-services
  port: 19132
```

Or in code:

```go
opts.Listen = geyserlite.FlyGlobalServices() + ":19132"
```

```rust
opts.listen = format!("{}:19132", geyserlite::fly_global_services());
```

### `mcstatus` times out but raw `nc -u` works

`mcstatus` defaults to a 3-second timeout, which is sometimes shorter than
the round-trip + RakNet processing. Try increasing the timeout, or use the
raw probe approach:

```sh
printf '\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00\xff\xff\x00\xfe\xfe\xfe\xfe\xfd\xfd\xfd\xfd\x12\x34\x56\x78\x00\x00\x00\x00\x00\x00\x00\x01' \
  | nc -u -w 5 <ip> 19132 | xxd | head
```

Look for `MCPE;<your-motd>` in the response.

## Routing

### Player joins but lands in a different/unexpected world

If using Connect (`*.play.minekube.net`), check that the bedrock client
typed the **exact** endpoint hostname, including hyphens. The endpoint name
`orchid-maul` is different from `orchidmaul` — the latter belongs to a
different operator's server. Connect's hostname routing matches strictly.

### "Falling back to browser" / hub

```
failed to connect player to endpoint, falling back to browser
```

Means the named endpoint either doesn't exist or rejected the player. The
fallback is Connect's hub server, not your Paper. Check:

- Endpoint name is exactly correct (no typos, hyphens included).
- The Connect plugin on Paper has `allow-offline-mode-players: true` if
  Paper itself is in offline mode.
- The Floodgate AES key on both sides matches byte-for-byte.

## In-process embedding (Go / Rust)

### `dlopen failed: libgeyserlite.so: cannot open shared object file`

The library can't find the `.so`. In order:

1. Set `Options.LibraryPath` / `Options::library_path` to the absolute path.
2. Set `$GEYSERLITE_LIBRARY` environment variable.
3. Build with the `embed` build tag / feature so the `.so` is bundled.
4. Place the `.so` in `/usr/lib` or somewhere on `LD_LIBRARY_PATH`.

### Process dies with SIGSEGV during play

Native crashes inside `libgeyserlite.so` propagate to the host process — Go's
`recover()` and Rust's `catch_unwind` cannot save you. Switch to subprocess
mode (`Mode::Subprocess`) for crash isolation. See
[`architecture.md`](./architecture.md) for the trade-off discussion.

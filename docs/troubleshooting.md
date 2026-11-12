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

### Last log line is `Downloading Minecraft JAR to extract required files`

**This is not a startup stall, and the server is already up.** Geyser hands
the client-JAR work to `CompletableFuture.runAsync`. In practice, the Mojang
manifest fetch usually outlasts local startup, so the RakNet listener binds
and `Done (…s)!` normally prints *before* that line appears. The ordering is a
race, not a guarantee:

```
Started Geyser on UDP port 19132
Done (1.584s)! Run /geyser help for help!
Downloading Minecraft JAR to extract required files, please wait...   <- last line
```

Either way, the async task does not block the listener from binding. Check the
UDP port rather than inferring readiness from the log tail:

```sh
go run go.minekube.com/geyserlite/cmd/bedrock-probe@latest <ip>:19132
```

### Player skins show the empty checkerboard texture

Upgrade to a build containing the AWT-free player-skin decoder. Older native
builds decoded the 18 default skins and downloaded Java skins with
`ImageIO.read`; because the native artifact has no AWT libraries, that path
fell back to `SkinProvider.EMPTY_SKIN`.

Current builds decode those PNGs with a pure-Java decoder and preserve
Geyser's size validation and opaque-pixel mask. The native image still does
not provide general AWT support; see
[Why AWT cannot be enabled](../build/README.md#why-awt-cannot-be-enabled).

## Network

### Bedrock client can't connect on Fly.io but TCP services work

NetherNet needs a separate TCP signaling bind: set
`GEYSERLITE_SIGNALING_ADDRESS=0.0.0.0` in the Fly app environment. The UDP
`bedrock.address` remains `fly-global-services`; TCP cannot bind there.
With `bedrock.transport: both`, keep UDP 19132 for RakNet, expose TCP 19132
for signaling and set/expose `bedrock.webrtc-port: 19133` over UDP. Set
`bedrock.signaling.advertise-addresses` to the dedicated public IPv4 address.
This environment setting also works in Go/Rust embedded mode.

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

### Bedrock sessions disconnect or stall intermittently

For a limited staging reproduction, enable the packet trace before starting
geyserlite:

```sh
GEYSERLITE_BEDROCK_PACKET_TRACE=1 ./geyserlite
```

The trace keeps the most recent 256 packet-metadata and channel-event records
in memory and dumps them, packet counts, channel state, and raw UDP statistics
when the session disconnects or raises an exception. It records packet types,
IDs, flags, and sizes, but not decoded packet payloads. The logs still contain
player names, socket addresses, disconnect reasons, event and exception
messages, and timing data, so handle them as sensitive diagnostic output and
disable the trace after collecting the reproduction.

Optional settings:

- `GEYSERLITE_BEDROCK_PACKET_TRACE_RING` changes the retained record count;
  values are clamped to 16-4096, and invalid values use 256.
- `GEYSERLITE_BEDROCK_PACKET_TRACE_VERBOSE=1` logs each packet-metadata and
  channel-event record as it occurs and can produce high-volume output.

### `Flying is not enabled on this server` moments after a Bedrock player joins

A converted Bedrock client that is suspended in the air — no terrain under it
yet, no vertical motion — is a state a Java client cannot produce, and Paper's
floating check latches on it: `clientIsFloating` is geometric and counts server
ticks, so it disconnects the player after 81 ticks (~4.05 s) with
`allow-flight=false`. The client's own on-ground flag does not help; the check
reads the position in the last movement packet.

Fixed across v0.7.2 – v0.7.4: a pending placement is resolved against the
world's own surface anywhere in the column (not only a 32-block window), the
hold survives on evidence instead of a 5 s timer, and while a placement cannot
be proved against real terrain **no movement sample is forwarded at all** —
sending the frozen position at an unsupported height re-arms the counter. On
v0.7.5 and newer the end of a hold is logged, so a hold that was released by a
timeout is distinguishable from one that was released by terrain.

If you still see this kick, capture the hold-end line plus
`GEYSERLITE_BEDROCK_PACKET_TRACE=1`; the counter is in the backend's own ticks,
so the backend's `server.properties` (`allow-flight`) and the player's
placement both matter.

### A player hangs on "generating world" and the log claims a login that never happened

Before v0.7.6, a session that exhausted the heap (for example inside
`ZlibCompression.encode` under `-Xmx256m`) terminated the Netty player
event-loop threads permanently. The player event-loop group was created once per
transport, so every later NetherNet session was bound to a dead executor:
`connectDownstream()` threw `RejectedExecutionException: event executor
terminated`, `authenticate()` swallowed it, the client sat on "generating world"
until its own timeout with its XUID still reserved, and the edge still logged
`Player connected with username <x>` after the throw.

v0.7.6 replaces a terminated thread group instead of handing it to new sessions.
On an older build the recovery is a restart — and the reserved XUID means the
same player cannot retry until the process is restarted.

### `Timed out` about 30 s after spawn while the session looks healthy

Before v0.6.1 the baseline config booted Geyser with
`gameplay.forward-player-ping: true`, so the Java backend's keepalive was
answered only when the Bedrock client echoed `NetworkStackLatency`. A
backgrounded or throttled mobile client, or a lossy UDP leg, starves that echo
and the backend drops the player with `Timed out` even though everything else is
healthy. Upstream Geyser's default for this setting is `false`; if you run a
custom `config.yml`, check that you have not set it back to `true`.

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

# build/

The native artifact pipeline. Anything in this directory affects the
`geyserlite` ELF and the `libgeyserlite.so` shared library — that is, the
products everything else in the repo wraps.

## Files

| File | Role |
|---|---|
| `geyser.version` | Pinned upstream `GeyserMC/Geyser` git ref. **Renovate-tracked.** |
| `graalvm.version` | Pinned `ghcr.io/graalvm/native-image-community` image digest. Renovate-tracked. |
| `Dockerfile` | Multi-stage GraalVM build that produces both the ELF and the `.so`. |
| `apply-overlay.sh` | Clones Geyser at `geyser.version`, copies `overlay/`, then applies intent-based mutations and `patches/`. |
| `flags.sh` | The full set of `native-image` flags we ship with, each annotated with what it costs/saves. Single source of truth — Dockerfile sources this. |
| `overlay/` | Files **added** to upstream Geyser before build (additive — never overwrites). |
| `patches/` | `.patch` files applied to upstream Geyser sources (numbered, applied in order). |
| `agent-config/` | GraalVM tracing-agent reflection metadata captured from a real login. Required for native-image to know what classes Gson/Netty/Floodgate reflect. |

## How it produces two artifacts

The `Dockerfile` runs `native-image` twice on the same Geyser source tree:

1. **Standalone executable** (the `geyserlite` ELF) — Geyser's normal main.
   Drop-in for `Geyser-Standalone.jar`.
2. **Shared library** (`libgeyserlite.so` + `libgeyserlite.h`) — built with
   `--shared` from the same code, exporting the `@CEntryPoint`-annotated
   functions in `overlay/geyserlite-native/.../GeyserBridge.java`.

Both share the same flags (`flags.sh`) so they have the same memory profile.

## Soft-fork pattern

We don't fork `GeyserMC/Geyser`. We clone the upstream repo at the pinned
ref, then apply our changes as an overlay + minimal patches. See the
"Soft-fork & sync strategy" section in [`../ROADMAP.md`](../ROADMAP.md).

## Updating Geyser

Renovate handles this automatically: it watches `GeyserMC/Geyser` master
and opens a PR bumping `geyser.version`. CI re-applies overlay + patches.
Clean? Auto-merged. Conflict? PR stays open for human attention.

To do it manually:

```sh
echo <new-sha> > build/geyser.version
./build/apply-overlay.sh   # smoke-test locally
git commit -am "chore: bump geyser.version"
git push                    # CI takes over
```

## Refreshing reflection metadata

GraalVM's static analyzer can't see reflective access. We ship a captured
`agent-config/` so the binary works without a live agent run. Refresh when
Geyser changes its reflection surface (rare — major Bedrock protocol bumps
mostly):

```sh
cd /tmp
git clone --recurse-submodules https://github.com/GeyserMC/Geyser.git
cd Geyser && ./gradlew :standalone:shadowJar
# Run with the GraalVM tracing agent attached:
$GRAALVM_HOME/bin/java \
  -agentlib:native-image-agent=config-merge-dir=$GEYSERLITE/build/agent-config \
  -jar bootstrap/standalone/build/libs/Geyser-Standalone.jar --nogui
# In another terminal: connect from a Bedrock client; play 30s; disconnect.
# Stop the JVM with SIGTERM (so the agent flushes).
# Then commit the updated agent-config/.
```

## Local build (for development)

Requires Docker. `make build` (or directly):

```sh
docker build -f build/Dockerfile -t geyserlite-build .
```

Outputs are extracted from the build container; see the Dockerfile for
the exact tags.

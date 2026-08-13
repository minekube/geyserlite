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
| `flags.sh` | Annotated `native-image` flags for the standalone executable. The shared-library Gradle build mirrors the compatible flags. |
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

The executable sources `flags.sh`. The shared-library Gradle build mirrors
the compatible flags and owns shared-only settings such as `--shared`.

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

## Why AWT cannot be enabled

The shipped binaries have no AWT. GeyserLite therefore patches the default
player-skin and downloaded Java-skin paths to decode PNGs with the pure-Java
PNGJ library; those paths no longer call `ImageIO`. Other Geyser features that
still use AWT remain unavailable. **Bundling `libawt*.so` does not fix this for
the artifact we actually ship.** Measured
against the pinned `graalvm.version` (`native-image-community:25-ol9`,
JDK 25.0.2) so nobody re-runs the experiment:

1. **native-image never links AWT statically.** It classifies
   `libawt.so`, `libawt_headless.so`, `libawt_xawt.so`, `libjavajpeg.so`,
   `liblcms.so`, and `libfontmanager.so` as `jdk_library` build artifacts and
   writes them *next to* the image, to be loaded at run time via
   `NativeLibraries.loadLibraryRelative`. The image ships
   `lib/static/linux-*/{glibc,musl}/libawt.a`, but nothing links it and
   `native-image --expert-options-all` exposes no option to force it.
   `-H:+StaticExecutableWithDynamicLibC` does not change the classification.

2. **Our amd64 build is `--static --libc=musl`, which forecloses it.** Under
   those flags native-image emits *no* AWT libraries at all, and the ELF has
   no dynamic section (`readelf -d` → `There is no dynamic section in this
   file`). It therefore cannot `dlopen` anything: copying the JDK's
   `libawt.so` next to the binary changes nothing. Verified directly.

3. **On the dynamic arm64 link, bundling does work.** A probe running
   `ProvidedSkins`' exact path — `ImageIO.read` followed by
   `SkinProvider.bufferedImageToImageData`'s `getRGB` sweep — decodes 64x64
   RGBA PNGs 9/9 where the unbundled image fails 9/9. (Representative PNGs,
   not the real Mojang skin assets.) It needs the sibling `.so` files present
   *next to the executable*, plus one JNI registration `agent-config/` lacks
   (`java.awt.image.SinglePixelPackedSampleModel`: `bitMasks`, `bitOffsets`,
   `bitSizes`, `maxBitSize`). Headless is auto-detected in a container; no
   `-Djava.awt.headless=true` was required.

Point 3 is not shippable on its own. Releases publish **bare single-file
binaries** (`geyserlite-linux-<arch>`), and the Go/Rust auto-download path
resolves one file against `checksums.txt`; sibling `.so` files have nowhere
to ride along. Taking it would also mean skins working on arm64 and not on
amd64.

General AWT support would still require one of the following; these are not
small, self-contained build-only changes:

- **Upstream GraalVM**: static AWT linking, or AWT support under `--static`.
- **Drop `--static --libc=musl` on amd64** and ship a multi-file artifact.
  That gives up the single self-contained ELF with no glibc dependency —
  the property the flag set was built around (see
  [`../ROADMAP.md`](../ROADMAP.md)) — and changes the release contract for
  every consumer.
- **Replace each remaining AWT path** with a native-compatible implementation,
  as the player-skin patch does for PNG decoding. This must be handled per
  feature because AWT is used for more than image decoding.

Do not spend time on `-Djava.awt.headless=true` or on copying `libawt*.so`
into the runtime stage: neither reaches the released amd64 binary.

## Local build (for development)

Requires Docker. `make build` (or directly):

```sh
docker build -f build/Dockerfile -t geyserlite-build .
```

Outputs are extracted from the build container; see the Dockerfile for
the exact tags.

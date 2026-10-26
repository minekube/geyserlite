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

2. **The former amd64 build used `--static --libc=musl`.** Under
   those flags native-image emits *no* AWT libraries at all, and the ELF has
   no dynamic section (`readelf -d` → `There is no dynamic section in this
   file`). It therefore cannot `dlopen` anything: copying the JDK's
   `libawt.so` next to the binary changes nothing. Verified directly.

3. **On a dynamic glibc link, bundling does work.** An arm64 probe running
   `ProvidedSkins`' exact path — `ImageIO.read` followed by
   `SkinProvider.bufferedImageToImageData`'s `getRGB` sweep — decodes 64x64
   RGBA PNGs 9/9 where the unbundled image fails 9/9. (Representative PNGs,
   not the real Mojang skin assets.) It needs the sibling `.so` files present
   *next to the executable*, plus one JNI registration `agent-config/` lacks
   (`java.awt.image.SinglePixelPackedSampleModel`: `bitMasks`, `bitOffsets`,
   `bitSizes`, `maxBitSize`). Headless is auto-detected in a container; no
   `-Djava.awt.headless=true` was required.

Both Linux architectures now use dynamic glibc for NetherNet's libdatachannel
JNI library. This removes the historical static-link restriction in point 2;
it does not bundle AWT's sibling libraries or supply their JNI registrations.

Point 3 is not shippable on its own. Releases publish **bare single-file
binaries** (`geyserlite-linux-<arch>`), and the Go/Rust auto-download path
resolves one file against `checksums.txt`; sibling `.so` files have nowhere
to ride along. Taking it would also mean skins working on arm64 and not on
amd64 unless all target architectures were verified.

General AWT support would still require one of the following; these are not
small, self-contained build-only changes:

- **Upstream GraalVM**: static AWT linking, or AWT support under `--static`.
- **Ship a multi-file artifact** containing the required JDK sibling libraries
  and their JNI registrations. The dynamic libc requirement is already in
  place for NetherNet, but the single-file download contract still needs work.
- **Replace each remaining AWT path** with a native-compatible implementation,
  as the player-skin patch does for PNG decoding. This must be handled per
  feature because AWT is used for more than image decoding.

Do not spend time on `-Djava.awt.headless=true` or on copying `libawt*.so`
into the runtime stage: the release download path does not distribute them.

## NetherNet native acceptance

The executable, Windows build and shared-library build retain upstream
`libdatachannel-java` native resources. Linux executables use dynamic glibc
because static musl executables cannot load JNI libraries at runtime.
`agent-config/jni-config.json` contains the libdatachannel JNI
entries selected from a GraalVM 25 tracing-agent run of Geyser `036293d`,
libdatachannel `0.24.6.0-20261020.080217-6`, Linux arm64, with both transports
and built-in signaling enabled. Its JNI loader eagerly resolves the callback
methods when loading the library, including callbacks not used during startup.
The minimal container already supplies OpenSSL3; its runtime stage also copies
libgcc_s and zlib from Debian. Without libgcc_s, libdatachannel cannot load even
though its JNI resource is present. Verify all DT_NEEDED entries, not only the
executable's direct dependencies, when changing the runtime base.

The selected entries are converted to the legacy JNI metadata schema so both
the Linux GraalVM25 and Windows GraalVM21 builds use the same registrations.

The native-image CI smoke uses `nethernet-smoke/config.yml` in an isolated
container. The separate Go module performs HTTP discovery, signed SDP/ICE/
DTLS/SCTP negotiation and a Bedrock NetworkSettings round trip over WebRTC;
the existing RakNet probe runs against the same process. The fixture disables
Xbox identity validation only for this synthetic connection and must never
be used as a production configuration. Production joins still need a real
authenticated client, and this test does not claim support for an unreleased
Bedrock packet codec.

CI maps `fly-global-services` to loopback alias `127.0.0.2` and uses the host
network. TCP discovery targets `127.0.0.1`, while both UDP transports bind
the alias. Without the environment override, discovery cannot reach that
different bind; this also exercises the embedded-accessible override path.

On Fly, keep the UDP bind on `fly-global-services`, and set
`GEYSER_SIGNALING_ADDRESS=0.0.0.0` for the TCP signaling listener. This works
for both embedding APIs and subprocess mode. The subprocess system property
`-DgeyserSignalingAddress` takes precedence over that environment setting.
For `transport: both`, expose TCP 19132, keep RakNet UDP 19132, and expose
WebRTC UDP 19133 without port rewriting. Advertise the app's dedicated IPv4
address with `bedrock.signaling.advertise-addresses`; Fly's egress/STUN address
is not the public service address.

## Temporary Bedrock 26.60 compatibility patch

Stable Geyser `036293d` includes NetherNet but advertises codecs only through
26.52. Patch `0009` is the unmodified upstream feature-branch commit
[`10607971`](https://github.com/GeyserMC/Geyser/commit/10607971cb4400906badfe352b2906a6998a6290)
of 21 October 2026, adding protocol 2225, its mappings and block/item assets.
It is applied to current stable instead of downgrading to that older branch,
which lacks the later NetherNet CR6, native-library and signaling fixes.
This is an explicit preview exception ahead of 26.60's planned 27 October
release, not an assertion that upstream has released stable 26.60 support.

Remove `0009` in the same PR that updates `build/geyser.version` to upstream's
stable 26.60 commit. Patch application is deliberately fail-closed, so an
upstream bump cannot silently retain or double-apply this temporary change.
The post-patch submodule update checks out the mapping commit the preview
requires. The native WebRTC smoke uses the actual advertised codec and thus
also exercises the protocol-2225 NetworkSettings path, while older-client
joins still need the RakNet and NetherNet canary gates.

Patch `0010` corrects a startup defect in that preview: `Old.INSTANCE`
inherited the parent singleton and bypassed the old palette's block remapping,
which aborted startup on `minecraft:poplar_planks` in the 26.30 palette.
The regression test requires the pale-oak replacement; it fails without the
subclass singleton. Reconcile/remove this patch together with `0009`.

## Local build (for development)

Requires Docker. `make build` (or directly):

```sh
docker build -f build/Dockerfile -t geyserlite-build .
```

Outputs are extracted from the build container; see the Dockerfile for
the exact tags.

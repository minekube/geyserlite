# geyserlite roadmap

> Living document. Last updated 2026-05-02.

## Vision

Ship GeyserMC's Bedrock-Java translation as a small, fast, **in-process
embeddable** native artifact suitable for resource-constrained hosts. Close
the gap between "Geyser standalone JVM (~440 MB RAM)" and "modern small
cloud VMs (256 MB)" so Bedrock cross-play stops requiring its own dedicated
machine alongside a Java proxy or server.

Five shipping forms, all built from one source of truth, **first-class
support for both Go and Rust**:

1. **Native binary** — drop-in replacement for `Geyser-Standalone.jar`.
2. **Shared library** (`libgeyserlite.so`) — what the Go and Rust libraries
   `dlopen` for in-process embedding. **This is the headline mode.**
3. **Container image** — `FROM scratch` + binary, for Docker users.
4. **Go library** — `go.minekube.com/geyserlite/go`, in-process default.
5. **Rust crate** — `geyserlite` on crates.io, in-process default.

The Go and Rust libraries are peers, not first-class/second-class. Both
load the same shared library, ship in the same release cycle, and provide
ergonomic wrappers over the C ABI exported from `libgeyserlite.so`.

A Gate adapter follows in v0.5 once the libraries land — Gate becomes the
first production consumer.

## Non-goals

- Replacing or duplicating GeyserMC's protocol translation work.
- Maintaining a hard fork that diverges from upstream.
- Supporting Spigot/Paper/Bukkit as plugin platforms — those have first-party
  Geyser plugins already.
- Kubernetes / Helm / Fly templates in v1.0. (Anyone can wrap the OCI image.)
- Multiple Geyser instances inside one process. (Deferred indefinitely;
  no concrete user need yet.)
- Windows support. (Server-side proxies are Linux-first.)
- Language bindings beyond Go and Rust in v1.0. (The C ABI from v0.1
  means anyone can write their own; we maintain the two flagship ones.)
- Upstream contributions to `GeyserMC/Geyser`. The soft-fork pattern
  is chosen explicitly to avoid coordinating with upstream maintainers.

## Architecture overview

```
┌─────────────────────────────────────────────────────────────────┐
│                       BUILD PIPELINE                             │
│                                                                  │
│  build/geyser.version (Renovate-tracked upstream Geyser SHA)    │
│  build/graalvm.version (Renovate-tracked GraalVM image digest)  │
│  build/agent-config/   (committed reflection metadata)          │
│  build/overlay/        (additive Gradle subproject w/ JNI ABI)  │
│  build/patches/        (surgical .patch files for upstream)     │
│           ↓ CI: native-image.yml                                 │
│                                                                  │
│  ┌──────────────────────┐    ┌──────────────────────┐           │
│  │   geyserlite ELF     │    │  libgeyserlite.so    │           │
│  │  (executable mode,   │    │  (shared library     │           │
│  │   for standalone)    │    │   with @CEntryPoint) │           │
│  └──────────────────────┘    └──────────────────────┘           │
│           ↓                            ↓                         │
│   GH Release / OCI image       go/   rust/                      │
│           ↓                     ↓      ↓                         │
│   docker run / ./geyserlite    purego  libloading              │
│                                 ↓      ↓                         │
│                            in-process embedded                   │
│                                 ↓                                │
│                         Gate adapter                             │
│                                 ↓                                │
│                              moxy                                │
└─────────────────────────────────────────────────────────────────┘
```

### How the libraries invoke Geyser

**Default mode is in-process.** Both the Go and Rust libraries load
`libgeyserlite.so` via `dlopen` (Go: `purego`, Rust: `libloading`)
and call C-ABI entry points exported by GraalVM's `@CEntryPoint`
mechanism. Same address space as the host; ~50 ns per call overhead
on lifecycle methods; zero overhead in the hot path because Geyser's
internal Netty event loops never cross the boundary.

**Subprocess mode is an opt-in fallback** for users who want OS-level
crash isolation (a Geyser segfault won't take down the host). Same API
shape; different mode flag.

### What the build produces

| Artifact | Why it exists |
|---|---|
| `geyserlite` ELF (executable) | Drop-in for `Geyser-Standalone.jar`; usable without any wrapper. Used by Docker users and the subprocess-mode fallback. |
| `libgeyserlite.so` (shared library) | The headline product: linked into Go/Rust hosts at runtime via `dlopen`. Same Geyser code as the ELF, different `native-image` invocation (`--shared` instead of executable). |
| `libgeyserlite.h` (C header) | Auto-generated by GraalVM. Lets anyone — not just Go/Rust — bind to the C ABI from any language with FFI. |

We never ship Level 1 (small upstream PRs to make subprocess management
nicer); we go straight to Level 2 (in-process via shared library) from
day 1.

## Distribution surface

| Artifact | Format | Acquisition | Audience |
|---|---|---|---|
| Native binary | static ELF (linux/amd64, linux/arm64) | GitHub Release, sha256 + cosign | self-hosters, package builders |
| Shared library | `.so` (linux/amd64, linux/arm64) + `.h` header | GitHub Release | language bindings (Go, Rust, anyone) |
| OCI image | `ghcr.io/minekube/geyserlite:<tag>` | `docker pull` | Docker users |
| Go module | `go.minekube.com/geyserlite/go` | `go get` | Go MC proxy authors |
| Rust crate | `geyserlite` | `cargo add geyserlite` | Rust MC server/proxy authors |
| Compose example | `deploy/compose/docker-compose.yml` | `git clone && docker compose up` | local dev / VPS users |
| Gate adapter | `go.minekube.com/geyserlite/go/integration/gate` | `go get` | Gate operators |

Deliberately not shipped: Helm charts, Fly templates, Kubernetes operators,
APT/RPM packages, language bindings beyond Go/Rust. Out of scope; users wrap
the OCI image themselves or write their own bindings against the C ABI.

## Phases

Phases are roughly sequential. Within a phase, Go and Rust work happens in
parallel — neither blocks the other. The native build (v0.1) gates
everything else.

### v0.1.0 — Native binary + shared library build pipeline

The foundation. **Both** the standalone ELF and the shared library are
built from day 1 — no separate "subprocess first, embed later" phase.

- [ ] Port `build/Dockerfile` from `minekube/moxy/geyser-bedrock/`
- [ ] Pin `build/geyser.version` to upstream Geyser commit SHA
- [ ] Pin `build/graalvm.version` to GraalVM Docker image digest
- [ ] Commit `build/agent-config/` (reflection metadata captured from a real login)
- [ ] `build/overlay/geyserlite-native/` — additive Gradle subproject
  - `build.gradle.kts` declaring `api(project(":standalone"))`
  - `src/main/java/com/minekube/geyserlite/bridge/GeyserBridge.java` with `@CEntryPoint` exports for `geyser_init / geyser_run / geyser_shutdown / geyser_status / geyser_create_isolate / geyser_tear_down_isolate`
- [ ] `build/patches/0001-register-subproject.patch` — single-line addition to upstream `settings.gradle.kts`
- [ ] `build/apply-overlay.sh` — clones Geyser at pinned ref, copies overlay, applies patches via `git apply --3way`
- [ ] CI workflow `.github/workflows/native-image.yml`:
  - Run `apply-overlay.sh`
  - Build `geyserlite` ELF (executable mode)
  - Build `libgeyserlite.so` (`--shared` mode)
  - Capture `libgeyserlite.h` (auto-generated by GraalVM)
  - Build OCI image from the ELF
- [ ] CI publishes:
  - `ghcr.io/minekube/geyserlite:<short-sha>` and `:latest`
  - GitHub Release assets per arch:
    - `geyserlite-linux-amd64`
    - `geyserlite-linux-arm64`
    - `libgeyserlite-linux-amd64.so`
    - `libgeyserlite-linux-arm64.so`
    - `libgeyserlite.h`
    - `checksums.txt` (sha256)
- [ ] Auto-sync from upstream:
  - Renovate watches GeyserMC/Geyser master; opens PR bumping `geyser.version`
  - CI re-applies overlay + patches; conflicts surface as failed PR
  - Mergify auto-merges clean Renovate PRs after smoke tests pass
  - Slack/Discord webhook on conflict
- [ ] Reproducibility: same SHAs in `geyser.version` + `graalvm.version` produce byte-identical artifacts (modulo timestamps in metadata)
- [ ] **Acceptance**:
  - `docker run ghcr.io/minekube/geyserlite` boots, listens on UDP 19132, responds to RakNet ping with the configured MOTD
  - A minimal C program that `dlopen`s `libgeyserlite.so` and calls `geyser_init`/`geyser_run` produces equivalent behavior

### v0.2.0 — Go library (in-process via purego, default)

Wrap the shared library as the primary embedding form. Subprocess mode
exists as an opt-in fallback.

- [ ] `go/` module: `go.minekube.com/geyserlite/go`
- [ ] Public API: `geyserlite.New(Options) (*Server, error)` and `*Server.Start/Stop/Healthy/Wait`
- [ ] Functional options for `Listen`, `Upstream`, `AuthType`, `FloodgateKey`, `MOTD`, `JVMArgs`, `Logger`, `Mode`
- [ ] **In-process mode (default)**:
  - `purego.Dlopen("libgeyserlite.so")` + `purego.RegisterLibFunc` on each `geyser_*` symbol
  - Substrate isolate lifecycle: `geyser_create_isolate` → `geyser_init` → `geyser_run` (in a goroutine) → `geyser_shutdown` → `geyser_tear_down_isolate`
  - CGO_ENABLED=0 builds work end-to-end
- [ ] Subprocess mode (opt-in via `Options.Mode = ModeSubprocess`):
  - `exec.CommandContext` with proper signal forwarding
  - Restart-on-crash with exponential backoff
  - Graceful shutdown: SIGTERM → wait → SIGKILL after timeout
  - Stdout/stderr forwarded to user's `slog.Logger`
- [ ] Shared-library auto-location strategy (in-process mode):
  1. `Options.LibraryPath` if set
  2. `$GEYSERLITE_LIBRARY` env var
  3. embedded copy if built with `-tags geyserlite_embed` (default in v0.4)
  4. system search paths (`/usr/lib`, `LD_LIBRARY_PATH`)
  5. error
- [ ] `geyserlite.GenerateFloodgateKey()` — 16 raw bytes (the **correct** Floodgate key format; upstream's docs example with `openssl genpkey -algorithm RSA` is wrong)
- [ ] `geyserlite.FlyGlobalServices()` — helper for binding Fly.io's `fly-global-services` UDP NAT address
- [ ] Crash boundary documented honestly: native segfault kills the host process; `recover()` does not save you. Subprocess mode is the answer for crash isolation.
- [ ] Examples: `go/examples/basic/` (in-process, 5 lines) and `go/examples/subprocess/` (fallback)
- [ ] **Acceptance**: 5-line Go program connects from a real Bedrock client through to a Paper backend; idle RSS within 5% of the standalone ELF baseline

### v0.3.0 — Rust crate (in-process via libloading, default)

Same shape as v0.2, but in Rust. Targets the Rust MC ecosystem
([valence-rs](https://github.com/valence-rs/valence),
[ferrumc](https://github.com/ferrumc-rs/ferrumc), homebrew proxies).

- [ ] `rust/` crate: `geyserlite` on crates.io
- [ ] Public API: `Server::new(Options) -> Result<Server>`; async `start/stop/wait` via `tokio`
- [ ] `Options` struct with `Default` impl matching Go's defaults
- [ ] **In-process mode (default)**:
  - `libloading::Library::new("libgeyserlite.so")` + typed function pointers
  - `bindgen`-generated `extern "C"` declarations from the published `libgeyserlite.h`
  - Substrate isolate lifecycle managed from Rust
  - No `cc` crate dependency — pure stable Rust
- [ ] Subprocess mode (opt-in via `Options::mode = Mode::Subprocess`):
  - `tokio::process::Command`
  - Signal forwarding (`SIGTERM` on `Drop` or explicit `stop()`)
  - Restart-on-crash with exponential backoff
  - stdout/stderr piped to `tracing` subscriber
- [ ] Shared-library auto-location strategy mirrors Go
- [ ] `geyserlite::generate_floodgate_key()` — 16 random bytes (`rand` crate)
- [ ] `geyserlite::fly_global_services()` — Fly.io NAT helper
- [ ] Examples: `rust/examples/basic.rs` (in-process) and `rust/examples/subprocess.rs`
- [ ] MSRV: stable Rust 1.85+ / Edition 2024 (no nightly features)
- [ ] **Acceptance**: 5-line Rust program connects from a real Bedrock client through to a Paper backend; idle RSS within 5% of the standalone ELF baseline

### v0.4.0 — Embed build mode (Go + Rust)

Single-binary distribution UX for both languages — embed the shared
library so the host program needs no external files.

**Go side:**

- [ ] Build tag `geyserlite_embed` activates per-arch embeds
- [ ] `go/embed/linux_amd64.go`, `go/embed/linux_arm64.go` — `//go:embed assets/...`
- [ ] Embedded `.so` blob is extracted to `os.UserCacheDir/geyserlite/<sha>/libgeyserlite.so` on first start (skip if already present + sha matches)
- [ ] `make embed-blobs` script downloads the right release `.so` files into `assets/` (binaries aren't committed)

**Rust side:**

- [ ] Cargo feature `embed` activates `include_bytes!` of the shared library
- [ ] Per-target embeds via `target_arch` cfg gates
- [ ] Same self-extract behavior on first start as Go

- [ ] **Acceptance**: `go build -tags geyserlite_embed` and `cargo build --features embed` each produce a single binary that runs Geyser in-process without external files

### v0.5.0 — Auto-download mode (Go + Rust)

For users who don't want to embed and don't want to pre-place the `.so`.

- [ ] HTTP fetch of the shared library matching the library version
- [ ] sha256 checksum verification against an embedded manifest
- [ ] Cache in OS-appropriate dir (`os.UserCacheDir` / `dirs::cache_dir`)
- [ ] `Mirror` option for self-hosters
- [ ] `Offline` option disables download
- [ ] Optional cosign signature verification when present
- [ ] **Acceptance** (both langs): fresh run on a machine with no cache fetches the `.so`, runs in-process, second run uses cache

### v0.6.0 — Gate adapter (first prod consumer)

Validates the Go in-process API by replacing moxy's hand-rolled supervisor.

- [ ] `go/integration/gate/` package — adapts `geyserlite.Server` to Gate's bedrock interface
- [ ] PR to `minekube/gate`: add `bedrock.mode: embedded` (in-process via geyserlite)
- [ ] Cut Gate `v0.65.0` with the new mode opt-in
- [ ] PR to `minekube/moxy`:
  - Remove `cmd/floodgate.go`, `geyser-bedrock/entrypoint.sh`, the entire `geyser-bedrock/` subtree
  - Switch `connect-proxy.yaml` to `bedrock.mode: embedded`
  - Switch `Dockerfile` back to plain distroless target
- [ ] Validate fra deploy: in-process mode under one process, same memory profile as today's two-process setup, no Java traffic blip
- [ ] **Acceptance**: 9-region rollout matches today's behavior; moxy's repo is meaningfully simpler

(No Rust adapter in this phase — there's no flagship Rust proxy in
the Minekube portfolio. Rust users integrate directly via the crate.)

### v1.0.0 — Stable

API freeze. Production stamp.

- [ ] API freeze: semver guarantee on the public surface of both libraries
- [ ] cosign-signed binary + container image + shared library
- [ ] SBOM (`syft`) attached to each release
- [ ] SLSA Level 3 build provenance attestations
- [ ] Documentation site (`geyserlite.minekube.com` from `docs/`, mkdocs-material)
- [ ] Migration guide from Gate's legacy `bedrock.mode: managed` to `embedded`
- [ ] Memory regression test gate in CI (Go and Rust both run it): PRs that push idle RSS above 130 MB or peak above 180 MB fail
- [ ] Synthetic Bedrock client harness in CI (RakNet ping, login attempt, MOTD parse) — no real Bedrock client required
- [ ] Both crates.io and Go module proxy publish the same version simultaneously per release
- [ ] **Acceptance**: 90 days of stable usage in production moxy without an emergency revert

## Soft-fork & sync strategy

geyserlite **does not contribute changes upstream**. We track Geyser
master and re-apply our additions on every bump, automated via CI.
This is set up in v0.1 — not deferred.

### Patch surface area, ranked

1. **Pure additions in `build/overlay/`** — entirely new files written into
   the Geyser source tree before build. No upstream conflict possible.
   `GeyserBridge.java` lives here. Most of our changes are this kind.

2. **Single-line patches to `settings.gradle.kts`** — registering our
   overlay subproject. Geyser rarely touches this file's `include(...)`
   block, so 3-way `git apply` succeeds across hundreds of upstream commits.

3. **Surgical `.patch` files in `build/patches/`** — for cases where the
   overlay can't avoid a real source modification (e.g., Geyser's
   `System.exit(0)` shutdown path doesn't compose with in-process
   embedding because `System.exit` would tear down the entire host).
   Each patch is small and numbered.

### Auto-sync flow

```
Renovate (daily)
  ↓ polls GeyserMC/Geyser master
  ↓ opens PR: "chore: bump geyser.version to <new-sha>"
CI runs build/apply-overlay.sh:
  1. git clone Geyser at <new-sha>
  2. cp -r overlay/* into Geyser tree
  3. for p in patches/*.patch: git apply --3way "$p"
  4. ./gradlew :geyserlite-native:nativeImage           # ELF
  5. ./gradlew :geyserlite-native:nativeImageShared     # .so
  6. Run synthetic Bedrock smoke test against both artifacts
Outcomes:
  ✅ Pass        → Mergify auto-merges PR; release pipeline cuts new tag
  ❌ Patch fail  → CI annotates which .patch failed; PR stays open for human
  ❌ Build fail  → API drift; human updates GeyserBridge.java in same PR
  ❌ Smoke fail  → Behavioral regression; investigate
```

### Expected ongoing maintenance

Realistic burden once stable:

- ~80% of upstream commits: clean auto-merge, zero touch
- ~15% of upstream commits: smoke test re-run or trivial fix
- ~5% of upstream commits: humans update a `.patch` or `GeyserBridge.java`
- Major Bedrock protocol bumps (quarterly-ish): hours of work, including
  re-capturing `agent-config/` from a fresh login session

Worst case budget: half a day per month, lumpy.

## Memory budget (asserted in CI from v1.0)

Targets the regression test gate enforces:

| Scenario | Target | Hard fail |
|---|---|---|
| Native binary idle (no players) | ≤ 115 MB RSS | > 130 MB |
| Native binary, 1 player loaded in | ≤ 145 MB RSS | > 165 MB |
| Native binary, peak HWM under play | ≤ 175 MB | > 200 MB |
| Native binary on disk | ≤ 115 MB | > 140 MB |
| Boot to "Done" | ≤ 2.0 s | > 3.0 s |

In-process mode is expected to come in within 5% of these numbers; the
shared-library overhead is mostly the same code as the executable plus
GraalVM's isolate management.

Baselines from production fra (May 2026, before v0.1):

```
Geyser-Native           : 77 MB RSS / 118 MB HWM under load
moxy (Gate)             : 36 MB RSS /  45 MB HWM
init / hallpass / shells: ~25 MB
total                   : ~138 MB / 207 MB usable on a 256 MB Fly VM
```

## CI/CD overview

| Workflow | Trigger | What it does |
|---|---|---|
| `ci.yml` | every push/PR | `mise install` → `task setup lint test`. Same exact commands run locally. |
| `native-image.yml` | changes to `build/**` or daily Renovate PR | GraalVM build (Dockerfile target `image` + `shared`); multi-arch manifest; smoke probe |
| `release.yml` | tag push | Pull native artifacts from native-image run + cosign-sign + GH Release + `cargo publish` |

CI uses [`mise`](https://mise.jdx.dev) to install pinned tooling (same as
local dev) and [`task`](https://taskfile.dev) as the entry point for lint
+ test (`task lint`, `task test`). Heavy paths (Docker buildx for the
GraalVM build) drop down to the docker actions for native GHA cache
integration; everything else routes through Taskfile so local and CI
can't diverge.

Caching:

- mise tools (Go, Rust, Java, task, linters): `jdx/mise-action@v2 with: cache: true`
- Go module + build cache, Cargo target: `actions/cache@v4` keyed on `go.sum` + `Cargo.lock` + `mise.toml`
- Docker buildx layers: `cache-from: type=gha` / `cache-to: type=gha,mode=max`, scoped per arch + per build target

## Testing strategy

- **Unit (each language)**: pure logic in supervisor, options validation, config rendering, Floodgate key encoding.
- **Integration in-process (each language)**: load `libgeyserlite.so`, exercise lifecycle, verify clean shutdown.
- **Integration subprocess (each language)**: launch the ELF against a fake Java upstream (TCP listener accepting Floodgate-formatted handshakes), assert lifecycle.
- **Synthetic Bedrock client**: tiny Go-side RakNet implementation that sends Unconnected Ping + minimal Login. Lives in `internal/synthetic/`. Used by both Go and Rust CI via subprocess invocation. No Mojang or real Bedrock client involvement.
- **Memory regression**: CI runs the binary, samples RSS at 5-second intervals for 60 seconds, asserts thresholds. Run from the Go test suite for now; Rust shells out to the same harness.
- **Crash isolation test**: deliberately segfault inside Geyser; assert that in-process mode kills the host (intended) and subprocess mode auto-restarts.

## Decisions log

- **2026-05-02** Project name: `geyserlite`. Sibling to Rust's `geyserite` (separate effort).
- **2026-05-02** Two flagship languages: Go and Rust, peers, same release cycle.
- **2026-05-02** **In-process embedding is the default and is shipped from v0.1.** Subprocess is an opt-in fallback for crash isolation. We do not pretend Level 0 (subprocess-only) is the destination.
- **2026-05-02** No upstream contributions to GeyserMC/Geyser. Soft-fork via overlay + patches, auto-synced via Renovate, set up in v0.1.
- **2026-05-02** Embed (`//go:embed` / `include_bytes!`) is opt-in via build tag/feature; auto-download is the friendly default for first-time users.
- **2026-05-02** No Kubernetes / Helm / Fly templates in v1.0 scope. Compose example is included for local dev.
- **2026-05-02** PGO not in CI build (requires live load run); shipped binary uses non-PGO + `--strict-image-heap` + static musl. PGO recipe documented for manual rebuild.
- **2026-05-02** Floodgate key format: 16 raw bytes (AES-128). Upstream docs example using `openssl genpkey -algorithm RSA` is wrong; ship a fixed `floodgate-keygen.sh`.
- **2026-05-02** Architecture support v1.0: linux/amd64, linux/arm64. macOS amd64/arm64 dev convenience only.
- **2026-05-02** Single Geyser instance per process. Multi-instance deferred indefinitely.
- **2026-05-02** Memory budget targets locked above; CI enforces from v1.0.
- **2026-05-02** Repository layout: polyglot subdirs (`go/`, `rust/`); shared `build/` and `docs/` at root.

## Open questions

- **Architecture matrix**: do we want a tier-2 `linux/arm64-musl` distinct from `linux/arm64-glibc`? Probably not — static-link makes it irrelevant.
- **Mirror strategy**: do we host a CDN mirror for binary downloads, or rely on GitHub Release infra? Start with GH only.
- **Floodgate key rotation**: hot reload, or kill-and-restart on key change? Start with restart; revisit if anyone asks.
- **macOS support**: dev-only Cmd-line binaries, or full integration tests? Start dev-only.
- **Multiple Geyser instances per process**: deferred. Will revisit if a multi-tenant SaaS use case shows up.
- **Public web demo**: should `geyserlite.minekube.com` host a live cross-play test server? Maybe v1.1.
- **Rust async runtime**: `tokio` is the default; do we expose `async-std` / `smol` compat too? Start tokio-only; abstract trait if demand surfaces.
- **Rust feature flag granularity**: how much do we split (`embed`, `download`, `tokio`, etc.)? Start minimal — keep features from leaking through the public API.
- **C ABI versioning**: how do we handle backwards-compat when the `@CEntryPoint` signatures need to change? Likely a major version bump, but worth thinking through before v1.0.

## Memory of how we got here

The optimization recipe encoded in this repo is the result of a multi-day
investigation in early May 2026 against `connect-proxy` on Fly.io:

| Stage | Idle RSS | Peak HWM | Notes |
|---|---|---|---|
| JVM Geyser warmed | 442 MB | 452 MB | doesn't fit 256 MB |
| First native build (`-O0`) | 222 MB | — | works, fits, slow |
| `-O2 -march=x86-64-v3` + Netty tuning | 175 MB | 195 MB | Netty's `maxDirectMemory` cap was the biggest single win |
| + PGO from a 4-min play session | 163 MB | 195 MB | -22 MB idle, -30 MB binary |
| + `-Xmx64m` + GC tuning | 127 MB | — | tighter heap + `MaxHeapFree=4m` |
| + `--strict-image-heap` + static musl + 1-core sim | 111 MB | 162 MB | full Hetzner sim |
| **In production (real fra Fly machine)** | **77 MB** | **118 MB** | with co-located Gate (`+36 MB`) for ~138 MB total |

Each step is one of the lines in `build/flags.sh`. The point of this
roadmap is to make sure none of that hard-won knowledge gets lost.

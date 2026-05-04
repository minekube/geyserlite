<!-- markdownlint-disable MD041 -->

<div align="center">

# geyserlite

**GeyserMC's Bedrock-Java translation, compiled to a small native binary.**
**Embeddable in Go and Rust. Drops into Java MC proxies that have no native bedrock support.**

[![Release](https://img.shields.io/github/v/release/minekube/geyserlite?display_name=tag&color=brightgreen)](https://github.com/minekube/geyserlite/releases/latest)
[![Crates.io](https://img.shields.io/crates/v/geyserlite.svg?color=ff7043)](https://crates.io/crates/geyserlite)
[![Go Reference](https://pkg.go.dev/badge/go.minekube.com/geyserlite.svg)](https://pkg.go.dev/go.minekube.com/geyserlite)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Discord](https://img.shields.io/discord/633708750032863232.svg?color=%237289da&label=discord)](https://minekube.com/discord)

</div>

JVM Geyser idles around **440 MB RSS**. That's bigger than a `shared-cpu-1x`
Fly machine (256 MB), so adding Bedrock cross-play to a Java proxy
historically meant running it as its own VM — extra cost, extra IP, extra
surface. `geyserlite` ships Geyser standalone compiled with **GraalVM
`native-image`** (musl-static on amd64, glibc-dynamic on arm64), tuned
flags, and a baked 192 MB heap, so the same workload fits in **~110 MB
idle / ~175 MB peak** alongside a Go or Rust proxy on the same machine.

Two flagship language libraries (Go and Rust, peers, same release cycle)
load `libgeyserlite.so` in-process via `dlopen` and hand back a normal
`Server` handle.

## What ships today

| Artifact | Where | Status |
|---|---|---|
| Native ELF, Bedrock listener | `geyserlite-linux-amd64` / `-arm64` on the [latest GitHub Release](https://github.com/minekube/geyserlite/releases/latest) | ✅ cosign-signed + SBOM-attested |
| Container image | `ghcr.io/minekube/geyserlite:latest` (multi-arch) | ✅ smoke-tested per build (RakNet ping → MOTD round-trip) |
| Shared library | `libgeyserlite-linux-{amd64,arm64}.so` + header on the same release | ✅ shipped; `@CEntryPoint` exports being wired up so Go/Rust can call `geyser_init` directly |
| Go module | `go get go.minekube.com/geyserlite` | ✅ published via vanity-URL Worker proxy |
| Rust crate | `cargo add geyserlite` | ✅ published via crates.io OIDC Trusted Publishing |
| Gate adapter | `go.minekube.com/geyserlite/integration/gate` | ✅ ready; Gate-side PR pending |

Verify a download:

```sh
curl -fsSL https://github.com/minekube/geyserlite/releases/latest/download/checksums.txt \
  | sha256sum -c --ignore-missing
```

## Quick starts

### Run the binary directly

```sh
curl -fsSL -o geyserlite \
  https://github.com/minekube/geyserlite/releases/latest/download/geyserlite-linux-amd64
chmod +x geyserlite
./geyserlite        # reads ./config.yml
```

### Docker

```sh
docker run --rm -p 19132:19132/udp \
  -v "$PWD/config.yml:/config.yml" \
  ghcr.io/minekube/geyserlite:latest
```

### Go

```go
import "go.minekube.com/geyserlite"

key, _ := geyserlite.GenerateFloodgateKey()
srv, _ := geyserlite.New(geyserlite.Options{
    Listen:       ":19132",
    Upstream:     "127.0.0.1:25567",
    AuthType:     geyserlite.Floodgate,
    FloodgateKey: key,
})
log.Fatal(srv.Start(ctx))
```

Single-binary distribution: `go build -tags geyserlite_embed` bundles the
matching `.so` with self-extract on first run.
[Full Go README →](./go/README.md)

### Rust

```rust,no_run
use geyserlite::{Server, Options, AuthType};

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    Server::new(Options {
        listen: ":19132".into(),
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

`cargo build --features embed` for single-binary distribution; `--features
download` for runtime auto-fetch with checksum verify.
[Full Rust README →](./rust/README.md)

## Architecture

```text
Bedrock client ─UDP 19132─▶ libgeyserlite.so (loaded in-process)
                                │
                                │ Floodgate AES-128 over loopback or TCP
                                ▼
                        Java MC proxy / server
```

The Go and Rust libraries `dlopen` the shared library and call C-ABI
entry points exported by GraalVM's `@CEntryPoint` mechanism. Same
address space as the host; lifecycle is `geyser_create_isolate →
geyser_init → geyser_run → geyser_shutdown`. Subprocess mode is an
opt-in fallback for OS-level crash isolation.

[`docs/architecture.md`](./docs/architecture.md) has the full breakdown.

## Self-sustaining release loop

This project is set up to ship updates without a human in the loop —
end to end, no merge buttons to click. The only time a human shows up
is when CI gates surface a real conflict.

```text
GeyserMC/Geyser commit
  ↓ (Renovate, daily)
"fix(deps): bump Geyser to <sha>" PR
  ↓ CI re-applies the soft-fork overlay; 8 jobs gate
  │  • build amd64 + arm64           • smoke (docker container)
  │  • header export assertion       • multi-arch manifest
  │  • go-integration (embedded)     • rust-integration (embedded)
  │  • go-subprocess-integration     • rust-subprocess-integration
  ↓ auto-merges on green (Renovate + GitHub platform-automerge)
main
  ↓ (release-please, on every push)
"chore(main): release 0.1.x" PR
  │   bumps rust/Cargo.toml + go/version.go + rust/src/version.rs +
  │   .release-please-manifest.json + writes CHANGELOG.md
  ↓ release-please enables --auto on the PR; main branch protection
  │  holds the merge until the `lint-test` check is green
v0.1.x tag (pushed by release-please) + GitHub Release row
  ↓ (release-please's trigger-release job dispatches release.yml)
release.yml
  ├─ pulls signed artifacts from the latest native-image run
  ├─ cosign sign-blob + attest each asset
  ├─ uploads to GitHub Release
  └─ cargo publish via crates.io OIDC Trusted Publishing
```

Conventional Commit prefixes drive what release-please decides:

| Prefix | Effect |
|---|---|
| `fix:` / `fix(...):` | patch release (0.1.2 → 0.1.3) |
| `feat:` / `feat(...):` | minor release (0.1.2 → 0.2.0) |
| `feat!:` (or `BREAKING CHANGE:`) | major release |
| anything else (`chore:`, `docs:`, `build:`, `ci:`, `refactor:`, `test:`) | no release |

Renovate is configured to tag Geyser bumps as `fix(deps):` so each
clean upstream sync trips a patch release end-to-end.

### Manual release knobs

The loop runs without a human on the happy path. These knobs exist
for the unhappy ones:

- **Pause auto-merge**: disable `Allow auto-merge` in repo Settings or
  remove `lint-test` from `main`'s branch protection — the PR stays
  open until you re-enable.
- **Skip a release**: close the release-please PR; the next push to
  `main` reopens it with the accumulated commits absorbed.
- **Force a release without a `fix:`/`feat:` commit**: add an empty
  commit with `git commit --allow-empty -m "fix: <reason>"`.
- **Force a major bump** before 1.0: amend the release-please PR title
  to start with `feat!:` or include `BREAKING CHANGE:` in the body.
- **Re-fire `release.yml` for an existing tag**: `gh workflow run
  release.yml --ref v0.1.x` (the trigger-release job uses the same
  workflow_dispatch surface).

### Trust signals

| Signal | How |
|---|---|
| Build pipeline integrity | All artifacts cosign-signed with sigstore keyless OIDC; SBOM-style SPDX attestations alongside the `.sig` files |
| Release authenticity | crates.io Trusted Publishing (OIDC) — no long-lived token to rotate or leak |
| Reproducible inputs | `build/geyser.version` (commit SHA) + `build/graalvm.version` (image digest) pin the build to byte-identical inputs |
| Verifiable downloads | `checksums.txt` on every GH Release; `sha256sum -c` works directly |

## Why a soft fork, not a hard fork?

We don't maintain a long-lived divergent branch of Geyser. The build
pipeline clones upstream at a pinned commit, lays an additive Gradle
subproject (`build/overlay/geyserlite-native/`) on top, runs idempotent
mutations and (when needed) `.patch` files against the tree, then
invokes `native-image`. Renovate watches `GeyserMC/Geyser master` and
opens PRs that re-run the whole pipeline against the new ref — clean
ones auto-merge, conflicts surface as failed CI. **There's no fork
checked out anywhere; the upstream tree is reconstituted from scratch
every build.**

Within that pattern, the choice between "context-based `.patch` file"
and "idempotent script mutation" is a per-edit call. A `.patch` is
right when the change has multi-line context that's stable across
upstream edits. An idempotent mutation is right when the change is
"ensure this one line exists somewhere in this file" and the
surrounding context churns. Our only edit so far —
`include(":geyserlite-native")` in `settings.gradle.kts` — fits the
second case: the patch broke on every upstream edit to that file, so
`apply-overlay.sh` now appends the line if it isn't already present.
The `.patch` machinery (`build/patches/*.patch`, picked up by
`apply-overlay.sh`) stays available for changes that genuinely need it.

## Local development

```sh
mise trust && mise install     # Go, Rust, GraalVM, task, linters — all pinned
task                            # list available tasks
task build:native               # GraalVM build via Docker (~3 min on a real CPU)
task test                       # all language tests
task lint                       # all linters (yaml, markdown, go, rust)
```

`task` itself is installed by `mise`; the only host prerequisite is
`mise` and Docker (for the native build).

## Repository layout

```text
geyserlite/
├── README.md
├── ROADMAP.md            ← phases, decisions log, memory budgets
├── go/                   ← Go module: go.minekube.com/geyserlite
│   ├── integration/gate/ ← Gate-shaped lifecycle adapter
│   └── examples/         ← basic, floodgate, fly, healthcheck, custom-config
├── rust/                 ← Rust crate: geyserlite (crates.io)
│   └── examples/         ← basic, floodgate, fly, healthcheck, custom_config
├── build/                ← native-image pipeline + soft-fork overlay/patches
├── docs/                 ← architecture, floodgate, troubleshooting
└── .github/workflows/    ← ci.yml, native-image.yml, release.yml
```

The Go module's import path is `go.minekube.com/geyserlite`; the source
lives in `go/`. The vanity-URL host (`go.minekube.com`) is a Cloudflare
Worker that runs the Go module proxy protocol against this repo's `go/`
subdirectory — see [`minekube/go-vanity`](https://github.com/minekube/go-vanity).

## Status & roadmap

`v0.1.1` is shipped: build pipeline, multi-arch ELF + shared lib + OCI
image, Go module + Rust crate, embed/auto-download paths in code,
Cloudflare Worker module proxy for the Go vanity URL. The piece still in
flight is `@CEntryPoint` reachability — the shared library currently
exports only the GraalVM runtime symbols; once `GeyserBridge`'s entry
points are reachable from analysis, `libgeyserlite.h` declares the real
`geyser_*` ABI and the in-process Go/Rust path is end-to-end usable.

[ROADMAP.md](./ROADMAP.md) tracks the rest: Gate integration PR, moxy
migration, memory regression CI gate, mkdocs docs site.

## Related projects

- [GeyserMC/Geyser](https://github.com/GeyserMC/Geyser) — upstream Bedrock-Java translator (Java)
- [`minekube/gate`](https://github.com/minekube/gate) — Go MC proxy; first production consumer of `geyserlite/integration/gate`
- [`minekube/connect-java`](https://github.com/minekube/connect-java) — Connect plugin for backend MC servers
- [valence-rs/valence](https://github.com/valence-rs/valence) — Rust MC server framework (potential Rust consumer)

## License

MIT. Geyser itself is MIT — see upstream for protocol-mapping copyright.

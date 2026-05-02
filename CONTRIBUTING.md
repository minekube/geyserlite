# Contributing to geyserlite

Thanks for thinking about contributing! This doc is the short form.
The longer story (architecture, design rationale, optimization journey)
lives in [ROADMAP.md](./ROADMAP.md) and [docs/](./docs/).

## TL;DR

```sh
git clone <fork> && cd geyserlite
mise trust && mise install   # installs Go, Rust, GraalVM, task, linters
task setup
task lint test               # what CI runs — match it locally before pushing
```

If `task lint test` is green you're 95% of the way through code review.

## Repo layout

- `build/` — GraalVM native-image pipeline. Slow (~3 min). **Don't touch
  unless you know what you're doing.** See [build/README.md](./build/README.md)
  and the soft-fork section of the [ROADMAP](./ROADMAP.md).
- `go/` — Go module (`go.minekube.com/geyserlite`). Source lives in this
  subdir; the vanity-URL server maps the import path → subdir.
- `rust/` — Rust crate (`geyserlite` on crates.io).
- `docs/` — architecture + tuning + troubleshooting + Floodgate docs.
- `deploy/compose/` — runnable docker-compose example.
- `scripts/` — small shell utilities (`fetch-embed-assets.sh`, `floodgate-keygen.sh`).
- `mise.toml` + `Taskfile.yml` — pin every dev tool and define every workflow.

## What's worth a PR

In rough order of welcome:

1. **Bug fixes** — always.
2. **Test coverage** — especially for the runners (subprocess + embedded
   lifecycle, error paths, cancellation).
3. **Doc improvements** — typos, broken links, missing context, fresh
   memory benchmarks against newer GraalVM / newer Geyser.
4. **New build flag combinations** — if you find a flag that shaves more
   RSS without behavioral regression, document the measurement in
   [docs/tuning.md](./docs/tuning.md) and submit.
5. **New target architectures** — currently linux/amd64 + linux/arm64.
   macOS targets for dev would need a non-musl static-link approach.
6. **API changes** — happy to discuss, but raise an issue first; the
   public Go and Rust APIs are intended to track each other closely.

What's not in scope (per the roadmap):

- Kubernetes / Helm / Fly templates.
- Multi-instance Geyser per process.
- Windows targets.
- Language bindings other than Go and Rust (the C ABI is exported and
  documented; anyone can write more).

## Soft-fork: never edit upstream Geyser directly

Our build clones [GeyserMC/Geyser](https://github.com/GeyserMC/Geyser)
at the SHA in `build/geyser.version` and applies `build/overlay/` (pure
additions) plus `build/patches/` (numbered surgical patches) on top.

If your change needs Geyser-side modifications:

1. Run `task overlay:apply` to materialize the full source tree at
   `build/.work/Geyser/`.
2. Make your edit there.
3. `task patch:create -- 0NNN-short-description` regenerates a patch
   file in `build/patches/`.
4. Don't commit anything from `build/.work/` — it's generated.

Patches that touch existing upstream files are tax: every Renovate-driven
Geyser bump risks a merge conflict you'll resolve. Pure additions to
`build/overlay/` (new files) are free. Prefer overlay over patches when
there's a choice.

## Commit messages

We use conventional-commit-ish style:

```
<area>: <imperative summary>
```

Areas roughly map to top-level dirs:

- `go:` Go library
- `rust:` Rust crate
- `build:` GraalVM build pipeline
- `ci:` workflows
- `docs:` documentation
- `taskfile:` Taskfile.yml
- `mise:` mise.toml

Add `[skip ci]` to commits that don't need CI to run.

## CI

- `ci.yml` runs on every PR: `task setup lint test`. Cached via mise +
  GHA caches.
- `native-image.yml` runs only when `build/**` changes (path-filtered).
  This is the slow ~3 min GraalVM build.
- `release.yml` runs on tag push: signs, publishes Rust crate, creates
  GH Release.

If your PR touches `build/**` and the slow native-image job spotlights
a regression, pinning the previous Geyser SHA in `build/geyser.version`
gets the build green again — that's the right escape hatch while
upstream stabilizes.

## Memory budgets

The shipped binary is tuned for 256 MB Fly machines. We assert in CI
(post v1.0) that PRs don't push idle RSS above 130 MB or peak above
180 MB. If your change touches build flags or runtime defaults, run
the synthetic probe locally and include before/after measurements in
the PR description:

```sh
task build:native              # Docker GraalVM build
docker run --rm -p 19132:19132/udp ghcr.io/minekube/geyserlite:local-image &
sleep 5
task probe -- 127.0.0.1:19132   # human-readable
docker stats --no-stream        # snapshot RSS
```

## Code style

- **Go**: `go vet` + `golangci-lint` (config inherited from CI). No
  custom style guide — idiomatic Go is fine.
- **Rust**: `cargo fmt` + `cargo clippy --all-targets --all-features
  -- -D warnings`. We accept `clippy::pedantic` warnings; warnings from
  lints in the default `-D warnings` set must be fixed.
- **Markdown**: `markdownlint-cli2`.
- **YAML**: `yamllint` with our `.yamllint` (line length 200, comments
  loose).

`task lint` runs all four.

## License

By contributing, you agree your changes are released under the
[MIT License](./LICENSE).

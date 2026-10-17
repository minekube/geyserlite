---
name: release
description: Release geyserlite and carry the release through the Gate managed dependency bump chain to downstream consumers.
---

# Release

## Release chain

- Releases publish checksummed native artifacts, Go metadata, and Rust crate
  metadata. Gate consumes the release through its managed dependency update
  workflow after release assets exist.
- Keep release-chain changes explicit: GeyserLite release -> Gate managed
  dependency bump -> Gate release -> downstream consumers.

## Release automation changes

For release automation changes, verify workflow syntax and the release asset
ordering before merging.

## Native artifacts belong to a commit, not to the tag

`release.yml` publishes the ELF/.so/.exe that `native-image.yml` built for the
**last first-parent commit touching the native build inputs** (`build/**`,
`mise.toml`, `native-image.yml`) — not for the tag commit, because a release
commit only bumps metadata while a `go/**`-only fix does not move that commit.
Two consequences, both load-bearing:

- `native-image.yml`'s `upload-artifact` retention must outlive a realistic
  release delay (>= 30 days). At 7 days it did not: geyserlite v0.6.1
  (2026-10-17) found a green native run whose uploads had expired, published 0
  assets, and skipped crates.io plus both cascade dispatches while still
  looking "published".
- `.github/scripts/fetch-native-artifacts.sh` is the acquisition path. When the
  resolved run is `completed/success` but its uploads are gone, it re-runs that
  exact run (same run id, same head sha, which republishes the artifacts) and
  waits; a red native tree still fails the release closed, and it never
  substitutes another run, another SHA or an older artifact set. Run
  `go test -run TestNativeArtifacts ./go` after touching it — the test drives
  the real script against a stub `gh` for every branch, including the expired-
  artifact repair and the fail-closed paths.


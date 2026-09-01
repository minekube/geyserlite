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

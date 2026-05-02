#!/usr/bin/env bash
# Fetch native release assets for embed-mode builds (Go //go:embed,
# Rust include_bytes!). Pulls them into go/assets/ and rust/assets/ where
# the per-target embed code expects to find them.
#
# Usage:
#   ./scripts/fetch-embed-assets.sh                # latest release
#   ./scripts/fetch-embed-assets.sh v0.5           # specific tag
#
# Requires: gh, curl, sha256sum.

set -euo pipefail

REPO="minekube/geyserlite"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

TAG="${1:-latest}"
if [[ "$TAG" == "latest" ]]; then
  TAG=$(gh release view --repo "$REPO" --json tagName --jq .tagName)
fi
echo "▸ Fetching geyserlite assets for tag: $TAG"

DEST_GO="${ROOT}/go/assets"
DEST_RUST="${ROOT}/rust/assets"
mkdir -p "$DEST_GO" "$DEST_RUST"

ASSETS=(
  "geyserlite-linux-amd64"
  "geyserlite-linux-arm64"
  "libgeyserlite-linux-amd64.so"
  "libgeyserlite-linux-arm64.so"
  "checksums.txt"
)

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

for a in "${ASSETS[@]}"; do
  echo "  - $a"
  gh release download "$TAG" --repo "$REPO" --pattern "$a" --dir "$TMP" --clobber
done

# Verify checksums.
( cd "$TMP" && sha256sum -c checksums.txt --ignore-missing )

# Stage into both go/assets/ and rust/assets/.
for a in "${ASSETS[@]}"; do
  cp "$TMP/$a" "$DEST_GO/$a"
  cp "$TMP/$a" "$DEST_RUST/$a"
done

echo "✓ Embed assets staged for $TAG"

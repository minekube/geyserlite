#!/usr/bin/env bash
# Clones Geyser at the pinned ref, applies our overlay + patches.
# Run from repo root. Idempotent; cleans up on rerun.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK_DIR="${REPO_ROOT}/build/.work"
GEYSER_DIR="${WORK_DIR}/Geyser"
GEYSER_VERSION="$(tr -d '[:space:]' < "${REPO_ROOT}/build/geyser.version")"

echo "▸ Geyser ref: ${GEYSER_VERSION}"

# Fresh checkout each run — small price for guaranteed clean state.
rm -rf "${GEYSER_DIR}"
mkdir -p "${WORK_DIR}"

git clone --quiet https://github.com/GeyserMC/Geyser.git "${GEYSER_DIR}"
( cd "${GEYSER_DIR}"
  git checkout --quiet "${GEYSER_VERSION}"
  git submodule --quiet update --init --recursive --depth 1
)

echo "▸ Copying overlay/ into Geyser tree"
cp -R "${REPO_ROOT}/build/overlay/." "${GEYSER_DIR}/"

echo "▸ Applying patches/"
shopt -s nullglob
for p in "${REPO_ROOT}"/build/patches/*.patch; do
  echo "  - $(basename "${p}")"
  ( cd "${GEYSER_DIR}" && git apply --3way "${p}" )
done

echo "✓ Overlay + patches applied; tree at ${GEYSER_DIR}"

#!/usr/bin/env bash
# Clones Geyser at the pinned ref, applies our overlay + mutations.
# Run from repo root. Idempotent; cleans up on rerun.
#
# Why no .patch files: line-based patches break on every upstream
# settings.gradle.kts edit. We instead apply mutations by *intent* —
# "ensure the include line is present" — which survives drift.
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

echo "▸ Registering :geyserlite-native subproject"
SETTINGS="${GEYSER_DIR}/settings.gradle.kts"
INCLUDE_LINE='include(":geyserlite-native")'
if grep -qF "${INCLUDE_LINE}" "${SETTINGS}"; then
  echo "  already registered; skipping"
else
  printf '\n// geyserlite overlay (added by https://github.com/minekube/geyserlite)\n%s\n' "${INCLUDE_LINE}" >> "${SETTINGS}"
  echo "  appended to settings.gradle.kts"
fi

# Optional .patch files for anything that genuinely needs a contextual diff.
shopt -s nullglob
patches=( "${REPO_ROOT}"/build/patches/*.patch )
if [ ${#patches[@]} -gt 0 ]; then
  echo "▸ Applying patches/"
  for p in "${patches[@]}"; do
    echo "  - $(basename "${p}")"
    ( cd "${GEYSER_DIR}" && git apply --3way "${p}" )
  done
fi

echo "✓ Overlay applied; tree at ${GEYSER_DIR}"

#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# Fetch the native-image.yml artifacts a release publishes, republishing them
# from the same run when they have expired.
#
# Why this is a script and not an inline step: the failure it repairs is a
# state machine (discover the run for NATIVE_SHA, require completed/success,
# notice that the uploads are gone, re-run that exact run, wait for it, notice
# that the artifacts are back) and every branch of it has to stay fail-closed.
# go/release_native_artifacts_test.go drives this file with a stub `gh` and
# asserts each branch, so the behaviour is testable without a 40-minute CI
# round trip. Keep the logic here; release.yml only wires it up.
#
# The defect this fixes (geyserlite v0.6.1, 2026-10-17):
#   Release.yml publishes the ELF/.so/.exe built by native-image.yml for the
#   latest first-parent commit that touched the native build inputs, NOT the
#   tag commit. Those uploads carry `retention-days` (7 at the time), while a
#   release can be cut arbitrarily later: a go/**-only fix does not move
#   NATIVE_SHA, so a release delayed past the retention window reaches for
#   artifacts that no longer exist. The run is still `completed/success`,
#   `gh run download --pattern 'geyserlite-*'` matches nothing, and the release
#   publishes 0 assets — skipping crates.io and both cascade dispatches while
#   looking "published" (tag + release object exist). v0.6.1 sat in exactly
#   that state until a human re-ran the native run and then the release.
#
#   Re-running a run keeps its id and head_sha, so the republished artifacts
#   belong to the same commit NATIVE_SHA resolved to. This script never
#   substitutes a different run, a different SHA or an older artifact set: a
#   red native tree still fails the release closed, exactly as before.
#
# Environment:
#   GITHUB_REPOSITORY          required, owner/repo
#   NATIVE_SHA                 optional; resolved from git when unset
#   ARTIFACT_DIR               default "artifacts"
#   NATIVE_ARTIFACT_NAMES      default "geyserlite-amd64 geyserlite-arm64
#                              geyserlite-windows-amd64"
#   NATIVE_WORKFLOW            default "native-image.yml"
#   NATIVE_BRANCH              default "main"
#   NATIVE_POLL_SECONDS        default 10
#   NATIVE_DISCOVERY_ATTEMPTS  default 30   (~5 min) no run exists for the SHA
#   NATIVE_INFLIGHT_ATTEMPTS   default 240  (~40 min) run is queued/in progress
#   NATIVE_RERUN_ATTEMPTS      default 1    self-heal re-runs of that same run
set -euo pipefail

REPO="${GITHUB_REPOSITORY:?GITHUB_REPOSITORY must be set (owner/repo)}"
ARTIFACT_DIR="${ARTIFACT_DIR:-artifacts}"
NATIVE_WORKFLOW="${NATIVE_WORKFLOW:-native-image.yml}"
NATIVE_BRANCH="${NATIVE_BRANCH:-main}"
NATIVE_POLL_SECONDS="${NATIVE_POLL_SECONDS:-10}"
NATIVE_DISCOVERY_ATTEMPTS="${NATIVE_DISCOVERY_ATTEMPTS:-30}"
NATIVE_INFLIGHT_ATTEMPTS="${NATIVE_INFLIGHT_ATTEMPTS:-240}"
NATIVE_RERUN_ATTEMPTS="${NATIVE_RERUN_ATTEMPTS:-1}"
NATIVE_ARTIFACT_NAMES="${NATIVE_ARTIFACT_NAMES:-geyserlite-amd64 geyserlite-arm64 geyserlite-windows-amd64}"

# Native build inputs whose last first-parent change owns the artifacts. Every
# entry must also be in native-image.yml's `on.push.paths`, otherwise this can
# resolve to a commit that never ran the native build (pinned by the contract
# test).
NATIVE_SOURCE_PATHS=(build mise.toml .github/workflows/native-image.yml)

die() {
  echo "::error::$*" >&2
  exit 1
}

pause() {
  # Fractional/zero sleeps keep the behavioural tests instant.
  if [ "${NATIVE_POLL_SECONDS}" != "0" ]; then
    sleep "${NATIVE_POLL_SECONDS}"
  fi
}

# Names of the non-expired geyserlite-* uploads on a run. `minekube~…dockerbuild`
# build-summary blobs are excluded by name, and expired uploads are never
# returned by the API (which is how a 7-day retention window turns into an
# empty download).
release_artifacts() {
  gh api "/repos/${REPO}/actions/runs/$1/artifacts?per_page=100" |
    jq -r '.artifacts[] | select(.expired | not)
           | select(.name | startswith("geyserlite-")) | .name'
}

# Latest run of NATIVE_WORKFLOW on NATIVE_BRANCH whose head sha is NATIVE_SHA.
# {} when there is none yet.
discover_run() {
  gh api "/repos/${REPO}/actions/workflows/${NATIVE_WORKFLOW}/runs?branch=${NATIVE_BRANCH}&per_page=100" |
    jq -c --arg sha "${NATIVE_SHA}" '
      [.workflow_runs[] | select(.head_sha == $sha)]
      | sort_by(.created_at) | reverse | .[0] // {}'
}

# {id,status,conclusion,head_sha,html_url} for one run.
run_state() {
  gh api "/repos/${REPO}/actions/runs/$1" |
    jq -c '{id, status, conclusion, head_sha, html_url}'
}

# Names in NATIVE_ARTIFACT_NAMES that are absent from the given name list.
missing_artifacts() {
  local present=$1 name missing=""
  for name in ${NATIVE_ARTIFACT_NAMES}; do
    if ! printf '%s\n' "${present}" | grep -Fxq "${name}"; then
      missing="${missing} ${name}"
    fi
  done
  printf '%s' "${missing}"
}

if [ -z "${NATIVE_SHA:-}" ]; then
  NATIVE_SHA=$(git log --first-parent -n 1 --format=%H -- "${NATIVE_SOURCE_PATHS[@]}")
fi
if [ -z "${NATIVE_SHA}" ]; then
  die "Could not resolve native artifact source commit"
fi

mkdir -p "${ARTIFACT_DIR}"

# ── Discover the run for NATIVE_SHA ─────────────────────────────────────────
echo "Waiting for native-image.yml artifacts from ${NATIVE_SHA}"
RUN_ID=""
attempt=0
while :; do
  attempt=$((attempt + 1))
  RUN_JSON=$(discover_run)
  RUN_ID=$(jq -r '.id // ""' <<<"${RUN_JSON}")
  if [ -n "${RUN_ID}" ]; then
    break
  fi
  if [ "${attempt}" -ge "${NATIVE_DISCOVERY_ATTEMPTS}" ]; then
    die "native-image.yml has no run for ${NATIVE_SHA} after ${attempt} attempt(s) on ${NATIVE_BRANCH}." \
      " That commit is in release.yml's NATIVE_SHA resolution but not in native-image.yml's push paths," \
      " or the run was deleted; re-run native-image.yml for ${NATIVE_SHA} and dispatch Release again."
  fi
  echo "No native-image.yml run for ${NATIVE_SHA} yet; waiting..."
  pause
done

RUN_SHA=$(jq -r '.head_sha // ""' <<<"${RUN_JSON}")
if [ "${RUN_SHA}" != "${NATIVE_SHA}" ]; then
  die "Discovered run ${RUN_ID} has head sha '${RUN_SHA}', not ${NATIVE_SHA}; refusing to publish foreign artifacts."
fi

# ── Require that run to be completed/success (fail closed on a red tree) ────
await_completion() {
  local run_id=$1 attempt=0 state status conclusion url
  while :; do
    attempt=$((attempt + 1))
    state=$(run_state "${run_id}")
    status=$(jq -r '.status // ""' <<<"${state}")
    conclusion=$(jq -r '.conclusion // ""' <<<"${state}")
    url=$(jq -r '.html_url // ""' <<<"${state}")
    echo "native-image.yml run ${run_id} is ${status}/${conclusion}: ${url}"
    if [ "${status}" = "completed" ]; then
      if [ "${conclusion}" = "success" ]; then
        return 0
      fi
      die "native-image.yml run ${run_id} for ${NATIVE_SHA} concluded ${conclusion} (${url});" \
        " refusing to publish artifacts from a red native build."
    fi
    if [ "${attempt}" -ge "${NATIVE_INFLIGHT_ATTEMPTS}" ]; then
      die "Timed out waiting for native-image.yml run ${run_id} (${status}) for ${NATIVE_SHA}: ${url}"
    fi
    pause
  done
}

# Wait until a re-run of run_id has actually left `completed`. Without this the
# completion poll can observe the previous attempt's `completed/success` and
# race straight back into "artifacts still missing".
await_rerun_start() {
  local run_id=$1 attempt=0 status
  while :; do
    attempt=$((attempt + 1))
    status=$(run_state "${run_id}" | jq -r '.status // ""')
    if [ "${status}" != "completed" ]; then
      return 0
    fi
    if [ "${attempt}" -ge "${NATIVE_DISCOVERY_ATTEMPTS}" ]; then
      die "Re-run of native-image.yml run ${run_id} never started (still ${status});" \
        " re-run it manually: https://github.com/${REPO}/actions/runs/${run_id}"
    fi
    pause
  done
}

await_completion "${RUN_ID}"
RUN_URL=$(run_state "${RUN_ID}" | jq -r '.html_url // ""')

# ── Ensure that run still publishes the artifacts we need ───────────────────
# A completed/success run whose uploads expired reports them as gone, so the
# download below would silently match nothing. Re-run that exact run (same id,
# same head sha) to republish them; anything red still fails the release.
reruns=0
while :; do
  PRESENT=""
  MISSING=""
  # Artifact visibility lags a completed run slightly; give it a short grace
  # window before declaring the uploads expired.
  for grace in 1 2 3; do
    PRESENT=$(release_artifacts "${RUN_ID}")
    MISSING=$(missing_artifacts "${PRESENT}")
    if [ -z "${MISSING}" ]; then
      break
    fi
    if [ "${grace}" -lt 3 ]; then
      pause
    fi
  done
  if [ -z "${MISSING}" ]; then
    break
  fi

  if [ "${reruns}" -ge "${NATIVE_RERUN_ATTEMPTS}" ]; then
    die "native-image.yml run ${RUN_ID} (head ${NATIVE_SHA}) publishes no${MISSING} artifact(s)" \
      " — their retention window expired; re-run ${RUN_URL} and dispatch Release again."
  fi

  reruns=$((reruns + 1))
  echo "::warning::native-image.yml run ${RUN_ID} is completed/success but is missing${MISSING} for ${NATIVE_SHA};" \
    " re-running that exact run to republish them (artifacts expired, head sha unchanged)."
  if ! gh api --method POST "/repos/${REPO}/actions/runs/${RUN_ID}/rerun" >/dev/null; then
    die "Could not re-run native-image.yml run ${RUN_ID}; the release token needs permissions: actions: write."
  fi
  await_rerun_start "${RUN_ID}"
  await_completion "${RUN_ID}"
done

# ── Download the release uploads ────────────────────────────────────────────
# Only our own upload-artifact outputs; the docker build-summary blobs are not
# zips and `gh run download` chokes on them.
gh run download "${RUN_ID}" --dir "${ARTIFACT_DIR}" --pattern 'geyserlite-*'

for name in ${NATIVE_ARTIFACT_NAMES}; do
  if [ ! -d "${ARTIFACT_DIR}/${name}" ]; then
    die "Downloaded artifacts from run ${RUN_ID} do not contain ${name} (${RUN_URL});" \
      " the release would publish an incomplete asset set."
  fi
done

file_count=$(find "${ARTIFACT_DIR}" -type f | wc -l | tr -d ' ')
if [ "${file_count}" -eq 0 ]; then
  die "Downloaded no files from native-image.yml run ${RUN_ID} (${RUN_URL});" \
    " refusing to publish an empty release."
fi

echo "Collected ${file_count} file(s) from native-image.yml run ${RUN_ID} (head ${NATIVE_SHA}):"
ls -la "${ARTIFACT_DIR}"

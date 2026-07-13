#!/usr/bin/env bash
# Clones Geyser at the pinned ref, applies our overlay + mutations.
# Run from repo root. Idempotent; cleans up on rerun.
#
# Why settings registration is not a .patch: line-based patches break on every
# upstream settings.gradle.kts edit. We instead apply that mutation by *intent*
# — "ensure the include line is present" — which survives drift.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK_DIR="${REPO_ROOT}/build/.work"
GEYSER_DIR="${WORK_DIR}/Geyser"
GEYSER_VERSION="$(tr -d '[:space:]' < "${REPO_ROOT}/build/geyser.version")"
PYTHON_BIN="${PYTHON:-python3}"

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

# The .so build's gradle config points at ${rootProject.projectDir}/agent-config
# (i.e. <Geyser-root>/agent-config) for reflection metadata. The reflect-config
# patcher (run later by Dockerfile) reads from /tmp/agent-config; the
# committed source-of-truth lives at REPO_ROOT/build/agent-config. Stage a
# copy at the Geyser root so the gradle build sees it. Without this, the
# .so silently builds with NO reflection metadata — log4j2 plugin discovery
# implodes at runtime ("ServiceLoader.load(Class,ClassLoader)" /
# "NoSuchMethodException: <init>()").
echo "▸ Staging agent-config at Geyser root for the .so build"
cp -R "${REPO_ROOT}/build/agent-config" "${GEYSER_DIR}/agent-config"

echo "▸ Registering :geyserlite-native subproject"
SETTINGS="${GEYSER_DIR}/settings.gradle.kts"
INCLUDE_LINE='include(":geyserlite-native")'
if grep -qF "${INCLUDE_LINE}" "${SETTINGS}"; then
  echo "  already registered; skipping"
else
  printf '\n// geyserlite overlay (added by https://github.com/minekube/geyserlite)\n%s\n' "${INCLUDE_LINE}" >> "${SETTINGS}"
  echo "  appended to settings.gradle.kts"
fi

echo "▸ Patching GeyserStandaloneBootstrap for embedded use"
# When the bridge runs Geyser inside our shared library, three sites in
# the standalone bootstrap are show-stoppers:
#   - System.exit(1)/System.exit(0) terminate the *host* process
#   - geyserLogger.start() is the stdin command-prompt loop; it blocks
# Gate them behind the geyserlite.embedded system property (set by
# GeyserBridge.init before kicking off the lifecycle) so the standalone
# behavior is unchanged when run as an ELF.
# Also expose two private fields so the bridge can configure the
# bootstrap without reflection.
#
# The property name lives in GeyserBridge.java's EMBED_PROP constant.
# Anchor it here too so a rename in either place breaks loudly.
SBP="${GEYSER_DIR}/bootstrap/standalone/src/main/java/org/geysermc/geyser/platform/standalone/GeyserStandaloneBootstrap.java"
EMBED_PROP="geyserlite.embedded"
"${PYTHON_BIN}" - "$SBP" "$EMBED_PROP" <<'PY'
import sys, re
path, prop = sys.argv[1], sys.argv[2]
src = open(path).read()

count = 0
def once(pattern, repl):
    global src, count
    new, n = re.subn(pattern, repl, src, count=1)
    if n != 1:
        sys.stderr.write(f"apply-overlay: expected exactly one match for {pattern!r}, got {n}\n")
        sys.exit(2)
    src = new
    count += 1

# 1. config-load failure should bail (return) instead of terminating
#    the host process when embedded.
once(
    r'(\n\s+)System\.exit\(1\);',
    rf'\1if (Boolean.getBoolean("{prop}")) {{ return; }} else {{ System.exit(1); }}',
)
# 2. stdin command-prompt loop must not run in-process.
once(
    r'(\n\s+)geyserLogger\.start\(\);',
    rf'\1if (!Boolean.getBoolean("{prop}")) {{ geyserLogger.start(); }}',
)
# 3. shutdown's System.exit(0) likewise.
once(
    r'(\n\s+)System\.exit\(0\);',
    rf'\1if (!Boolean.getBoolean("{prop}")) {{ System.exit(0); }}',
)
# 4. Native images cannot define the hidden classes Log4j's StatusLogger
#    ServiceLoader error path tries to synthesize while looking for
#    WatchEventService implementations. That path is non-fatal but noisy,
#    so silence Log4j internal status logging before GeyserStandaloneLogger
#    triggers LogManager initialization. Normal Geyser logs still flow.
once(
    r'(\n\s+System\.setProperty\("java\.util\.logging\.manager", "org\.apache\.logging\.log4j\.jul\.LogManager"\);\n)(\s+)GeyserStandaloneLogger\.setupStreams\(\);',
    r'\1\2org.apache.logging.log4j.status.StatusLogger.getLogger().setLevel(Level.OFF);\n\2GeyserStandaloneLogger.setupStreams();',
)
# 5-6. Open useGui + configFilename so the bridge can configure them
#      without reflection. Anchor on the type+initializer so a rename
#      to a similarly-named field can't silently match.
once(r'\bprivate (boolean useGui\b\s*=)', r'public \1')
once(r'\bprivate (String configFilename\b\s*=)', r'public \1')

open(path, 'w').write(src)
print(f"  patched {count} sites")
PY

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

echo "▸ Validating Geyser source assumptions"
"${PYTHON_BIN}" "${REPO_ROOT}/build/validate-geyser-source.py" "${GEYSER_DIR}"

echo "✓ Overlay applied; tree at ${GEYSER_DIR}"

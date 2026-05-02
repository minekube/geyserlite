#!/usr/bin/env bash
# Single source of truth for native-image flags.
# Sourced by build/Dockerfile. Each flag is annotated with what it does
# and what we measured it saving. See ../ROADMAP.md for the full memory journey.
#
# Usage:
#   source build/flags.sh
#   native-image "${NI_FLAGS_COMMON[@]}" "${NI_FLAGS_EXECUTABLE[@]}" -o geyserlite -jar Geyser-Standalone.jar
#   native-image "${NI_FLAGS_COMMON[@]}" "${NI_FLAGS_SHARED[@]}" -o libgeyserlite ...

# Architecture-specific flags. Detected from `uname -m` so the same
# flags.sh works under both linux/amd64 and linux/arm64 buildx targets.
#
# Static musl linking is only applied on amd64. On aarch64 the GraalVM
# 21-ol9 image doesn't ship the static JDK pieces required for
# --libc=musl, so we fall back to dynamic glibc (still produces a clean
# ELF, just one with the standard glibc runtime dependency).
NI_LIBC_FLAGS=()
case "$(uname -m)" in
    x86_64|amd64)
        NI_MARCH="-march=x86-64-v2"
        NI_LIBC_FLAGS=(--static --libc=musl)
        ;;
    aarch64|arm64)
        NI_MARCH="-march=armv8-a"
        ;;
    *)
        NI_MARCH="-march=compatibility"
        ;;
esac

# Flags shared by both the ELF and the .so build.
NI_FLAGS_COMMON=(
    # Reflection / JNI metadata captured by the tracing agent.
    -H:ConfigurationFileDirectories=agent-config

    # Bundle Geyser's runtime resources into the image. Without this,
    # the binary throws "Unable to find resource: custom-skulls.yml"
    # (and similar) because native-image discards classpath resources
    # by default. The patterns cover the YAML configs, JSON schemas,
    # mappings, language packs, and the embedded resource pack.
    '-H:IncludeResources=^(custom-skulls\.yml|permissions\.yml|.+\.json|.+\.properties|.+\.lang|languages/.+|mappings/.+|bedrock/.+|assets/.+|.+\.mcpack)$'

    # Don't fall back to bytecode at runtime if static analysis can't reach a method —
    # we want a true native binary, not a JVM wrapper.
    --no-fallback

    # Geyser fetches Mojang manifests over HTTPS at startup.
    --enable-url-protocols=https,http

    # Force log4j to initialize at build time so its ServiceLoader reflection
    # runs in the JVM (where reflection works) instead of native runtime
    # (where it doesn't, because of LambdaMetafactory hidden classes).
    # Also need terminalconsoleappender + jline + jansi (interactive
    # console layer that log4j2 wires up for Geyser-Standalone), plus
    # snakeyaml whose dynamic constructor synthesis hits the same
    # "hidden classes at runtime" wall.
    --initialize-at-build-time=org.apache.logging.log4j,net.minecrell.terminalconsoleappender,org.jline,org.fusesource.jansi,org.yaml.snakeyaml,java.awt.Color,com.sun.jna

    # Override init policy for AWT internals that pull in headless toolkit state
    # we don't want frozen into the image.
    --initialize-at-run-time=sun.awt.HeadlessToolkit,sun.awt.SunHints

    # Stricter image-heap policy. Catches accidentally retained mutable state at
    # build time. Slightly larger binary; cleaner image.
    --strict-image-heap

    # Linkage: amd64 statically links musl for a single-file ELF with no
    # glibc dependency. aarch64 falls back to dynamic glibc because the
    # GraalVM 21-ol9 image doesn't ship the static JDK pieces required
    # for --libc=musl on aarch64. NI_LIBC_FLAGS is empty there.
    "${NI_LIBC_FLAGS[@]}"

    # Architecture-specific baseline. x86-64-v2 covers every modern x86 host
    # (Fly machines, most VPSs); on aarch64 we use armv8-a, the standard
    # baseline.
    "$NI_MARCH"

    -O2

    # Bake heap settings into the image so runtime needs no -Xmx parsing.
    -R:MaxHeapSize=64m

    -H:+UnlockExperimentalVMOptions
    -H:+RemoveSaturatedTypeFlows

    # Production: don't pay for stack-trace formatting on rare paths.
    -H:-ReportExceptionStackTraces

    # Build-time resources (CPU on the build host, not runtime).
    -J-Xmx14g
)

# Flags specific to the standalone executable build.
NI_FLAGS_EXECUTABLE=(
    # Geyser's main class.
    --no-fallback
)

# Flags specific to the shared library build.
NI_FLAGS_SHARED=(
    --shared
    # @CEntryPoint exports declared in
    # build/overlay/geyserlite-native/.../GeyserBridge.java are picked up automatically.
)

# PGO is NOT in the CI build because it requires a live load run.
# To rebuild with PGO locally, see ../docs/tuning.md.

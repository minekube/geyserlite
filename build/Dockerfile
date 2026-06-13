# syntax=docker/dockerfile:1.24
#
# Builds two artifacts from the same Geyser source tree:
#   /out/geyserlite           — standalone ELF (drop-in for Geyser-Standalone.jar)
#   /out/libgeyserlite.so     — shared library (loaded by go/ and rust/ libraries)
#   /out/libgeyserlite.h      — auto-generated C header for the @CEntryPoint exports
#
# See build/flags.sh for the full flag set with annotations.

ARG GRAALVM_IMAGE=ghcr.io/graalvm/native-image-community:21-ol9

# ── Build stage ──────────────────────────────────────────────────────────────
FROM ${GRAALVM_IMAGE} AS builder

# musl + static zlib for --libc=musl static linking.
#
# The GraalVM 21-ol9 image baked-in glibc-static 2.34-83 from @System,
# which depsolves against glibc-devel 2.34-83 only — but ol9_appstream
# has moved on to 2.34-231 and refuses to coexist. microdnf doesn't
# support --allowerasing, so we instead remove the stale static libs
# from @System first; the gcc/gcc-c++ install then pulls fresh devel
# packages from appstream cleanly.
# (glibc-static itself isn't needed: the static-link target uses musl,
# built below from source.)
RUN rpm -e --nodeps glibc-static libstdc++-static 2>/dev/null || true \
    && microdnf install -y --setopt=install_weak_deps=0 \
        git curl tar gzip findutils python3 \
        gcc gcc-c++ zlib-static \
    && microdnf clean all

# Build a static zlib against musl (the system zlib is glibc).
RUN curl -fsSLO https://musl.libc.org/releases/musl-1.2.5.tar.gz \
    && tar xzf musl-1.2.5.tar.gz && cd musl-1.2.5 \
    && ./configure --prefix=/usr/local/musl --disable-shared \
    && make -j"$(nproc)" install \
    && cd .. && rm -rf musl-1.2.5* \
    && curl -fsSL https://github.com/madler/zlib/releases/download/v1.3.1/zlib-1.3.1.tar.gz | tar xz \
    && cd zlib-1.3.1 \
    && CC=/usr/local/musl/bin/musl-gcc ./configure --prefix=/usr/local/musl --static \
    && make -j"$(nproc)" install \
    && cp /usr/local/musl/lib/libz.a /usr/lib64/ \
    && cd .. && rm -rf zlib-1.3.1 \
    # GraalVM's --libc=musl looks up the C compiler as
    # `<arch>-linux-musl-gcc` on PATH (see CCompilerInvoker.java).
    # Provide both linkage names from our musl install.
    && ln -sf /usr/local/musl/bin/musl-gcc /usr/local/bin/x86_64-linux-musl-gcc \
    && ln -sf /usr/local/musl/bin/musl-gcc /usr/local/bin/aarch64-linux-musl-gcc

WORKDIR /work
COPY build/geyser.version build/apply-overlay.sh build/flags.sh build/augment-annotation-reflect-config.py build/mark-reflect-unsafe.py build/validate-geyser-source.py ./build/
COPY build/overlay/   ./build/overlay/
COPY build/patches/   ./build/patches/
COPY build/agent-config/ ./build/agent-config/

# Apply overlay + patches to a fresh Geyser clone at the pinned ref.
RUN bash build/apply-overlay.sh

WORKDIR /work/build/.work/Geyser

# Build the standalone shadow JAR (input to native-image).
RUN ./gradlew :standalone:shadowJar --no-daemon

# Geyser's annotation processor writes resource files listing classes that
# AnnotationUtils loads with Class.forName at runtime. Keep those classes
# reachable for both native-image targets using the exact pinned JAR's lists.
RUN python3 /work/build/augment-annotation-reflect-config.py \
      --jar bootstrap/standalone/build/libs/Geyser-Standalone.jar \
      --config-dir /work/build/.work/Geyser/agent-config

# Patch reflect-config to mark non-JDK classes as unsafe-allocated for Gson's UnsafeAllocator path.
RUN cp -R /work/build/.work/Geyser/agent-config /tmp/agent-config
RUN python3 /work/build/mark-reflect-unsafe.py --config-dir /tmp/agent-config

# Stage the JAR + agent-config into a flat build dir so native-image flags resolve cleanly.
WORKDIR /build
RUN cp /work/build/.work/Geyser/bootstrap/standalone/build/libs/Geyser-Standalone.jar ./ \
    && cp -r /tmp/agent-config ./

# Build the standalone executable.
RUN bash -c 'source /work/build/flags.sh \
    && native-image "${NI_FLAGS_COMMON[@]}" \
        -jar Geyser-Standalone.jar \
        --parallelism="$(nproc)" \
        -o geyserlite \
    && strip geyserlite'

# Build the shared library from the same Geyser-Standalone.jar.
# The @CEntryPoint methods in build/overlay/geyserlite-native/.../GeyserBridge.java
# are picked up because the overlay registered that subproject in settings.gradle.kts.
WORKDIR /work/build/.work/Geyser
RUN ./gradlew :geyserlite-native:nativeCompile --no-daemon \
    || (echo "geyserlite-native subproject build failed — see build/overlay/" && exit 1)

# Collect outputs.
# When @CEntryPoint methods are reachable, GraalVM emits libgeyserlite.h
# next to the .so. When they're not, only graal_isolate.h is produced —
# we fall back to that so the build doesn't break, but the resulting
# header carries no project-specific exports until the bridge is wired.
RUN NC=/work/build/.work/Geyser/geyserlite-native/build/native/nativeCompile \
    && mkdir -p /out \
    && cp /build/geyserlite /out/geyserlite \
    && cp "$NC/libgeyserlite.so" /out/libgeyserlite.so \
    && if [ -f "$NC/libgeyserlite.h" ]; then \
         cp "$NC/libgeyserlite.h" /out/libgeyserlite.h; \
       else \
         echo "WARN: libgeyserlite.h not generated — using graal_isolate.h passthrough" >&2; \
         cp "$NC/graal_isolate.h" /out/libgeyserlite.h; \
       fi

# The amd64 executable is fully static, while arm64 currently falls back to
# dynamic glibc and zlib. Stage zlib from the target platform so the same final
# image works on both architectures.
FROM debian:bookworm-slim AS runtime-deps
RUN mkdir -p /runtime-libs \
    && cp -L /usr/lib/*-linux-gnu/libz.so.1 /runtime-libs/libz.so.1

# ── Output stage: minimal runnable image + permissions.yml ───────────────────
FROM gcr.io/distroless/base-debian12 AS image
COPY --from=builder /out/geyserlite /geyserlite
COPY --from=runtime-deps /runtime-libs/libz.so.1 /usr/lib/libz.so.1
# permissions.yml moved upstream from bootstrap/standalone to core/
# (see GeyserMC/Geyser commit history). Pinning to the new location.
COPY --from=builder /work/build/.work/Geyser/core/src/main/resources/permissions.yml /permissions.yml
LABEL org.opencontainers.image.source="https://github.com/minekube/geyserlite"
LABEL org.opencontainers.image.description="GeyserMC built with GraalVM native-image — small, fast, embeddable"
ENTRYPOINT ["/geyserlite", "--nogui"]

# ── Output stage: just the shared library + header ───────────────────────────
FROM scratch AS shared
COPY --from=builder /out/libgeyserlite.so /libgeyserlite.so
COPY --from=builder /out/libgeyserlite.h  /libgeyserlite.h

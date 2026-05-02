# syntax=docker/dockerfile:1.7
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
    && cd .. && rm -rf zlib-1.3.1

WORKDIR /work
COPY build/geyser.version build/apply-overlay.sh build/flags.sh ./build/
COPY build/overlay/   ./build/overlay/
COPY build/patches/   ./build/patches/
COPY build/agent-config/ ./build/agent-config/

# Apply overlay + patches to a fresh Geyser clone at the pinned ref.
RUN bash build/apply-overlay.sh

WORKDIR /work/build/.work/Geyser

# Build the standalone shadow JAR (input to native-image).
RUN ./gradlew :standalone:shadowJar --no-daemon

# Patch reflect-config to mark non-JDK classes as unsafe-allocated for Gson's UnsafeAllocator path.
COPY build/agent-config /tmp/agent-config
RUN python3 -c "import json,sys; \
    p='/tmp/agent-config/reflect-config.json'; \
    cfg=json.load(open(p)); n=0; \
    [(e.update({'unsafeAllocated': True}), n := n+1) for e in cfg \
       if not e.get('name','').startswith(('java.','javax.','sun.','jdk.','com.sun.','com.oracle.','org.graalvm.')) \
       and not e.get('unsafeAllocated')]; \
    json.dump(cfg, open(p,'w'), indent=2); \
    print(f'patched {n} entries')"

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
RUN mkdir -p /out \
    && cp /build/geyserlite /out/geyserlite \
    && cp /work/build/.work/Geyser/geyserlite-native/build/native/nativeCompile/libgeyserlite.so /out/libgeyserlite.so \
    && cp /work/build/.work/Geyser/geyserlite-native/build/native/nativeCompile/libgeyserlite.h  /out/libgeyserlite.h

# ── Output stage: scratch image with just the ELF + permissions.yml ──────────
FROM scratch AS image
COPY --from=builder /out/geyserlite /geyserlite
COPY --from=builder /work/build/.work/Geyser/bootstrap/standalone/src/main/resources/permissions.yml /permissions.yml
LABEL org.opencontainers.image.source="https://github.com/minekube/geyserlite"
LABEL org.opencontainers.image.description="GeyserMC built with GraalVM native-image — small, fast, embeddable"
ENTRYPOINT ["/geyserlite", "--nogui"]

# ── Output stage: just the shared library + header ───────────────────────────
FROM scratch AS shared
COPY --from=builder /out/libgeyserlite.so /libgeyserlite.so
COPY --from=builder /out/libgeyserlite.h  /libgeyserlite.h

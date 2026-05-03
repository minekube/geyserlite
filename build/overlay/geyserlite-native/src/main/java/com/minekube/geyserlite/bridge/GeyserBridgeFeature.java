// SPDX-License-Identifier: MIT
package com.minekube.geyserlite.bridge;

import org.graalvm.nativeimage.hosted.Feature;

/**
 * GraalVM Feature that pulls {@link GeyserBridge} into the native-image
 * analysis tree.
 *
 * <p>For an executable build, native-image walks from {@code main} and
 * discovers everything reachable from there. For a shared-library
 * build, there's no {@code main}, so we need to tell native-image
 * which classes contain the {@code @CEntryPoint} methods — otherwise
 * the analysis prunes them as unreachable and the produced
 * {@code libgeyserlite.so} contains only the GraalVM runtime symbols
 * (no {@code geyser_init}, etc.).
 *
 * <p>Registered via the {@code -H:Features=...} build argument in
 * {@code build.gradle.kts}.
 */
public final class GeyserBridgeFeature implements Feature {
    @Override
    public void duringSetup(DuringSetupAccess access) {
        // findClassByName() forces the class to be loaded, which makes
        // its @CEntryPoint methods discoverable to the analyzer.
        Class<?> bridge = access.findClassByName(
            "com.minekube.geyserlite.bridge.GeyserBridge");
        if (bridge == null) {
            throw new IllegalStateException(
                "GeyserBridge not on the build classpath; check the "
              + "geyserlite-native subproject's compile dependencies.");
        }
    }
}

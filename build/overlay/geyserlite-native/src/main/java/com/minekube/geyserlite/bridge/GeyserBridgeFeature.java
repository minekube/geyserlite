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

        // Pull log4j's core configuration machinery into the image at BUILD time.
        //
        // The executable build gets that for free: Geyser's own static loggers call
        // LogManager.getLogger() while the analysis runs, so the
        // LogManager -> Log4jContextFactory -> LoggerContext -> AbstractConfiguration
        // -> WatchManager chain is analysed there and — with
        // --initialize-at-build-time=org.apache.logging.log4j — initialised in the
        // builder JVM. This image's analysis root is GeyserBridge, and
        // GeyserBridge's equivalent log4j wiring deliberately runs at RUNTIME
        // (GeyserBridge.run mirrors GeyserStandaloneBootstrap.main's prelude), so
        // without this touch the chain is first entered at runtime, where log4j's
        // ServiceLoader implementation defines a lambda class through
        // ClassLoader.defineClass to locate WatchEventService implementations:
        //
        //   UnsupportedFeatureError: Classes cannot be defined at runtime by default
        //   ... Tried to define class '.../core/util/WatchManager$$Lambda...'
        //   ERROR StatusConsoleListener Unable to locate appender "TerminalConsole"
        //
        // Geyser is then left without a working logger and the Bedrock listener never
        // binds (the go/rust "run library against the just-built .so" gates).
        // Running the same prelude here registers the same reachability at build
        // time, mirroring the ELF.
        System.setProperty("java.util.logging.manager",
            "org.apache.logging.log4j.jul.LogManager");
        org.apache.logging.log4j.status.StatusLogger.getLogger()
            .setLevel(org.apache.logging.log4j.Level.OFF);
        org.apache.logging.log4j.LogManager.getContext(false);
    }
}

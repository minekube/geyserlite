/*
 * geyserlite — GraalVM @CEntryPoint exports for in-process embedding.
 *
 * Compiled into libgeyserlite.so by GraalVM native-image --shared. The Go
 * library (purego) and Rust crate (libloading) call these as plain C
 * functions via dlopen/dlsym.
 *
 * SPDX-License-Identifier: MIT
 */
package com.minekube.geyserlite.bridge;

import io.netty.util.ResourceLeakDetector;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.atomic.AtomicBoolean;
import org.geysermc.geyser.platform.standalone.GeyserStandaloneBootstrap;
import org.geysermc.geyser.text.GeyserLocale;
import org.graalvm.nativeimage.IsolateThread;
import org.graalvm.nativeimage.c.function.CEntryPoint;
import org.graalvm.nativeimage.c.type.CCharPointer;
import org.graalvm.nativeimage.c.type.CTypeConversion;

/**
 * C-ABI entry points exposed to host languages (Go, Rust, anything with FFI).
 *
 * <p>Lifecycle the host drives:
 * <pre>
 *   graal_create_isolate(...)         // returns isolate + main thread handle
 *   geyser_init(thread, configPath)   // store config path; idempotent
 *   geyser_run(thread)                // BLOCKS on a shutdown latch
 *
 *   // from any other thread (after graal_attach_thread):
 *   geyser_shutdown(thread)           // counts down the latch; run() returns
 *   geyser_status(thread)             // 1 if running, 0 otherwise
 *
 *   graal_tear_down_isolate(thread)   // host owns isolate teardown
 * </pre>
 *
 * <p>Implementation: {@link #run} reproduces the prelude of
 * {@link GeyserStandaloneBootstrap#main} (log4j wiring, locale init,
 * netty leak-detector off), instantiates the standalone bootstrap with
 * the configured {@code configPath}, and calls {@code onGeyserInitialize()}.
 * The standalone bootstrap is patched at apply-overlay time to honor a
 * {@code geyserlite.embedded} system property, which makes it skip its
 * stdin command-prompt loop and its terminal {@code System.exit} calls.
 * That lets the bridge own the lifecycle and the host process keep
 * running after Geyser stops.
 */
public final class GeyserBridge {

    /** Single source of truth for the property name. The
     * apply-overlay.sh patch script reads the same string from this file
     * via grep at apply time, so there's exactly one place to change it.
     * @see com.minekube.geyserlite.bridge.GeyserBridge#EMBED_PROP */
    static final String EMBED_PROP = "geyserlite.embedded";

    // State managed by the entry-point methods. Static because the C ABI
    // has no this-pointer and we run one instance per isolate. The latch
    // is non-final / re-allocated per run() so a fresh init/run cycle
    // after a shutdown still blocks correctly.
    private static GeyserStandaloneBootstrap bootstrap;
    private static volatile CountDownLatch shutdownLatch = new CountDownLatch(1);
    private static final AtomicBoolean started = new AtomicBoolean(false);
    private static final AtomicBoolean running = new AtomicBoolean(false);
    private static volatile String configPath = "config.yml";

    private GeyserBridge() {}

    /**
     * Stash the config-file path the next {@link #run} call should use.
     * Called once before run; idempotent.
     *
     * @param cConfigPath null-terminated UTF-8 path to a Geyser config.yml
     * @return 0 on success, -1 on error
     */
    @CEntryPoint(name = "geyser_init")
    public static int init(IsolateThread thread, CCharPointer cConfigPath) {
        try {
            String path = CTypeConversion.toJavaString(cConfigPath);
            if (path != null && !path.isEmpty()) {
                configPath = path;
            }
            // Gate the patched standalone bootstrap into embed mode —
            // skips the stdin command-prompt loop and the host-killing
            // System.exit calls. See apply-overlay.sh.
            System.setProperty(EMBED_PROP, "true");
            return 0;
        } catch (Throwable t) {
            t.printStackTrace();
            return -1;
        }
    }

    /**
     * Boot Geyser and block until {@link #shutdown} is called or Geyser
     * exits on its own. Must run on the same OS thread that owns the
     * IsolateThread (the create_isolate thread, in practice).
     *
     * @return 0 on clean shutdown, -1 on init/runtime error, -2 if run
     *         was already called on this isolate
     */
    @CEntryPoint(name = "geyser_run")
    public static int run(IsolateThread thread) {
        if (!started.compareAndSet(false, true)) {
            return -2;
        }
        // Fresh latch per run cycle so a second init/run after a clean
        // shutdown still blocks correctly. Volatile ensures shutdown()
        // sees this assignment.
        shutdownLatch = new CountDownLatch(1);
        try {
            // Mirror GeyserStandaloneBootstrap.main()'s prelude — these
            // are the side effects the standalone CLI sets up before any
            // bootstrap method runs. We can't call main() directly: it
            // parses argv, prints help on some inputs, and uses
            // System.console() / awt headless detection that don't apply
            // to an embed.
            if (System.getProperty("io.netty.leakDetection.level") == null) {
                ResourceLeakDetector.setLevel(ResourceLeakDetector.Level.DISABLED);
            }
            System.setProperty("java.util.logging.manager",
                "org.apache.logging.log4j.jul.LogManager");
            org.apache.logging.log4j.status.StatusLogger.getLogger()
                .setLevel(org.apache.logging.log4j.Level.OFF);
            // Skip GeyserStandaloneLogger.setupStreams(): it depends on
            // terminalconsoleappender (not on the embed compile classpath)
            // and reassigns System.out/err for the standalone CLI prompt
            // — the host process already owns those streams.

            bootstrap = new GeyserStandaloneBootstrap();
            bootstrap.useGui = false;             // patched to public
            bootstrap.configFilename = configPath; // patched to public

            GeyserLocale.init(bootstrap);
            bootstrap.onGeyserInitialize();
            // onGeyserInitialize → onGeyserEnable returns now: the
            // patched geyserLogger.start() is gated behind EMBED_PROP.
            // The bedrock listener was started by GeyserImpl.start()
            // inside onGeyserEnable.

            running.set(true);
            shutdownLatch.await();
            running.set(false);

            // onGeyserShutdown's System.exit is patched to a no-op
            // under EMBED_PROP.
            bootstrap.onGeyserShutdown();
            return 0;
        } catch (Throwable t) {
            t.printStackTrace();
            running.set(false);
            return -1;
        } finally {
            // Drop references so the host can re-init on a fresh isolate
            // without leaking the previous Geyser singletons through
            // static state.
            bootstrap = null;
            started.set(false);
        }
    }

    /**
     * Signal {@link #run} to return. Idempotent. Safe to call from a
     * different OS thread than {@code run()}, provided the caller has
     * its own IsolateThread (use {@code graal_attach_thread}).
     *
     * @return 0 on success
     */
    @CEntryPoint(name = "geyser_shutdown")
    public static int shutdown(IsolateThread thread) {
        try {
            // No null check needed: shutdownLatch is initialized at
            // class load and replaced atomically inside run() before
            // anything could await it. A shutdown call before run() is
            // harmless — it just pre-arms the latch.
            shutdownLatch.countDown();
            return 0;
        } catch (Throwable t) {
            t.printStackTrace();
            return -1;
        }
    }

    /**
     * Liveness probe the host can poll while waiting for the bedrock
     * listener to come up.
     *
     * @return 1 if Geyser is running, 0 otherwise
     */
    @CEntryPoint(name = "geyser_status")
    public static int status(IsolateThread thread) {
        return running.get() ? 1 : 0;
    }
}

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

import org.graalvm.nativeimage.IsolateThread;
import org.graalvm.nativeimage.c.function.CEntryPoint;
import org.graalvm.nativeimage.c.type.CCharPointer;
import org.graalvm.nativeimage.c.type.CTypeConversion;

/**
 * C-ABI entry points exposed to host languages (Go, Rust, anything with FFI).
 *
 * Lifecycle:
 *   {@link #init} once with a config-file path.
 *   {@link #run} blocks until the host calls {@link #shutdown} on another thread,
 *   or Geyser exits on its own (returns non-zero).
 *
 * All methods are reentrant only across distinct {@link IsolateThread}s.
 * Hosts should create one isolate per geyserlite instance (typically one per
 * process, since multi-instance is not yet supported).
 */
public final class GeyserBridge {

    private GeyserBridge() {}

    /**
     * Initialize Geyser with a config file path. Idempotent within an isolate.
     *
     * @param configPath null-terminated UTF-8 path to a Geyser config.yml
     * @return 0 on success, negative on error
     */
    @CEntryPoint(name = "geyser_init")
    public static int init(IsolateThread thread, CCharPointer configPath) {
        try {
            String path = CTypeConversion.toJavaString(configPath);
            // TODO: wire Geyser bootstrap with explicit config path.
            // For now: System.setProperty so existing standalone bootstrap picks it up.
            System.setProperty("geyser.config.path", path);
            return 0;
        } catch (Throwable t) {
            t.printStackTrace();
            return -1;
        }
    }

    /**
     * Start Geyser and block. Returns when {@link #shutdown} is called or
     * Geyser exits on its own.
     *
     * @return 0 on clean shutdown, negative on error
     */
    @CEntryPoint(name = "geyser_run")
    public static int run(IsolateThread thread) {
        try {
            // TODO: invoke Geyser's lifecycle inline (not via System.exit).
            // org.geysermc.geyser.platform.standalone.GeyserStandaloneBootstrap.main(new String[]{"--nogui"});
            return 0;
        } catch (Throwable t) {
            t.printStackTrace();
            return -1;
        }
    }

    /**
     * Request a graceful shutdown. Causes a concurrent {@link #run} call to return.
     *
     * @return 0 on success, negative on error
     */
    @CEntryPoint(name = "geyser_shutdown")
    public static int shutdown(IsolateThread thread) {
        try {
            // TODO: org.geysermc.geyser.GeyserImpl.getInstance().shutdown();
            return 0;
        } catch (Throwable t) {
            t.printStackTrace();
            return -1;
        }
    }

    /**
     * Lightweight liveness probe.
     *
     * @return 1 if Geyser is running and accepting connections, 0 otherwise
     */
    @CEntryPoint(name = "geyser_status")
    public static int status(IsolateThread thread) {
        try {
            // TODO: check Geyser bootstrap state.
            return 1;
        } catch (Throwable t) {
            return 0;
        }
    }
}

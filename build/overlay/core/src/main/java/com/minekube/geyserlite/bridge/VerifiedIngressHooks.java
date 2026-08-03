/*
 * geyserlite — shared native ingress lifecycle hooks.
 *
 * SPDX-License-Identifier: MIT
 */
package com.minekube.geyserlite.bridge;

import java.util.ServiceLoader;
import org.geysermc.geyser.GeyserImpl;
import org.geysermc.geyser.api.connection.GeyserConnection;
import org.geysermc.geyser.api.event.bedrock.SessionInitializeEvent;
import org.geysermc.geyser.event.type.SessionDisconnectEventImpl;

public final class VerifiedIngressHooks {
    public interface Sink {
        void onConnectionOpened(GeyserConnection connection);

        void onConnectionClosed(GeyserConnection connection);

        void onVerified(GeyserConnection connection, byte[] frame);
    }

    private static Sink sink;
    private static GeyserImpl installed;

    private VerifiedIngressHooks() {}

    public static synchronized void register(Sink candidate) {
        if (candidate == null) {
            throw new IllegalArgumentException("null verified ingress sink");
        }
        if (sink != null && sink != candidate) {
            throw new IllegalStateException("verified ingress sink already registered");
        }
        sink = candidate;
    }

    public static synchronized void unregister(Sink candidate) {
        if (sink == candidate) {
            sink = null;
        }
    }

    public static synchronized void install(GeyserImpl geyser) {
        if (installed == geyser) {
            return;
        }
        installed = geyser;
        geyser.eventBus().subscribe(geyser, SessionInitializeEvent.class,
                event -> {
                    Sink current = sink;
                    if (current != null) {
                        current.onConnectionOpened(event.connection());
                    }
                });
        geyser.eventBus().subscribe(geyser, SessionDisconnectEventImpl.class,
                event -> {
                    Sink current = sink;
                    if (current != null) {
                        current.onConnectionClosed(event.connection());
                    }
                });
    }

    public static byte[] verify(GeyserConnection connection, byte[] correlation, long expiresUnixMs) {
        for (ConnectVerifiedIngressVerifier verifier : ServiceLoader.load(ConnectVerifiedIngressVerifier.class)) {
            byte[] frame = verifier.verify(connection, correlation.clone(), expiresUnixMs);
            if (frame != null) {
                return frame.clone();
            }
        }
        return null;
    }
}

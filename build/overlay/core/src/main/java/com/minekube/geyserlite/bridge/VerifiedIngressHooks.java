/*
 * geyserlite — shared native ingress lifecycle hooks.
 *
 * SPDX-License-Identifier: MIT
 */
package com.minekube.geyserlite.bridge;

import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.ThreadFactory;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;
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

    public static final class ConnectVerifierHandle {
        private final long id;

        private ConnectVerifierHandle(long id) {
            this.id = id;
        }
    }

    public static final class VerificationHandoff {
        private final ConnectVerifierHandle handle;
        private final GeyserConnection connection;
        private final byte[] correlation;
        private final long expiresUnixMs;
        private final CountDownLatch completed = new CountDownLatch(1);
        private final AtomicBoolean open = new AtomicBoolean(true);
        private volatile byte[] frame;
        private volatile Throwable failure;

        private VerificationHandoff(ConnectVerifierHandle handle, GeyserConnection connection,
                                    byte[] correlation, long expiresUnixMs) {
            this.handle = handle;
            this.connection = connection;
            this.correlation = correlation.clone();
            this.expiresUnixMs = expiresUnixMs;
        }

        public ConnectVerifierHandle verifierHandle() {
            return handle;
        }

        public GeyserConnection connection() {
            return connection;
        }

        public byte[] correlation() {
            return correlation.clone();
        }

        public long expiresUnixMs() {
            return expiresUnixMs;
        }

        public boolean complete(byte[] candidate) {
            synchronized (VerifiedIngressHooks.class) {
                if (registration == null || registration.handle != handle) {
                    return false;
                }
                if (!open.compareAndSet(true, false)) {
                    return false;
                }
            }
            if (candidate == null || candidate.length < 1 || candidate.length > 4096) {
                failure = new IllegalArgumentException("invalid verified ingress frame");
                completed.countDown();
                return false;
            }
            frame = candidate.clone();
            completed.countDown();
            return true;
        }

        private void fail(Throwable reason) {
            if (open.compareAndSet(true, false)) {
                failure = reason;
                completed.countDown();
            }
        }

        private void expire() {
            fail(new IllegalStateException("verified ingress handoff expired"));
        }

        private byte[] await(long timeoutMillis) throws InterruptedException {
            long remaining = Math.min(timeoutMillis, expiresUnixMs - System.currentTimeMillis());
            if (remaining <= 0 || !completed.await(remaining, TimeUnit.MILLISECONDS)) {
                expire();
                return null;
            }
            if (System.currentTimeMillis() >= expiresUnixMs) {
                expire();
                return null;
            }
            if (failure != null) {
                return null;
            }
            return frame == null ? null : frame.clone();
        }
    }

    private static final class Registration {
        private final ConnectVerifierHandle handle;
        private final ConnectVerifiedIngressVerifier verifier;

        private Registration(ConnectVerifierHandle handle, ConnectVerifiedIngressVerifier verifier) {
            this.handle = handle;
            this.verifier = verifier;
        }
    }

    private static Sink sink;
    private static GeyserImpl installed;
    private static Registration registration;
    private static final AtomicLong verifierIDs = new AtomicLong();
    private static ExecutorService verifierExecutor;

    private VerifiedIngressHooks() {}

    public static synchronized ConnectVerifierHandle registerConnectVerifier(
            ConnectVerifiedIngressVerifier verifier) {
        if (verifier == null) {
            throw new IllegalArgumentException("null Connect verifier");
        }
        if (registration != null) {
            if (registration.verifier == verifier) {
                return registration.handle;
            }
            throw new IllegalStateException("Connect verifier already registered");
        }
        ConnectVerifierHandle handle = new ConnectVerifierHandle(verifierIDs.incrementAndGet());
        registration = new Registration(handle, verifier);
        return handle;
    }

    public static synchronized void unregisterConnectVerifier(ConnectVerifierHandle handle) {
        if (registration != null && registration.handle == handle) {
            registration = null;
        }
    }

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

    public static byte[] verify(GeyserConnection connection, byte[] correlation, long expiresUnixMs,
                                long timeoutMillis) {
        Registration current;
        synchronized (VerifiedIngressHooks.class) {
            current = registration;
        }
        if (current == null || timeoutMillis <= 0) {
            return null;
        }
        VerificationHandoff handoff = new VerificationHandoff(current.handle, connection, correlation, expiresUnixMs);
        Future<?> task = verifierExecutor().submit(() -> {
            try {
                current.verifier.verify(current.handle, handoff);
            } catch (Throwable error) {
                handoff.fail(error);
            }
        });
        try {
            return handoff.await(timeoutMillis);
        } catch (InterruptedException interrupted) {
            Thread.currentThread().interrupt();
            handoff.expire();
            return null;
        } finally {
            handoff.expire();
            task.cancel(true);
        }
    }

    private static synchronized ExecutorService verifierExecutor() {
        if (verifierExecutor == null) {
            verifierExecutor = Executors.newCachedThreadPool(new VerifierThreadFactory());
        }
        return verifierExecutor;
    }

    private static final class VerifierThreadFactory implements ThreadFactory {
        @Override
        public Thread newThread(Runnable runnable) {
            Thread thread = new Thread(runnable, "geyserlite-connect-verifier");
            thread.setDaemon(true);
            return thread;
        }
    }
}

/*
 * geyserlite — VerifiedIngressV1 embedded callback transport.
 *
 * SPDX-License-Identifier: MIT
 */
package org.geysermc.geyser.util;

import java.util.Arrays;
import java.util.HashMap;
import java.util.IdentityHashMap;
import java.util.Map;
import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.ThreadFactory;
import com.minekube.geyserlite.bridge.VerifiedIngressHooks;
import org.geysermc.geyser.api.connection.GeyserConnection;
import org.graalvm.nativeimage.c.function.CFunctionPointer;
import org.graalvm.nativeimage.c.function.InvokeCFunctionPointer;
import org.graalvm.nativeimage.c.type.CCharPointer;
import org.graalvm.nativeimage.c.type.CTypeConversion;

public final class IngressTransport implements VerifiedIngressHooks.Sink {
    public static final int CALLBACK_REGISTRATION_OK = 0;
    public static final int ASSIGN_OK = 0;
    public static final int ASSIGN_UNKNOWN_OR_CLOSED_HANDLE = -1;
    public static final int ASSIGN_DUPLICATE_HANDLE_OR_CORRELATION = -2;
    public static final int ASSIGN_INVALID_OR_EXPIRED_TIME = -3;
    public static final int ASSIGN_WRONG_CONNECTION_STATE = -4;

    static final int CORRELATION_BYTES = 16;
    static final int MIN_FRAME_BYTES = 1;
    static final int MAX_FRAME_BYTES = 4096;
    static final long MAX_LIFETIME_MS = 5000;

    public interface OpenCallback extends CFunctionPointer {
        @InvokeCFunctionPointer
        void invoke(long handle);
    }

    public interface VerifiedCallback extends CFunctionPointer {
        @InvokeCFunctionPointer
        void invoke(CCharPointer correlation, CCharPointer frame, int frameLength, long expiresUnixMs);
    }

    private static final class Assignment {
        final long handle;
        final byte[] correlation;
        final long expiresUnixMs;
        ScheduledFuture<?> expiry;

        Assignment(long handle, byte[] correlation, long expiresUnixMs) {
            this.handle = handle;
            this.correlation = correlation;
            this.expiresUnixMs = expiresUnixMs;
        }
    }

    private final AtomicLong nextHandle = new AtomicLong(1);
    private final Map<Long, Object> connections = new HashMap<>();
    private final Map<Object, Long> handles = new IdentityHashMap<>();
    private final Map<Long, Assignment> assignments = new HashMap<>();
    private final Map<String, Long> correlations = new HashMap<>();
    private final ScheduledExecutorService expiryExecutor = Executors.newSingleThreadScheduledExecutor(new DaemonThreadFactory());

    private OpenCallback openCallback;
    private VerifiedCallback verifiedCallback;

    public synchronized int setCallbacks(OpenCallback open, VerifiedCallback verified) {
        if (open == null && verified == null) {
            openCallback = null;
            verifiedCallback = null;
            clearAssignments();
            return CALLBACK_REGISTRATION_OK;
        }
        if (open == null || verified == null) {
            return ASSIGN_WRONG_CONNECTION_STATE;
        }
        openCallback = open;
        verifiedCallback = verified;
        return CALLBACK_REGISTRATION_OK;
    }

    public int assign(long handle, CCharPointer correlationPointer, long expiresUnixMs) {
        if (correlationPointer == null) {
            return ASSIGN_WRONG_CONNECTION_STATE;
        }
        byte[] correlation = new byte[CORRELATION_BYTES];
        for (int i = 0; i < correlation.length; i++) {
            correlation[i] = correlationPointer.read(i);
        }
        return assign(handle, correlation, expiresUnixMs);
    }

    @Override
    public void onConnectionOpened(GeyserConnection connection) {
        openConnection(connection);
    }

    @Override
    public void onConnectionClosed(GeyserConnection connection) {
        closeConnection(connection);
    }

    synchronized void openConnection(GeyserConnection connection) {
        if (handles.containsKey(connection)) {
            return;
        }
        long handle = nextHandle.getAndIncrement();
        if (handle == 0) {
            handle = nextHandle.getAndIncrement();
        }
        handles.put(connection, handle);
        connections.put(handle, connection);
        OpenCallback callback = openCallback;
        if (callback != null) {
            callback.invoke(handle);
        }
    }

    synchronized void closeConnection(GeyserConnection connection) {
        GeyserLiteVerifiedIngressProducer.forget(connection);
        Long handle = handles.remove(connection);
        if (handle != null) {
            connections.remove(handle);
            Assignment assignment = assignments.remove(handle);
            if (assignment != null) {
                cancelExpiry(assignment);
                correlations.remove(correlationKey(assignment.correlation));
            }
        }
    }

    void publishVerified(GeyserConnection connection, byte[] frame) {
        Long handle;
        synchronized (this) {
            handle = handles.get(connection);
        }
        if (handle != null) {
            publishVerified(handle, frame);
        }
    }

    boolean publishVerified(long handle, byte[] frame) {
        Assignment assignment = claimAssignment(handle, frame);
        if (assignment == null) {
            return false;
        }
        VerifiedCallback callback;
        synchronized (this) {
            callback = verifiedCallback;
        }
        if (callback == null) {
            return false;
        }
        try (CTypeConversion.CCharPointerHolder correlation = CTypeConversion.toCBytes(assignment.correlation);
             CTypeConversion.CCharPointerHolder payload = CTypeConversion.toCBytes(frame)) {
            callback.invoke(correlation.get(), payload.get(), frame.length, assignment.expiresUnixMs);
            return true;
        } catch (Throwable ignored) {
            return false;
        }
    }

    public synchronized void close() {
        expiryExecutor.shutdownNow();
        clearAssignments();
        for (Object connection : connections.values()) {
            if (connection instanceof GeyserConnection geyserConnection) {
                GeyserLiteVerifiedIngressProducer.forget(geyserConnection);
            }
        }
        connections.clear();
        handles.clear();
    }

    private int assign(long handle, byte[] correlation, long expiresUnixMs) {
        Assignment assignment;
        GeyserConnection connection;
        synchronized (this) {
            if (verifiedCallback == null) {
                return ASSIGN_WRONG_CONNECTION_STATE;
            }
            long now = System.currentTimeMillis();
            if (!connections.containsKey(handle)) {
                return ASSIGN_UNKNOWN_OR_CLOSED_HANDLE;
            }
            if (isZero(correlation) || expiresUnixMs <= now || expiresUnixMs - now > MAX_LIFETIME_MS) {
                return ASSIGN_INVALID_OR_EXPIRED_TIME;
            }
            if (assignments.containsKey(handle) || correlations.containsKey(correlationKey(correlation))) {
                return ASSIGN_DUPLICATE_HANDLE_OR_CORRELATION;
            }
            assignment = new Assignment(handle, correlation.clone(), expiresUnixMs);
            assignments.put(handle, assignment);
            correlations.put(correlationKey(correlation), handle);
            assignment.expiry = expiryExecutor.schedule(
                    () -> expire(handle, correlation, expiresUnixMs),
                    Math.max(1, expiresUnixMs - now), java.util.concurrent.TimeUnit.MILLISECONDS);
            connection = (GeyserConnection) connections.get(handle);
        }
        try {
            byte[] frame = GeyserLiteVerifiedIngressProducer.produce(connection, correlation, expiresUnixMs);
            if (frame == null || !publishVerified(handle, frame)) {
                removeAssignment(handle, assignment);
                return ASSIGN_WRONG_CONNECTION_STATE;
            }
            return ASSIGN_OK;
        } catch (Throwable ignored) {
            removeAssignment(handle, assignment);
            return ASSIGN_WRONG_CONNECTION_STATE;
        }
    }

    private synchronized void removeAssignment(long handle, Assignment assignment) {
        if (assignments.get(handle) == assignment) {
            assignments.remove(handle);
            cancelExpiry(assignment);
            correlations.remove(correlationKey(assignment.correlation));
        }
    }

    private synchronized Assignment claimAssignment(long handle, byte[] frame) {
        Assignment assignment = assignments.remove(handle);
        if (assignment == null) {
            return null;
        }
        cancelExpiry(assignment);
        correlations.remove(correlationKey(assignment.correlation));
        long now = System.currentTimeMillis();
        if (now >= assignment.expiresUnixMs || frame == null || frame.length < MIN_FRAME_BYTES
                || frame.length > MAX_FRAME_BYTES) {
            return null;
        }
        return assignment;
    }

    private synchronized void expire(long handle, byte[] correlation, long expiresUnixMs) {
        Assignment assignment = assignments.get(handle);
        if (assignment == null || assignment.expiresUnixMs != expiresUnixMs
                || !Arrays.equals(assignment.correlation, correlation)) {
            return;
        }
        assignments.remove(handle);
        correlations.remove(correlationKey(correlation));
    }

    private void clearAssignments() {
        for (Assignment assignment : assignments.values()) {
            cancelExpiry(assignment);
        }
        assignments.clear();
        correlations.clear();
    }

    private static void cancelExpiry(Assignment assignment) {
        if (assignment.expiry != null) {
            assignment.expiry.cancel(false);
        }
    }

    private static final class DaemonThreadFactory implements ThreadFactory {
        @Override
        public Thread newThread(Runnable runnable) {
            Thread thread = new Thread(runnable, "geyserlite-verified-ingress-expiry");
            thread.setDaemon(true);
            return thread;
        }
    }

    private static String correlationKey(byte[] correlation) {
        return Arrays.toString(correlation);
    }

    private static boolean isZero(byte[] value) {
        for (byte b : value) {
            if (b != 0) {
                return false;
            }
        }
        return true;
    }
}

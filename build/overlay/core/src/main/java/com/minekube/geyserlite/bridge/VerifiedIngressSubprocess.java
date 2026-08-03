/*
 * geyserlite — VerifiedIngressV1 subprocess transport.
 *
 * SPDX-License-Identifier: MIT
 */
package org.geysermc.geyser.util;

import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.io.IOException;
import java.security.GeneralSecurityException;
import java.security.MessageDigest;
import java.util.Arrays;
import java.util.HashMap;
import java.util.IdentityHashMap;
import java.util.Map;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.ThreadFactory;
import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;
import com.minekube.geyserlite.bridge.VerifiedIngressHooks;
import org.geysermc.geyser.api.connection.GeyserConnection;

public final class VerifiedIngressSubprocess implements VerifiedIngressHooks.Sink {
    private static final int VERSION = 1;
    private static final int ASSIGNMENT = 1;
    private static final int ASSIGNMENT_ACK = 2;
    private static final int VERIFIED_INGRESS = 3;
    private static final int CONNECTION_OPEN = 4;
    private static final int ACK_POSITIVE = 0;
    private static final int ACK_NEGATIVE = 1;
    private static final int BOOTSTRAP_BYTES = 41;
    private static final int KEY_BYTES = 32;
    private static final int MAC_BYTES = 32;
    private static final int MAX_PACKET_BYTES = 8192;
    private static final int CORRELATION_BYTES = 16;
    private static final int MAX_FRAME_BYTES = 4096;
    private static final long MAX_LIFETIME_MS = 5000;
    private static final int GENERATION_OFFSET = 2;
    private static final int SEQUENCE_OFFSET = 10;
    private static final int HANDLE_OFFSET = 18;
    private static final int CORRELATION_OFFSET = 26;
    private static final int EXPIRES_OFFSET = 42;
    private static final int ASSIGNMENT_MAC_OFFSET = 50;
    private static final int ASSIGNMENT_BYTES = 82;
    private static final int ACK_STATUS_OFFSET = 42;
    private static final int ACK_MAC_OFFSET = 43;
    private static final int ACK_BYTES = 75;
    private static final int PAYLOAD_OFFSET = 26;
    private static final int OPEN_MAC_OFFSET = 26;
    private static final int OPEN_BYTES = 58;

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

    public static void startIfPresent() {
        if (Boolean.getBoolean("geyserlite.embedded") || !InheritedSocket.isPresent(3)) {
            return;
        }
        VerifiedIngressSubprocess transport = new VerifiedIngressSubprocess();
        if (!transport.start()) {
            return;
        }
        VerifiedIngressHooks.register(transport);
    }

    private final Map<Long, GeyserConnection> connections = new HashMap<>();
    private final Map<GeyserConnection, Long> handles = new IdentityHashMap<>();
    private final Map<Long, Assignment> assignments = new HashMap<>();
    private final Map<String, Long> correlations = new HashMap<>();
    private final ScheduledExecutorService expiryExecutor = Executors.newSingleThreadScheduledExecutor(new DaemonThreadFactory());
    private long nextHandle = 1;
    private long generation;
    private long readSequence;
    private long writeSequence;
    private byte[] key;
    private FileInputStream input;
    private FileOutputStream output;
    private boolean running;

    private boolean start() {
        try {
            input = InheritedSocket.openInput(3);
        } catch (IOException e) {
            return false;
        }
        try {
            output = new FileOutputStream(input.getFD());
            byte[] bootstrap = read(BOOTSTRAP_BYTES);
            if (bootstrap == null) {
                closeTransport();
                return false;
            }
            if (bootstrap.length != BOOTSTRAP_BYTES || bootstrap[0] != VERSION) {
                throw new IOException("invalid verified ingress bootstrap");
            }
            generation = readLong(bootstrap, 1);
            if (generation == 0) {
                throw new IOException("invalid verified ingress generation");
            }
            key = Arrays.copyOfRange(bootstrap, 9, BOOTSTRAP_BYTES);
            readSequence = 1;
            writeSequence = 1;
            running = true;
            Thread reader = new Thread(this::readLoop, "geyserlite-verified-ingress");
            reader.setDaemon(true);
            reader.start();
            return true;
        } catch (IOException e) {
            closeTransport();
            throw new IllegalStateException("verified ingress bootstrap failed", e);
        }
    }

    @Override
    public void onConnectionOpened(GeyserConnection connection) {
        open(connection);
    }

    private synchronized void open(GeyserConnection connection) {
        if (!running || handles.containsKey(connection)) {
            return;
        }
        long handle = nextHandle++;
        if (handle == 0) {
            handle = nextHandle++;
        }
        handles.put(connection, handle);
        connections.put(handle, connection);
        try {
            byte[] packet = prefix(CONNECTION_OPEN, handle, OPEN_BYTES);
            sign(packet, OPEN_MAC_OFFSET);
            write(packet);
        } catch (IOException | GeneralSecurityException e) {
            closeTransport();
        }
    }

    @Override
    public void onConnectionClosed(GeyserConnection connection) {
        close(connection);
    }

    private synchronized void close(GeyserConnection connection) {
        GeyserLiteVerifiedIngressProducer.forget(connection);
        Long handle = handles.remove(connection);
        if (handle == null) {
            return;
        }
        connections.remove(handle);
        Assignment assignment = assignments.remove(handle);
        if (assignment != null) {
            cancelExpiry(assignment);
            correlations.remove(correlationKey(assignment.correlation));
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
        try {
            writeVerified(handle, frame);
            return true;
        } catch (Throwable e) {
            closeTransport();
            return false;
        }
    }

    private void readLoop() {
        try {
            while (running) {
                byte[] packet = read(MAX_PACKET_BYTES);
                if (packet == null) {
                    throw new IOException("verified ingress EOF");
                }
                readAssignment(packet);
            }
        } catch (IOException | GeneralSecurityException e) {
            closeTransport();
        }
    }

    private byte[] read(int capacity) throws IOException {
        byte[] packet = new byte[capacity];
        int length = input.read(packet);
        return length < 0 ? null : Arrays.copyOf(packet, length);
    }

    private void readAssignment(byte[] packet) throws IOException, GeneralSecurityException {
        if (packet.length != ASSIGNMENT_BYTES || packet[0] != VERSION || packet[1] != ASSIGNMENT
                || readLong(packet, GENERATION_OFFSET) != generation
                || readLong(packet, SEQUENCE_OFFSET) != readSequence || readLong(packet, HANDLE_OFFSET) == 0) {
            throw new IOException("invalid verified ingress assignment");
        }
        verify(packet, ASSIGNMENT_MAC_OFFSET);
        readSequence++;
        long handle = readLong(packet, HANDLE_OFFSET);
        byte[] correlation = Arrays.copyOfRange(packet, CORRELATION_OFFSET, EXPIRES_OFFSET);
        long expires = readLong(packet, EXPIRES_OFFSET);
        int status = accept(handle, correlation, expires) ? ACK_POSITIVE : ACK_NEGATIVE;
        writeAck(handle, correlation, status);
        if (status == ACK_NEGATIVE) {
            throw new IOException("verified ingress assignment rejected");
        }
        byte[] frame = emitVerified(handle, correlation, expires);
        if (frame == null || !publishVerified(handle, frame)) {
            clearAssignment(handle, correlation, expires);
            throw new IOException("verified ingress verification rejected");
        }
    }

    private byte[] emitVerified(long handle, byte[] correlation, long expires)
            throws IOException {
        long remaining = expires - System.currentTimeMillis();
        if (remaining <= 0 || remaining > MAX_LIFETIME_MS) {
            return null;
        }
        byte[] frame;
        try {
            GeyserConnection connection;
            synchronized (this) {
                connection = connections.get(handle);
            }
            if (connection == null) {
                return null;
            }
            frame = GeyserLiteVerifiedIngressProducer.produce(connection, correlation, expires);
        } catch (Throwable e) {
            throw new IOException("verified ingress verification failed", e);
        }
        return frame;
    }

    private synchronized boolean accept(long handle, byte[] correlation, long expires) {
        long now = System.currentTimeMillis();
        if (!connections.containsKey(handle) || isZero(correlation) || expires <= now
                || expires - now > MAX_LIFETIME_MS || assignments.containsKey(handle)
                || correlations.containsKey(correlationKey(correlation))) {
            return false;
        }
        Assignment assignment = new Assignment(handle, correlation.clone(), expires);
        assignments.put(handle, assignment);
        correlations.put(correlationKey(correlation), handle);
        assignment.expiry = expiryExecutor.schedule(
                () -> expire(handle, correlation, expires),
                Math.max(1, expires - now), java.util.concurrent.TimeUnit.MILLISECONDS);
        return true;
    }

    private synchronized void expire(long handle, byte[] correlation, long expires) {
        Assignment assignment = assignments.get(handle);
        if (assignment == null || assignment.expiresUnixMs != expires
                || !Arrays.equals(assignment.correlation, correlation)) {
            return;
        }
        assignments.remove(handle);
        correlations.remove(correlationKey(correlation));
    }

    private synchronized void writeAck(long handle, byte[] correlation, int status)
            throws IOException, GeneralSecurityException {
        byte[] packet = prefix(ASSIGNMENT_ACK, handle, ACK_BYTES);
        System.arraycopy(correlation, 0, packet, CORRELATION_OFFSET, CORRELATION_BYTES);
        packet[ACK_STATUS_OFFSET] = (byte) status;
        sign(packet, ACK_MAC_OFFSET);
        write(packet);
    }

    private synchronized void writeVerified(long handle, byte[] frame)
            throws IOException, GeneralSecurityException {
        byte[] packet = prefix(VERIFIED_INGRESS, handle, PAYLOAD_OFFSET + frame.length + MAC_BYTES);
        System.arraycopy(frame, 0, packet, PAYLOAD_OFFSET, frame.length);
        sign(packet, packet.length - MAC_BYTES);
        write(packet);
    }

    private synchronized Assignment claimAssignment(long handle, byte[] frame) {
        Assignment assignment = assignments.remove(handle);
        if (assignment == null) {
            return null;
        }
        cancelExpiry(assignment);
        correlations.remove(correlationKey(assignment.correlation));
        if (!running || System.currentTimeMillis() >= assignment.expiresUnixMs || frame == null
                || frame.length < 1 || frame.length > MAX_FRAME_BYTES) {
            return null;
        }
        return assignment;
    }

    private synchronized void clearAssignment(long handle, byte[] correlation, long expires) {
        Assignment assignment = assignments.get(handle);
        if (assignment == null || assignment.expiresUnixMs != expires
                || !Arrays.equals(assignment.correlation, correlation)) {
            return;
        }
        assignments.remove(handle);
        cancelExpiry(assignment);
        correlations.remove(correlationKey(assignment.correlation));
    }

    private byte[] prefix(int type, long handle, int length) {
        byte[] packet = new byte[length];
        packet[0] = VERSION;
        packet[1] = (byte) type;
        writeLong(packet, GENERATION_OFFSET, generation);
        writeLong(packet, SEQUENCE_OFFSET, writeSequence++);
        writeLong(packet, HANDLE_OFFSET, handle);
        return packet;
    }

    private void write(byte[] packet) throws IOException {
        if (!running || output == null) {
            throw new IOException("verified ingress transport is closed");
        }
        output.write(packet);
        output.flush();
    }

    private void verify(byte[] packet, int macOffset) throws IOException, GeneralSecurityException {
        byte[] expected = hmac(packet, macOffset);
        if (!MessageDigest.isEqual(expected, Arrays.copyOfRange(packet, macOffset, packet.length))) {
            throw new IOException("verified ingress authentication failed");
        }
    }

    private void sign(byte[] packet, int macOffset) throws GeneralSecurityException {
        System.arraycopy(hmac(packet, macOffset), 0, packet, macOffset, MAC_BYTES);
    }

    private byte[] hmac(byte[] packet, int macOffset) throws GeneralSecurityException {
        if (key == null || key.length != KEY_BYTES) {
            throw new GeneralSecurityException("verified ingress key unavailable");
        }
        Mac mac = Mac.getInstance("HmacSHA256");
        mac.init(new SecretKeySpec(key, "HmacSHA256"));
        return mac.doFinal(Arrays.copyOf(packet, macOffset));
    }

    private synchronized void closeTransport() {
        running = false;
        VerifiedIngressHooks.unregister(this);
        expiryExecutor.shutdownNow();
        for (Assignment assignment : assignments.values()) {
            cancelExpiry(assignment);
        }
        assignments.clear();
        correlations.clear();
        for (GeyserConnection connection : connections.values()) {
            GeyserLiteVerifiedIngressProducer.forget(connection);
        }
        connections.clear();
        handles.clear();
        if (key != null) {
            Arrays.fill(key, (byte) 0);
            key = null;
        }
        if (input != null) {
            try {
                input.close();
            } catch (IOException ignored) {
            }
        }
        input = null;
        output = null;
    }

    private static String correlationKey(byte[] correlation) {
        return Arrays.toString(correlation);
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

    private static boolean isZero(byte[] value) {
        for (byte b : value) {
            if (b != 0) {
                return false;
            }
        }
        return true;
    }

    private static long readLong(byte[] value, int offset) {
        long result = 0;
        for (int i = 0; i < Long.BYTES; i++) {
            result = (result << 8) | (value[offset + i] & 0xffL);
        }
        return result;
    }

    private static void writeLong(byte[] value, int offset, long number) {
        for (int i = Long.BYTES - 1; i >= 0; i--) {
            value[offset + i] = (byte) number;
            number >>>= 8;
        }
    }
}

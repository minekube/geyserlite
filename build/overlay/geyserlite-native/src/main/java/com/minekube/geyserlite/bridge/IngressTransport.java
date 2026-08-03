/*
 * geyserlite — VerifiedIngressV1 transport.
 *
 * SPDX-License-Identifier: MIT
 */
package com.minekube.geyserlite.bridge;

import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.io.IOException;
import java.security.GeneralSecurityException;
import java.security.MessageDigest;
import java.util.Arrays;
import java.util.HashMap;
import java.util.IdentityHashMap;
import java.util.Map;
import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.ThreadFactory;
import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;
import org.geysermc.geyser.GeyserImpl;
import org.geysermc.geyser.api.connection.GeyserConnection;
import org.geysermc.geyser.api.event.bedrock.SessionInitializeEvent;
import org.geysermc.geyser.event.type.SessionDisconnectEventImpl;
import org.graalvm.nativeimage.c.function.CFunctionPointer;
import org.graalvm.nativeimage.c.function.InvokeCFunctionPointer;
import org.graalvm.nativeimage.c.type.CCharPointer;
import org.graalvm.nativeimage.c.type.CTypeConversion;

public final class IngressTransport {
    static final int CALLBACK_REGISTRATION_OK = 0;
    static final int ASSIGN_OK = 0;
    static final int ASSIGN_UNKNOWN_OR_CLOSED_HANDLE = -1;
    static final int ASSIGN_DUPLICATE_HANDLE_OR_CORRELATION = -2;
    static final int ASSIGN_INVALID_OR_EXPIRED_TIME = -3;
    static final int ASSIGN_WRONG_CONNECTION_STATE = -4;

    static final int CORRELATION_BYTES = 16;
    static final int MIN_FRAME_BYTES = 1;
    static final int MAX_FRAME_BYTES = 4096;
    static final long MAX_LIFETIME_MS = 5000;

    static final int VERSION = 1;
    static final int ASSIGNMENT = 1;
    static final int ASSIGNMENT_ACK = 2;
    static final int VERIFIED_INGRESS = 3;
    static final int CONNECTION_OPEN = 4;
    static final int ACK_POSITIVE = 0;
    static final int ACK_NEGATIVE = 1;
    static final int BOOTSTRAP_BYTES = 41;
    static final int MAC_BYTES = 32;
    static final int MAX_PACKET_BYTES = 8192;
    static final int VERSION_OFFSET = 0;
    static final int TYPE_OFFSET = 1;
    static final int GENERATION_OFFSET = 2;
    static final int SEQUENCE_OFFSET = 10;
    static final int HANDLE_OFFSET = 18;
    static final int CORRELATION_OFFSET = 26;
    static final int EXPIRES_OFFSET = 42;
    static final int ASSIGNMENT_MAC_OFFSET = 50;
    static final int ASSIGNMENT_BYTES = 82;
    static final int ACK_STATUS_OFFSET = 42;
    static final int ACK_MAC_OFFSET = 43;
    static final int ACK_BYTES = 75;
    static final int PAYLOAD_OFFSET = 26;
    static final int OPEN_MAC_OFFSET = 26;
    static final int OPEN_BYTES = 58;

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
    private FileInputStream subprocessInput;
    private FileOutputStream subprocessOutput;
    private Thread subprocessReader;
    private byte[] subprocessKey;
    private long subprocessGeneration;
    private long subprocessReadSequence;
    private long subprocessWriteSequence;
    private boolean subprocessRunning;
    private boolean installed;

    synchronized int setCallbacks(OpenCallback open, VerifiedCallback verified) {
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

    synchronized int assign(long handle, CCharPointer correlationPointer, long expiresUnixMs) {
        if (correlationPointer == null) {
            return ASSIGN_WRONG_CONNECTION_STATE;
        }
        byte[] correlation = new byte[CORRELATION_BYTES];
        for (int i = 0; i < correlation.length; i++) {
            correlation[i] = correlationPointer.read(i);
        }
        return assign(handle, correlation, expiresUnixMs);
    }

    synchronized void install(GeyserImpl geyser) {
        if (installed) {
            return;
        }
        installed = true;
        geyser.eventBus().subscribe(geyser, SessionInitializeEvent.class,
                event -> openConnection(event.connection()));
        geyser.eventBus().subscribe(geyser, SessionDisconnectEventImpl.class,
                event -> closeConnection(event.connection()));
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
        if (subprocessRunning) {
            try {
                writeOpen(handle);
            } catch (IOException | GeneralSecurityException e) {
                closeSubprocess(e);
            }
            return;
        }
        OpenCallback callback = openCallback;
        if (callback != null) {
            callback.invoke(handle);
        }
    }

    synchronized void closeConnection(GeyserConnection connection) {
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

    synchronized void publishVerified(GeyserConnection connection, byte[] frame) {
        Long handle = handles.get(connection);
        if (handle != null) {
            publishVerified(handle, frame);
        }
    }

    synchronized void publishVerified(long handle, byte[] frame) {
        Assignment assignment = assignments.get(handle);
        long now = System.currentTimeMillis();
        if (assignment == null || now >= assignment.expiresUnixMs || frame == null
                || frame.length < MIN_FRAME_BYTES || frame.length > MAX_FRAME_BYTES) {
            return;
        }
        assignments.remove(handle);
        cancelExpiry(assignment);
        correlations.remove(correlationKey(assignment.correlation));
        if (subprocessRunning) {
            try {
                writeVerified(handle, frame);
            } catch (IOException | GeneralSecurityException e) {
                closeSubprocess(e);
            }
            return;
        }
        VerifiedCallback callback = verifiedCallback;
        if (callback == null) {
            return;
        }
        try (CTypeConversion.CCharPointerHolder correlation = CTypeConversion.toCBytes(assignment.correlation);
             CTypeConversion.CCharPointerHolder payload = CTypeConversion.toCBytes(frame)) {
            callback.invoke(correlation.get(), payload.get(), frame.length, assignment.expiresUnixMs);
        }
    }

    synchronized void startSubprocess(int fd) {
        if (Boolean.getBoolean(GeyserBridge.EMBED_PROP)) {
            return;
        }
        try {
            subprocessInput = new FileInputStream("/proc/self/fd/" + fd);
            subprocessOutput = new FileOutputStream(subprocessInput.getFD());
            subprocessRunning = true;
            subprocessReader = new Thread(this::readSubprocess, "geyserlite-verified-ingress");
            subprocessReader.setDaemon(true);
            subprocessReader.start();
        } catch (IOException e) {
            subprocessInput = null;
            subprocessOutput = null;
        }
    }

    synchronized void close() {
        subprocessRunning = false;
        expiryExecutor.shutdownNow();
        clearAssignments();
        if (subprocessKey != null) {
            Arrays.fill(subprocessKey, (byte) 0);
            subprocessKey = null;
        }
        if (subprocessInput != null) {
            try {
                subprocessInput.close();
            } catch (IOException ignored) {
            }
        }
        subprocessInput = null;
        subprocessOutput = null;
        subprocessReader = null;
    }

    private int assign(long handle, byte[] correlation, long expiresUnixMs) {
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
        Assignment assignment = new Assignment(handle, correlation.clone(), expiresUnixMs);
        assignments.put(handle, assignment);
        correlations.put(correlationKey(correlation), handle);
        assignment.expiry = expiryExecutor.schedule(
                () -> expire(handle, correlation, expiresUnixMs),
                Math.max(1, expiresUnixMs - now), java.util.concurrent.TimeUnit.MILLISECONDS);
        return ASSIGN_OK;
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

    private void readSubprocess() {
        try {
            byte[] bootstrap = readPacket(BOOTSTRAP_BYTES);
            if (bootstrap == null || bootstrap.length != BOOTSTRAP_BYTES || bootstrap[VERSION_OFFSET] != VERSION) {
                throw new IOException("invalid verified ingress bootstrap");
            }
            subprocessGeneration = readLong(bootstrap, 1);
            if (subprocessGeneration == 0) {
                throw new IOException("invalid verified ingress generation");
            }
            subprocessKey = Arrays.copyOfRange(bootstrap, 9, BOOTSTRAP_BYTES);
            subprocessReadSequence = 1;
            subprocessWriteSequence = 1;
            while (subprocessRunning) {
                byte[] packet = readPacket(MAX_PACKET_BYTES);
                if (packet == null) {
                    throw new IOException("verified ingress EOF");
                }
                readAssignment(packet);
            }
        } catch (IOException | GeneralSecurityException e) {
            closeSubprocess(e);
        }
    }

    private byte[] readPacket(int capacity) throws IOException {
        byte[] packet = new byte[capacity];
        int length = subprocessInput.read(packet);
        if (length < 0) {
            return null;
        }
        return Arrays.copyOf(packet, length);
    }

    private void readAssignment(byte[] packet) throws IOException, GeneralSecurityException {
        if (packet.length != ASSIGNMENT_BYTES || packet[VERSION_OFFSET] != VERSION
                || packet[TYPE_OFFSET] != ASSIGNMENT || readLong(packet, GENERATION_OFFSET) != subprocessGeneration
                || readLong(packet, SEQUENCE_OFFSET) != subprocessReadSequence || readLong(packet, HANDLE_OFFSET) == 0) {
            throw new IOException("invalid verified ingress assignment");
        }
        verify(packet, ASSIGNMENT_MAC_OFFSET);
        subprocessReadSequence++;
        long handle = readLong(packet, HANDLE_OFFSET);
        byte[] correlation = Arrays.copyOfRange(packet, CORRELATION_OFFSET, EXPIRES_OFFSET);
        long expiresUnixMs = readLong(packet, EXPIRES_OFFSET);
        int status = assign(handle, correlation, expiresUnixMs) == ASSIGN_OK ? ACK_POSITIVE : ACK_NEGATIVE;
        writeAck(handle, correlation, status);
        if (status == ACK_NEGATIVE) {
            throw new IOException("verified ingress assignment rejected");
        }
    }

    private void writeOpen(long handle) throws IOException, GeneralSecurityException {
        byte[] packet = prefix(CONNECTION_OPEN, handle, OPEN_BYTES);
        sign(packet, OPEN_MAC_OFFSET);
        writePacket(packet);
    }

    private void writeAck(long handle, byte[] correlation, int status) throws IOException, GeneralSecurityException {
        byte[] packet = prefix(ASSIGNMENT_ACK, handle, ACK_BYTES);
        System.arraycopy(correlation, 0, packet, CORRELATION_OFFSET, CORRELATION_BYTES);
        packet[ACK_STATUS_OFFSET] = (byte) status;
        sign(packet, ACK_MAC_OFFSET);
        writePacket(packet);
    }

    private void writeVerified(long handle, byte[] frame) throws IOException, GeneralSecurityException {
        byte[] packet = prefix(VERIFIED_INGRESS, handle, PAYLOAD_OFFSET + frame.length + MAC_BYTES);
        System.arraycopy(frame, 0, packet, PAYLOAD_OFFSET, frame.length);
        sign(packet, packet.length - MAC_BYTES);
        writePacket(packet);
    }

    private byte[] prefix(int type, long handle, int length) {
        byte[] packet = new byte[length];
        packet[VERSION_OFFSET] = VERSION;
        packet[TYPE_OFFSET] = (byte) type;
        writeLong(packet, GENERATION_OFFSET, subprocessGeneration);
        writeLong(packet, SEQUENCE_OFFSET, subprocessWriteSequence++);
        writeLong(packet, HANDLE_OFFSET, handle);
        return packet;
    }

    private void writePacket(byte[] packet) throws IOException {
        subprocessOutput.write(packet);
        subprocessOutput.flush();
    }

    private void verify(byte[] packet, int macOffset) throws GeneralSecurityException, IOException {
        if (subprocessKey == null) {
            throw new IOException("verified ingress key unavailable");
        }
        byte[] expected = hmac(packet, macOffset);
        if (!MessageDigest.isEqual(expected, Arrays.copyOfRange(packet, macOffset, packet.length))) {
            throw new IOException("verified ingress authentication failed");
        }
    }

    private void sign(byte[] packet, int macOffset) throws GeneralSecurityException {
        byte[] mac = hmac(packet, macOffset);
        System.arraycopy(mac, 0, packet, macOffset, MAC_BYTES);
    }

    private byte[] hmac(byte[] packet, int macOffset) throws GeneralSecurityException {
        Mac mac = Mac.getInstance("HmacSHA256");
        mac.init(new SecretKeySpec(subprocessKey, "HmacSHA256"));
        return mac.doFinal(Arrays.copyOf(packet, macOffset));
    }

    private void closeSubprocess(Exception reason) {
        synchronized (this) {
            subprocessRunning = false;
            clearAssignments();
            if (subprocessKey != null) {
                Arrays.fill(subprocessKey, (byte) 0);
                subprocessKey = null;
            }
            if (subprocessInput != null) {
                try {
                    subprocessInput.close();
                } catch (IOException ignored) {
                }
            }
            subprocessInput = null;
            subprocessOutput = null;
        }
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

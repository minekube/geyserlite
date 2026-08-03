/*
 * geyserlite — pinned Geyser raw-ingress producer.
 *
 * SPDX-License-Identifier: MIT
 */
package org.geysermc.geyser.util;

import java.io.ByteArrayOutputStream;
import java.nio.charset.StandardCharsets;
import java.util.IdentityHashMap;
import java.util.Map;
import org.geysermc.geyser.api.connection.GeyserConnection;
import org.geysermc.geyser.session.GeyserSession;

final class GeyserLiteVerifiedIngressProducer {
    private static final int CORRELATION_BYTES = 16;
    private static final int MAX_FRAME_BYTES = 4096;
    private static final long MAX_LIFETIME_MS = 5000;
    private static final String SOURCE_PROTOCOL = "bedrock";

    private static final Map<GeyserConnection, Principal> authenticated = new IdentityHashMap<>();

    private GeyserLiteVerifiedIngressProducer() {}

    static synchronized void authenticated(GeyserSession session, String verificationMethod, boolean signed) {
        if (!signed) {
            return;
        }
        try {
            String xuid = session.xuid();
            int protocolVersion = session.protocolVersion();
            if (!isCanonicalXuid(xuid) || protocolVersion <= 0 || !isVerificationMethod(verificationMethod)) {
                return;
            }
            authenticated.put(session, new Principal(
                    xuid,
                    session.bedrockUsername(),
                    protocolVersion,
                    verificationMethod,
                    System.currentTimeMillis()));
        } catch (RuntimeException ignored) {
        }
    }

    static synchronized void forget(GeyserConnection connection) {
        authenticated.remove(connection);
    }

    static byte[] produce(GeyserConnection connection, byte[] correlation, long expiresUnixMs) {
        if (correlation == null || correlation.length != CORRELATION_BYTES || isZero(correlation)) {
            return null;
        }
        long now = System.currentTimeMillis();
        if (expiresUnixMs <= now || expiresUnixMs - now > MAX_LIFETIME_MS) {
            return null;
        }
        Principal principal;
        synchronized (GeyserLiteVerifiedIngressProducer.class) {
            principal = authenticated.get(connection);
        }
        if (principal == null) {
            return null;
        }
        ByteArrayOutputStream frame = new ByteArrayOutputStream();
        writeVarintField(frame, 1, 1);
        writeBytesField(frame, 2, correlation);
        writeStringField(frame, 3, principal.xuid);
        writeStringField(frame, 4, principal.displayName);
        writeStringField(frame, 5, SOURCE_PROTOCOL);
        writeVarintField(frame, 6, principal.protocolVersion);
        writeStringField(frame, 7, principal.verificationMethod);
        writeVarintField(frame, 8, principal.verifiedAtUnixMs);
        byte[] result = frame.toByteArray();
        return result.length <= MAX_FRAME_BYTES ? result : null;
    }

    private static boolean isCanonicalXuid(String xuid) {
        return xuid != null && xuid.matches("[1-9][0-9]{0,18}");
    }

    private static boolean isZero(byte[] value) {
        for (byte current : value) {
            if (current != 0) {
                return false;
            }
        }
        return true;
    }

    private static boolean isVerificationMethod(String method) {
        return "minecraft_legacy_chain+client_jwt+ecdh_v1".equals(method)
                || "minecraft_full_jwks+client_jwt+ecdh_v1".equals(method);
    }

    private static void writeStringField(ByteArrayOutputStream output, int field, String value) {
        if (value == null || value.isEmpty()) {
            return;
        }
        writeBytesField(output, field, value.getBytes(StandardCharsets.UTF_8));
    }

    private static void writeBytesField(ByteArrayOutputStream output, int field, byte[] value) {
        writeVarint(output, ((long) field << 3) | 2);
        writeVarint(output, value.length);
        output.writeBytes(value);
    }

    private static void writeVarintField(ByteArrayOutputStream output, int field, long value) {
        writeVarint(output, (long) field << 3);
        writeVarint(output, value);
    }

    private static void writeVarint(ByteArrayOutputStream output, long value) {
        while ((value & ~0x7fL) != 0) {
            output.write((int) ((value & 0x7f) | 0x80));
            value >>>= 7;
        }
        output.write((int) value);
    }

    private record Principal(String xuid, String displayName, int protocolVersion,
                             String verificationMethod, long verifiedAtUnixMs) {}
}

/*
 * SPDX-License-Identifier: MIT
 */
package org.geysermc.geyser.util;

import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertTrue;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.when;

import java.util.Arrays;
import org.geysermc.geyser.session.GeyserSession;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Test;

class GeyserLiteVerifiedIngressProducerTest {
    private static final String VERIFICATION_METHOD = "minecraft_full_jwks+client_jwt+ecdh_v1";

    private GeyserSession session;

    @AfterEach
    void clearAuthentication() {
        if (session != null) {
            GeyserLiteVerifiedIngressProducer.forget(session);
        }
    }

    @Test
    void unsignedAuthenticationCannotProduceVerifiedIdentity() {
        session = authenticatedSession();
        byte[] correlation = correlation();

        LoginEncryptionUtils.recordVerifiedIngress(session, VERIFICATION_METHOD, false);

        assertNull(GeyserLiteVerifiedIngressProducer.produce(session, correlation,
                System.currentTimeMillis() + 1000));
    }

    @Test
    void signedAuthenticationProducesCorrelatedOpaqueFrame() {
        session = authenticatedSession();
        byte[] correlation = {1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16};

        LoginEncryptionUtils.recordVerifiedIngress(session, VERIFICATION_METHOD, true);
        byte[] frame = GeyserLiteVerifiedIngressProducer.produce(session, correlation,
                System.currentTimeMillis() + 1000);

        assertNotNull(frame);
        assertTrue(indexOf(frame, correlation) >= 0);
    }

    @Test
    void expiredOrForgottenAuthenticationCannotBeReused() {
        session = authenticatedSession();
        byte[] correlation = {1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16};

        LoginEncryptionUtils.recordVerifiedIngress(session, VERIFICATION_METHOD, true);
        assertNull(GeyserLiteVerifiedIngressProducer.produce(session, correlation,
                System.currentTimeMillis() - 1));

        GeyserLiteVerifiedIngressProducer.forget(session);
        assertNull(GeyserLiteVerifiedIngressProducer.produce(session, correlation,
                System.currentTimeMillis() + 1000));
    }

    private static GeyserSession authenticatedSession() {
        GeyserSession session = mock(GeyserSession.class);
        when(session.xuid()).thenReturn("123456789");
        when(session.protocolVersion()).thenReturn(776);
        when(session.bedrockUsername()).thenReturn("player");
        return session;
    }

    private static byte[] correlation() {
        return new byte[] {1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16};
    }

    private static int indexOf(byte[] value, byte[] needle) {
        for (int offset = 0; offset <= value.length - needle.length; offset++) {
            if (Arrays.equals(value, offset, offset + needle.length, needle, 0, needle.length)) {
                return offset;
            }
        }
        return -1;
    }
}

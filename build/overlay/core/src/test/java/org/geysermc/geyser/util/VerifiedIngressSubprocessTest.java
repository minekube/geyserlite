/*
 * SPDX-License-Identifier: MIT
 */
package org.geysermc.geyser.util;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertDoesNotThrow;
import static org.junit.jupiter.api.Assumptions.assumeTrue;

import com.minekube.geyserlite.bridge.VerifiedIngressHooks;
import org.geysermc.geyser.api.connection.GeyserConnection;
import org.junit.jupiter.api.Test;

class VerifiedIngressSubprocessTest {

    @Test
    void startIfPresentIsANoOpWithoutParentMarker() {
        assumeTrue(System.getenv(VerifiedIngressSubprocess.FD_ENV) == null);
        // The test JVM routinely inherits an unrelated descriptor 3; without
        // the parent-supplied marker the transport must never adopt it.
        assertDoesNotThrow(VerifiedIngressSubprocess::startIfPresent);
        VerifiedIngressHooks.Sink probe = new VerifiedIngressHooks.Sink() {
            @Override
            public void onConnectionOpened(GeyserConnection connection) {
            }

            @Override
            public void onConnectionClosed(GeyserConnection connection) {
            }
        };
        assertDoesNotThrow(() -> VerifiedIngressHooks.register(probe));
        VerifiedIngressHooks.unregister(probe);
    }

    @Test
    void inheritedDescriptorRequiresExplicitNumericMarker() {
        assertEquals(-1, VerifiedIngressSubprocess.inheritedDescriptor(null));
        assertEquals(-1, VerifiedIngressSubprocess.inheritedDescriptor(""));
        assertEquals(-1, VerifiedIngressSubprocess.inheritedDescriptor("socket"));
        assertEquals(-1, VerifiedIngressSubprocess.inheritedDescriptor("-3"));
        assertEquals(3, VerifiedIngressSubprocess.inheritedDescriptor("3"));
    }

    @Test
    void inheritedDescriptorIsDisabledInEmbeddedMode() {
        System.setProperty("geyserlite.embedded", "true");
        try {
            assertEquals(-1, VerifiedIngressSubprocess.inheritedDescriptor("3"));
        } finally {
            System.clearProperty("geyserlite.embedded");
        }
    }
}

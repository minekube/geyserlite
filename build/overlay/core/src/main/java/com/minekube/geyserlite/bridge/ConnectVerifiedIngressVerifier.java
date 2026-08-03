/*
 * geyserlite — Connect-owned VerifiedIngressV1 producer hook.
 *
 * SPDX-License-Identifier: MIT
 */
package com.minekube.geyserlite.bridge;

import org.geysermc.geyser.api.connection.GeyserConnection;

@FunctionalInterface
public interface ConnectVerifiedIngressVerifier {
    byte[] verify(GeyserConnection connection, byte[] correlation, long expiresUnixMs);
}

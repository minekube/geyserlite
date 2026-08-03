/*
 * geyserlite — Connect-owned VerifiedIngressV1 producer hook.
 *
 * SPDX-License-Identifier: MIT
 */
package com.minekube.geyserlite.bridge;

@FunctionalInterface
public interface ConnectVerifiedIngressVerifier {
    void verify(VerifiedIngressHooks.ConnectVerifierHandle handle,
                VerifiedIngressHooks.VerificationHandoff handoff);
}

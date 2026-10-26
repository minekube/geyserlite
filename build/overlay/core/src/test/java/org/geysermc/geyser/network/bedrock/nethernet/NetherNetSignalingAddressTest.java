package org.geysermc.geyser.network.bedrock.nethernet;

import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.assertEquals;

class NetherNetSignalingAddressTest {
    private final String previous = System.getProperty("geyserSignalingAddress");

    @AfterEach
    void restoreProperty() {
        if (previous == null) {
            System.clearProperty("geyserSignalingAddress");
        } else {
            System.setProperty("geyserSignalingAddress", previous);
        }
    }

    @Test
    void keepsConfiguredAddressByDefault() {
        System.clearProperty("geyserSignalingAddress");
        assertEquals("192.0.2.1", NetherNetServer.signalingAddress("192.0.2.1", null));
        assertEquals("192.0.2.1", NetherNetServer.signalingAddress("192.0.2.1", " "));
    }

    @Test
    void allowsTcpWildcardWithoutChangingUdpBind() {
        System.setProperty("geyserSignalingAddress", "0.0.0.0");
        assertEquals("0.0.0.0", NetherNetServer.signalingAddress("fly-global-services", "192.0.2.1"));
    }

    @Test
    void embeddedModeCanUseHostingEnvironment() {
        System.clearProperty("geyserSignalingAddress");
        assertEquals("0.0.0.0", NetherNetServer.signalingAddress("fly-global-services", "0.0.0.0"));
    }
}

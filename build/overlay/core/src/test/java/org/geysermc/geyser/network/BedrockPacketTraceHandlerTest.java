/*
 * Copyright (c) 2026 Minekube.
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in
 * all copies or substantial portions of the Software.
 */

package org.geysermc.geyser.network;

import io.netty.buffer.Unpooled;
import org.cloudburstmc.protocol.bedrock.netty.BedrockPacketWrapper;
import org.cloudburstmc.protocol.bedrock.packet.LoginPacket;
import org.junit.jupiter.api.Test;

import java.lang.reflect.Method;

import static org.junit.jupiter.api.Assertions.assertEquals;

class BedrockPacketTraceHandlerTest {
    @Test
    void describesDecodedPacketsWithoutPayloads() throws ReflectiveOperationException {
        LoginPacket packet = new LoginPacket();
        packet.setClientJwt("sensitive-client-jwt");

        assertEquals(
                "packetType=LoginPacket packetId=unknown flags=unknown size=unknown",
                describe(packet)
        );
    }

    @Test
    void describesPacketWrappersWithSafeMetadataOnly() throws ReflectiveOperationException {
        LoginPacket packet = new LoginPacket();
        packet.setClientJwt("sensitive-client-jwt");
        BedrockPacketWrapper wrapper = BedrockPacketWrapper.create(
                1, 0, 0, packet, Unpooled.wrappedBuffer(new byte[] { 0x01, 0x02, 0x03 })
        );

        try {
            assertEquals(
                    "packetType=LoginPacket packetId=1 flags={} size=3",
                    describe(wrapper)
            );
        } finally {
            wrapper.release();
        }
    }

    private static String describe(Object message) throws ReflectiveOperationException {
        Method method = BedrockPacketTraceHandler.class.getDeclaredMethod("describe", Object.class);
        method.setAccessible(true);
        return (String) method.invoke(null, message);
    }
}

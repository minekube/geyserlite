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

package org.geysermc.geyser.network.netty;

import io.netty.buffer.Unpooled;
import io.netty.channel.embedded.EmbeddedChannel;
import io.netty.channel.socket.DatagramPacket;
import org.junit.jupiter.api.Test;

import java.net.InetSocketAddress;

import static org.junit.jupiter.api.Assertions.assertTrue;

class BedrockRawDatagramTraceHandlerTest {
    @Test
    void dumpSummaryIncludesTopRawRemotesForAddressMismatches() {
        InetSocketAddress requestedRemote = new InetSocketAddress("203.0.113.10", 19132);
        InetSocketAddress outboundOnlyRemote = new InetSocketAddress("198.51.100.20", 19132);
        EmbeddedChannel channel = new EmbeddedChannel(new BedrockRawDatagramTraceHandler());

        channel.writeInbound(new DatagramPacket(Unpooled.wrappedBuffer(new byte[] { 0x01, 0x02 }), requestedRemote));
        channel.writeOutbound(new DatagramPacket(Unpooled.wrappedBuffer(new byte[] { 0x03, 0x04, 0x05 }), outboundOnlyRemote));

        String summary = BedrockRawDatagramTraceHandler.dumpSummaryForTesting(requestedRemote);

        assertTrue(summary.contains("rawDatagram remote=" + requestedRemote), summary);
        assertTrue(summary.contains("topRawRemotes=["), summary);
        assertTrue(summary.contains(outboundOnlyRemote.toString()), summary);
        assertTrue(summary.contains("outDatagrams=1"), summary);
    }
}

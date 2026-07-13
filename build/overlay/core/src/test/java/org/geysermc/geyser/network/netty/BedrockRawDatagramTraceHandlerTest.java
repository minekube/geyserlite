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
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

import java.lang.reflect.Field;
import java.net.InetSocketAddress;
import java.util.Map;

import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

class BedrockRawDatagramTraceHandlerTest {
    @BeforeEach
    void clearStats() throws ReflectiveOperationException {
        statsByRemote().clear();
    }

    @Test
    void dumpSummaryIncludesTopRawRemotesForAddressMismatches() {
        InetSocketAddress requestedRemote = new InetSocketAddress("203.0.113.10", 19132);
        InetSocketAddress outboundOnlyRemote = new InetSocketAddress("198.51.100.20", 19132);
        InetSocketAddress local = new InetSocketAddress("127.0.0.1", 19132);
        EmbeddedChannel channel = new EmbeddedChannel(new BedrockRawDatagramTraceHandler());

        channel.writeInbound(new DatagramPacket(
                Unpooled.wrappedBuffer(new byte[] { 0x01, 0x02 }), local, requestedRemote
        ));
        channel.writeOutbound(new DatagramPacket(Unpooled.wrappedBuffer(new byte[] { 0x03, 0x04, 0x05 }), outboundOnlyRemote));

        String summary = BedrockRawDatagramTraceHandler.dumpSummaryForTesting(requestedRemote);

        assertTrue(summary.contains("rawDatagram remote=" + requestedRemote), summary);
        assertTrue(summary.contains("topRawRemotes=["), summary);
        assertTrue(summary.contains(outboundOnlyRemote.toString()), summary);
        assertTrue(summary.contains("outDatagrams=1"), summary);
    }

    @Test
    void boundsTrackedRemoteAddresses() throws ReflectiveOperationException {
        Map<String, ?> stats = statsByRemote();
        InetSocketAddress local = new InetSocketAddress("127.0.0.1", 19132);
        EmbeddedChannel channel = new EmbeddedChannel(new BedrockRawDatagramTraceHandler());

        for (int i = 0; i < 1_100; i++) {
            InetSocketAddress remote = new InetSocketAddress("192.0." + (i / 256) + "." + (i % 256), 19132);
            channel.writeInbound(new DatagramPacket(Unpooled.buffer(1).writeByte(1), local, remote));
        }

        assertTrue(stats.size() <= 1_024, "tracked remotes=" + stats.size());
    }

    @Test
    void expiresInactiveRemoteAddresses() throws ReflectiveOperationException {
        Map<String, ?> stats = statsByRemote();
        InetSocketAddress remote = new InetSocketAddress("203.0.113.30", 19132);
        InetSocketAddress local = new InetSocketAddress("127.0.0.1", 19132);
        EmbeddedChannel channel = new EmbeddedChannel(new BedrockRawDatagramTraceHandler());
        channel.writeInbound(new DatagramPacket(Unpooled.buffer(1).writeByte(1), local, remote));

        Object remoteStats = stats.get(remote.toString());
        Field lastInAtMillis = remoteStats.getClass().getDeclaredField("lastInAtMillis");
        lastInAtMillis.setAccessible(true);
        lastInAtMillis.setLong(remoteStats, 1L);

        String summary = BedrockRawDatagramTraceHandler.dumpSummaryForTesting(remote);

        assertTrue(summary.contains("seen=false"), summary);
        assertFalse(stats.containsKey(remote.toString()));
    }

    @Test
    void resetsStatsWhenAnInactiveRemoteReturns() throws ReflectiveOperationException {
        Map<String, ?> stats = statsByRemote();
        InetSocketAddress remote = new InetSocketAddress("203.0.113.40", 19132);
        InetSocketAddress local = new InetSocketAddress("127.0.0.1", 19132);
        EmbeddedChannel channel = new EmbeddedChannel(new BedrockRawDatagramTraceHandler());
        channel.writeInbound(new DatagramPacket(Unpooled.buffer(1).writeByte(1), local, remote));

        Object remoteStats = stats.get(remote.toString());
        Field lastInAtMillis = remoteStats.getClass().getDeclaredField("lastInAtMillis");
        lastInAtMillis.setAccessible(true);
        lastInAtMillis.setLong(remoteStats, 1L);
        channel.writeInbound(new DatagramPacket(Unpooled.buffer(1).writeByte(1), local, remote));

        String summary = BedrockRawDatagramTraceHandler.dumpSummaryForTesting(remote);

        assertTrue(summary.contains("inDatagrams=1"), summary);
    }

    @SuppressWarnings("unchecked")
    private static Map<String, ?> statsByRemote() throws ReflectiveOperationException {
        Field field = BedrockRawDatagramTraceHandler.class.getDeclaredField("STATS_BY_REMOTE");
        field.setAccessible(true);
        return (Map<String, ?>) field.get(null);
    }
}

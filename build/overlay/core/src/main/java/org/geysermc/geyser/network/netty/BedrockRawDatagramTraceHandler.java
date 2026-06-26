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

import io.netty.buffer.ByteBuf;
import io.netty.channel.ChannelDuplexHandler;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelPromise;
import io.netty.channel.socket.DatagramPacket;

import java.net.SocketAddress;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;

/**
 * Staging-only parent UDP socket trace for distinguishing raw UDP loss from
 * RakNet/Bedrock decode stalls.
 */
public final class BedrockRawDatagramTraceHandler extends ChannelDuplexHandler {
    public static final String NAME = "geyserlite-bedrock-raw-datagram-trace";

    private static final boolean ENABLED = enabled("GEYSERLITE_BEDROCK_RAW_DATAGRAM_TRACE")
            || enabled("GEYSERLITE_BEDROCK_PACKET_TRACE");
    private static final Map<String, Stats> STATS_BY_REMOTE = new ConcurrentHashMap<>();

    public static boolean enabled() {
        return ENABLED;
    }

    public static String dumpSummary(SocketAddress remoteAddress) {
        if (!ENABLED) {
            return "rawDatagram disabled";
        }
        return dumpSummaryForTesting(remoteAddress);
    }

    static String dumpSummaryForTesting(SocketAddress remoteAddress) {
        if (remoteAddress == null) {
            return "rawDatagram remote=null";
        }
        Stats stats = STATS_BY_REMOTE.get(remoteAddress.toString());
        long now = System.currentTimeMillis();
        if (stats == null) {
            return "rawDatagram remote=" + remoteAddress + " seen=false " + topRawRemotes(now, 6);
        }
        return stats.summary(remoteAddress.toString(), now) + " " + topRawRemotes(now, 6);
    }

    @Override
    public void channelRead(ChannelHandlerContext ctx, Object msg) throws Exception {
        if (msg instanceof DatagramPacket packet) {
            recordIn(packet.sender(), packet.content());
        }
        super.channelRead(ctx, msg);
    }

    @Override
    public void write(ChannelHandlerContext ctx, Object msg, ChannelPromise promise) throws Exception {
        if (msg instanceof DatagramPacket packet) {
            recordOut(packet.recipient(), packet.content());
        }
        super.write(ctx, msg, promise);
    }

    private static void recordIn(SocketAddress remote, ByteBuf content) {
        if (remote == null) {
            return;
        }
        STATS_BY_REMOTE.computeIfAbsent(remote.toString(), ignored -> new Stats()).recordIn(content);
    }

    private static void recordOut(SocketAddress remote, ByteBuf content) {
        if (remote == null) {
            return;
        }
        STATS_BY_REMOTE.computeIfAbsent(remote.toString(), ignored -> new Stats()).recordOut(content);
    }

    private static boolean enabled(String name) {
        String value = System.getenv(name);
        return value != null && (value.equals("1") || Boolean.parseBoolean(value));
    }

    private static final class Stats {
        private long firstAtMillis;
        private long lastInAtMillis;
        private long lastOutAtMillis;
        private long inDatagrams;
        private long outDatagrams;
        private long inBytes;
        private long outBytes;
        private int lastInPacketId = -1;
        private int lastOutPacketId = -1;

        synchronized void recordIn(ByteBuf content) {
            long now = System.currentTimeMillis();
            if (firstAtMillis == 0) {
                firstAtMillis = now;
            }
            lastInAtMillis = now;
            inDatagrams++;
            inBytes += readableBytes(content);
            lastInPacketId = packetId(content);
        }

        synchronized void recordOut(ByteBuf content) {
            long now = System.currentTimeMillis();
            if (firstAtMillis == 0) {
                firstAtMillis = now;
            }
            lastOutAtMillis = now;
            outDatagrams++;
            outBytes += readableBytes(content);
            lastOutPacketId = packetId(content);
        }

        synchronized String summary(String remote, long now) {
            return "rawDatagram remote=" + remote
                    + " ageMs=" + age(now, firstAtMillis)
                    + " inDatagrams=" + inDatagrams
                    + " outDatagrams=" + outDatagrams
                    + " inBytes=" + inBytes
                    + " outBytes=" + outBytes
                    + " lastInAgeMs=" + age(now, lastInAtMillis)
                    + " lastOutAgeMs=" + age(now, lastOutAtMillis)
                    + " lastInPacketId=" + packetIdString(lastInPacketId)
                    + " lastOutPacketId=" + packetIdString(lastOutPacketId);
        }

        synchronized long totalDatagrams() {
            return inDatagrams + outDatagrams;
        }
    }

    private static int readableBytes(ByteBuf content) {
        return content == null ? 0 : content.readableBytes();
    }

    private static int packetId(ByteBuf content) {
        if (content == null || !content.isReadable()) {
            return -1;
        }
        return content.getUnsignedByte(content.readerIndex());
    }

    private static String packetIdString(int packetId) {
        return packetId < 0 ? "none" : "0x" + Integer.toHexString(packetId);
    }

    private static String topRawRemotes(long now, int limit) {
        List<Map.Entry<String, Stats>> entries = new ArrayList<>(STATS_BY_REMOTE.entrySet());
        entries.sort(Comparator.comparingLong(entry -> -entry.getValue().totalDatagrams()));
        StringBuilder builder = new StringBuilder("topRawRemotes=[");
        int count = Math.min(entries.size(), limit);
        for (int i = 0; i < count; i++) {
            if (i > 0) {
                builder.append(", ");
            }
            Map.Entry<String, Stats> entry = entries.get(i);
            builder.append(entry.getValue().summary(entry.getKey(), now));
        }
        if (entries.size() > limit) {
            builder.append(", ...");
        }
        return builder.append(']').toString();
    }

    private static String age(long now, long eventAtMillis) {
        if (eventAtMillis == 0) {
            return "never";
        }
        return Long.toString(now - eventAtMillis);
    }
}

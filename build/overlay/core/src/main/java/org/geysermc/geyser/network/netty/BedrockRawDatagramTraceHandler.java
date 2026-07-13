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
    private static final int MAX_REMOTE_STATS = 1_024;
    private static final long REMOTE_STATS_TTL_MILLIS = 10 * 60 * 1_000L;
    private static final long MAINTENANCE_INTERVAL_MILLIS = 60 * 1_000L;
    private static final Map<String, Stats> STATS_BY_REMOTE = new ConcurrentHashMap<>();
    private static final Object STATS_MAINTENANCE_LOCK = new Object();
    private static long nextMaintenanceAtMillis;

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
        long now = System.currentTimeMillis();
        maintainStats(now, true);
        Stats stats = STATS_BY_REMOTE.get(remoteAddress.toString());
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
        long now = System.currentTimeMillis();
        String key = remote.toString();
        while (!statsFor(key, now).recordIn(content, now)) {
            now = System.currentTimeMillis();
        }
    }

    private static void recordOut(SocketAddress remote, ByteBuf content) {
        if (remote == null) {
            return;
        }
        long now = System.currentTimeMillis();
        String key = remote.toString();
        while (!statsFor(key, now).recordOut(content, now)) {
            now = System.currentTimeMillis();
        }
    }

    private static Stats statsFor(String remote, long now) {
        Stats stats = STATS_BY_REMOTE.get(remote);
        if (stats != null && !inactive(stats, now)) {
            return stats;
        }
        synchronized (STATS_MAINTENANCE_LOCK) {
            stats = STATS_BY_REMOTE.get(remote);
            if (stats != null && !inactive(stats, now)) {
                return stats;
            }
            if (stats != null) {
                stats.retire();
                STATS_BY_REMOTE.remove(remote, stats);
            }
            maintainStatsLocked(now, false);
            while (STATS_BY_REMOTE.size() >= MAX_REMOTE_STATS) {
                if (!evictOldest()) {
                    break;
                }
            }
            Stats created = new Stats(now);
            STATS_BY_REMOTE.put(remote, created);
            return created;
        }
    }

    private static void maintainStats(long now, boolean force) {
        synchronized (STATS_MAINTENANCE_LOCK) {
            maintainStatsLocked(now, force);
        }
    }

    private static void maintainStatsLocked(long now, boolean force) {
        if (!force && now < nextMaintenanceAtMillis) {
            return;
        }
        expireInactive(now);
        nextMaintenanceAtMillis = now + MAINTENANCE_INTERVAL_MILLIS;
    }

    private static void expireInactive(long now) {
        for (Map.Entry<String, Stats> entry : STATS_BY_REMOTE.entrySet()) {
            Stats stats = entry.getValue();
            if (stats.retireIfInactive(now)) {
                STATS_BY_REMOTE.remove(entry.getKey(), stats);
            }
        }
    }

    private static boolean inactive(Stats stats, long now) {
        return stats.inactive(now);
    }

    private static boolean evictOldest() {
        String oldestRemote = null;
        Stats oldestStats = null;
        long oldestAtMillis = Long.MAX_VALUE;
        for (Map.Entry<String, Stats> entry : STATS_BY_REMOTE.entrySet()) {
            long lastSeenAtMillis = entry.getValue().lastSeenAtMillis();
            if (lastSeenAtMillis < oldestAtMillis) {
                oldestRemote = entry.getKey();
                oldestStats = entry.getValue();
                oldestAtMillis = lastSeenAtMillis;
            }
        }
        if (oldestRemote == null) {
            return false;
        }
        oldestStats.retire();
        STATS_BY_REMOTE.remove(oldestRemote, oldestStats);
        return true;
    }

    private static boolean enabled(String name) {
        String value = System.getenv(name);
        return value != null && (value.equals("1") || Boolean.parseBoolean(value));
    }

    private static final class Stats {
        private long firstAtMillis;
        private long lastSeenAtMillis;
        private long lastInAtMillis;
        private long lastOutAtMillis;
        private long inDatagrams;
        private long outDatagrams;
        private long inBytes;
        private long outBytes;
        private int lastInPacketId = -1;
        private int lastOutPacketId = -1;
        private boolean retired;

        Stats(long now) {
            firstAtMillis = now;
            lastSeenAtMillis = now;
        }

        synchronized boolean recordIn(ByteBuf content, long now) {
            if (retired) {
                return false;
            }
            lastSeenAtMillis = now;
            lastInAtMillis = now;
            inDatagrams++;
            inBytes += readableBytes(content);
            lastInPacketId = packetId(content);
            return true;
        }

        synchronized boolean recordOut(ByteBuf content, long now) {
            if (retired) {
                return false;
            }
            lastSeenAtMillis = now;
            lastOutAtMillis = now;
            outDatagrams++;
            outBytes += readableBytes(content);
            lastOutPacketId = packetId(content);
            return true;
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

        synchronized long lastSeenAtMillis() {
            return lastSeenAtMillis;
        }

        synchronized boolean inactive(long now) {
            return retired || now - lastSeenAtMillis >= REMOTE_STATS_TTL_MILLIS;
        }

        synchronized boolean retireIfInactive(long now) {
            if (!inactive(now)) {
                return false;
            }
            retired = true;
            return true;
        }

        synchronized void retire() {
            retired = true;
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

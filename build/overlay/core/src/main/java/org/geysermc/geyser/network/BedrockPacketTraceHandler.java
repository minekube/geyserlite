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

import io.netty.buffer.ByteBuf;
import io.netty.channel.Channel;
import io.netty.channel.ChannelDuplexHandler;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelPromise;
import org.cloudburstmc.protocol.bedrock.packet.BedrockPacket;
import org.cloudburstmc.protocol.bedrock.netty.BedrockPacketWrapper;
import org.geysermc.geyser.GeyserImpl;
import org.geysermc.geyser.network.netty.BedrockRawDatagramTraceHandler;
import org.geysermc.geyser.session.GeyserSession;

import java.net.SocketAddress;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.Deque;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

/**
 * Staging-only packet trace hook for diagnosing Bedrock/RakNet timeouts.
 *
 * <p>Enable with {@code GEYSERLITE_BEDROCK_PACKET_TRACE=1}. Set
 * {@code GEYSERLITE_BEDROCK_PACKET_TRACE_VERBOSE=1} to log every decoded
 * Bedrock packet as it passes the codec.</p>
 */
public final class BedrockPacketTraceHandler extends ChannelDuplexHandler {
    public static final String NAME = "geyserlite-bedrock-packet-trace";

    private static final boolean ENABLED = enabled("GEYSERLITE_BEDROCK_PACKET_TRACE");
    private static final boolean VERBOSE = enabled("GEYSERLITE_BEDROCK_PACKET_TRACE_VERBOSE");
    private static final int DEFAULT_RING_SIZE = 256;
    private static final int RING_SIZE = intEnv("GEYSERLITE_BEDROCK_PACKET_TRACE_RING", DEFAULT_RING_SIZE);

    private final GeyserImpl geyser;
    private final GeyserSession session;
    private final Deque<String> recent = new ArrayDeque<>(RING_SIZE);
    private final long startedAtMillis = System.currentTimeMillis();
    private long inboundPackets;
    private long outboundPackets;
    private long inboundBytes;
    private long outboundBytes;
    private long lastInboundAtMillis;
    private long lastOutboundAtMillis;
    private long writeFailures;
    private long writableTransitions;
    private long unwritableTransitions;
    private boolean lastWritable = true;
    private String lastChannelState = "unknown";
    private SocketAddress lastRemoteAddress;
    private final Map<String, Long> inboundByPacket = new HashMap<>();
    private final Map<String, Long> outboundByPacket = new HashMap<>();

    public BedrockPacketTraceHandler(GeyserImpl geyser, GeyserSession session) {
        this.geyser = geyser;
        this.session = session;
    }

    public static boolean enabled() {
        return ENABLED;
    }

    public static void dump(Channel channel, String reason) {
        if (!ENABLED || channel == null) {
            return;
        }
        Object handler = channel.pipeline().get(NAME);
        if (handler instanceof BedrockPacketTraceHandler trace) {
            trace.dump(reason);
        }
    }

    @Override
    public void channelActive(ChannelHandlerContext ctx) throws Exception {
        updateChannelState(ctx.channel());
        record("event", "channelActive " + endpoints(ctx.channel()));
        super.channelActive(ctx);
    }

    @Override
    public void channelRead(ChannelHandlerContext ctx, Object msg) throws Exception {
        inboundPackets++;
        inboundBytes += readableBytes(msg);
        lastInboundAtMillis = System.currentTimeMillis();
        inboundByPacket.merge(packetName(msg), 1L, Long::sum);
        updateChannelState(ctx.channel());
        record("in", describe(msg));
        super.channelRead(ctx, msg);
    }

    @Override
    public void write(ChannelHandlerContext ctx, Object msg, ChannelPromise promise) throws Exception {
        outboundPackets++;
        outboundBytes += readableBytes(msg);
        lastOutboundAtMillis = System.currentTimeMillis();
        outboundByPacket.merge(packetName(msg), 1L, Long::sum);
        updateChannelState(ctx.channel());
        promise.addListener((ChannelFutureListener) future -> {
            if (!future.isSuccess()) {
                synchronized (BedrockPacketTraceHandler.this) {
                    writeFailures++;
                    record("event", "writeFailed " + future.cause().getClass().getName() + ": "
                            + future.cause().getMessage() + " packet=" + packetName(msg));
                }
            }
        });
        record("out", describe(msg));
        super.write(ctx, msg, promise);
    }

    @Override
    public void userEventTriggered(ChannelHandlerContext ctx, Object evt) throws Exception {
        updateChannelState(ctx.channel());
        record("event", "userEvent " + evt.getClass().getName() + " " + abbreviate(String.valueOf(evt)));
        super.userEventTriggered(ctx, evt);
    }

    @Override
    public void exceptionCaught(ChannelHandlerContext ctx, Throwable cause) throws Exception {
        updateChannelState(ctx.channel());
        record("event", "exception " + cause.getClass().getName() + ": " + cause.getMessage());
        dump("exceptionCaught");
        super.exceptionCaught(ctx, cause);
    }

    @Override
    public void channelWritabilityChanged(ChannelHandlerContext ctx) throws Exception {
        updateChannelState(ctx.channel());
        record("event", "channelWritabilityChanged writable=" + ctx.channel().isWritable()
                + " bytesBeforeUnwritable=" + ctx.channel().bytesBeforeUnwritable()
                + " bytesBeforeWritable=" + ctx.channel().bytesBeforeWritable());
        super.channelWritabilityChanged(ctx);
    }

    @Override
    public void channelInactive(ChannelHandlerContext ctx) throws Exception {
        updateChannelState(ctx.channel());
        record("event", "channelInactive " + endpoints(ctx.channel()));
        dump("channelInactive");
        super.channelInactive(ctx);
    }

    private synchronized void record(String direction, String detail) {
        long now = System.currentTimeMillis();
        String line = now + " +" + (now - startedAtMillis) + "ms " + direction
                + " " + username()
                + " " + detail;
        if (recent.size() >= RING_SIZE) {
            recent.removeFirst();
        }
        recent.addLast(line);
        if (VERBOSE) {
            geyser.getLogger().info("geyserlite bedrock packet trace " + line);
        }
    }

    private synchronized void dump(String reason) {
        long now = System.currentTimeMillis();
        geyser.getLogger().warning("geyserlite bedrock packet trace dump reason=" + reason
                + " user=" + username()
                + " ageMs=" + (now - startedAtMillis)
                + " inboundPackets=" + inboundPackets
                + " outboundPackets=" + outboundPackets
                + " inboundBytes=" + inboundBytes
                + " outboundBytes=" + outboundBytes
                + " lastInboundAgeMs=" + age(now, lastInboundAtMillis)
                + " lastOutboundAgeMs=" + age(now, lastOutboundAtMillis)
                + " writeFailures=" + writeFailures
                + " writableTransitions=" + writableTransitions
                + " unwritableTransitions=" + unwritableTransitions
                + " channel=" + lastChannelState);
        geyser.getLogger().warning("geyserlite bedrock packet trace packetCounts in="
                + topPacketCounts(inboundByPacket) + " out=" + topPacketCounts(outboundByPacket));
        geyser.getLogger().warning("geyserlite bedrock packet trace "
                + BedrockRawDatagramTraceHandler.dumpSummary(lastRemoteAddress));
        for (String line : recent) {
            geyser.getLogger().warning("geyserlite bedrock packet trace recent " + line);
        }
    }

    private String username() {
        try {
            return session.bedrockUsername();
        } catch (Throwable ignored) {
            return "unknown";
        }
    }

    private static String describe(Object msg) {
        if (msg instanceof BedrockPacketWrapper wrapper) {
            BedrockPacket packet = wrapper.getPacket();
            return "BedrockPacketWrapper packet=" + packetName(msg)
                    + " packetId=" + wrapper.getPacketId()
                    + " flags=" + wrapper.getFlags()
                    + " packetBufferBytes=" + readableBytes(wrapper.getPacketBuffer())
                    + " " + abbreviate(String.valueOf(packet));
        }
        if (msg instanceof BedrockPacket packet) {
            return packet.getClass().getSimpleName() + " " + abbreviate(String.valueOf(packet));
        }
        if (msg instanceof ByteBuf buf) {
            return "ByteBuf readableBytes=" + buf.readableBytes();
        }
        return msg.getClass().getName() + " " + abbreviate(String.valueOf(msg));
    }

    private static int readableBytes(Object msg) {
        if (msg == null) {
            return 0;
        }
        if (msg instanceof ByteBuf buf) {
            return buf.readableBytes();
        }
        if (msg instanceof BedrockPacketWrapper wrapper) {
            return readableBytes(wrapper.getPacketBuffer());
        }
        return 0;
    }

    private synchronized void updateChannelState(Channel channel) {
        lastRemoteAddress = channel.remoteAddress();
        boolean writable = channel.isWritable();
        if (writable != lastWritable) {
            if (writable) {
                writableTransitions++;
            } else {
                unwritableTransitions++;
            }
            lastWritable = writable;
        }
        lastChannelState = endpoints(channel)
                + " active=" + channel.isActive()
                + " open=" + channel.isOpen()
                + " writable=" + writable
                + " bytesBeforeUnwritable=" + channel.bytesBeforeUnwritable()
                + " bytesBeforeWritable=" + channel.bytesBeforeWritable();
    }

    private static String packetName(Object msg) {
        if (msg instanceof BedrockPacketWrapper wrapper && wrapper.getPacket() != null) {
            return wrapper.getPacket().getClass().getSimpleName();
        }
        if (msg instanceof BedrockPacket packet) {
            return packet.getClass().getSimpleName();
        }
        if (msg == null) {
            return "null";
        }
        return msg.getClass().getSimpleName();
    }

    private static String topPacketCounts(Map<String, Long> counts) {
        List<Map.Entry<String, Long>> entries = new ArrayList<>(counts.entrySet());
        entries.sort(Map.Entry.comparingByValue(Comparator.reverseOrder()));
        StringBuilder builder = new StringBuilder("[");
        int limit = Math.min(entries.size(), 12);
        for (int i = 0; i < limit; i++) {
            if (i > 0) {
                builder.append(", ");
            }
            Map.Entry<String, Long> entry = entries.get(i);
            builder.append(entry.getKey()).append("=").append(entry.getValue());
        }
        if (entries.size() > limit) {
            builder.append(", ...");
        }
        return builder.append("]").toString();
    }

    private static String endpoints(Channel channel) {
        return "id=" + channel.id().asShortText()
                + " local=" + channel.localAddress()
                + " remote=" + channel.remoteAddress();
    }

    private static String age(long now, long eventAtMillis) {
        if (eventAtMillis == 0) {
            return "never";
        }
        return Long.toString(now - eventAtMillis);
    }

    private static String abbreviate(String value) {
        if (value == null) {
            return "null";
        }
        return value.length() <= 240 ? value : value.substring(0, 240) + "...";
    }

    private static boolean enabled(String name) {
        String value = System.getenv(name);
        return value != null && (value.equals("1") || Boolean.parseBoolean(value));
    }

    private static int intEnv(String name, int fallback) {
        String value = System.getenv(name);
        if (value == null || value.isBlank()) {
            return fallback;
        }
        try {
            return Math.max(16, Integer.parseInt(value));
        } catch (NumberFormatException ignored) {
            return fallback;
        }
    }
}

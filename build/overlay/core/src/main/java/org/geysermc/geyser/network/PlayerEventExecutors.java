/*
 * Copyright (c) 2026 GeyserMC. http://geysermc.org
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
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
 * THE SOFTWARE.
 */

package org.geysermc.geyser.network;

import io.netty.channel.DefaultEventLoopGroup;
import io.netty.channel.EventLoop;
import io.netty.util.concurrent.DefaultThreadFactory;
import io.netty.util.concurrent.EventExecutor;

import java.util.Queue;
import java.util.concurrent.ConcurrentLinkedQueue;
import java.util.concurrent.atomic.AtomicLong;

/**
 * The player threads of one Bedrock transport, as handed to each new session.
 * <p>
 * A player thread runs the session tick. If a fatal error escapes the loop (an
 * {@link OutOfMemoryError} while compressing a downstream burst, for example), Netty logs
 * {@code Unexpected exception from an event executor} once and the executor is terminated
 * for good: it is never respawned and every {@code execute} or {@code schedule} on it is
 * rejected from then on. Every session created afterwards would be bound to that dead
 * executor and could never tick, which is invisible from the client side: it stays on
 * "generating world" until its own timeout while its XUID stays reserved.
 * <p>
 * So a terminated executor must never be handed to a new session. When one is found the
 * whole group is retired and replaced. Sessions that are still on the retired group keep
 * ticking; the retired group is shut down when the transport stops.
 */
public final class PlayerEventExecutors {
    private final String threadName;
    /**
     * Groups that were replaced because one of their threads had terminated. They are only
     * kept so that {@link #shutdown()} can stop the threads of sessions that are still on
     * them.
     */
    private final Queue<DefaultEventLoopGroup> retired = new ConcurrentLinkedQueue<>();
    private final AtomicLong terminatedExecutors = new AtomicLong();

    private volatile DefaultEventLoopGroup eventLoopGroup;

    public PlayerEventExecutors(String threadName) {
        this(threadName, 0);
    }

    /**
     * @param threads the number of player threads, or 0 to use Netty's default
     */
    PlayerEventExecutors(String threadName, int threads) {
        this.threadName = threadName;
        this.eventLoopGroup = newEventLoopGroup(threads);
    }

    private DefaultEventLoopGroup newEventLoopGroup(int threads) {
        return new DefaultEventLoopGroup(threads, new DefaultThreadFactory(this.threadName));
    }

    /**
     * @return a live executor for a new session, replacing the group if any of its threads
     * has terminated
     */
    public EventLoop next() {
        return liveGroup().next();
    }

    /**
     * @return the group new sessions are bound to; replaced when a player thread in it has
     * terminated
     */
    public DefaultEventLoopGroup liveGroup() {
        DefaultEventLoopGroup group = this.eventLoopGroup;
        if (hasTerminatedExecutor(group)) {
            group = replace(group);
        }
        return group;
    }

    private synchronized DefaultEventLoopGroup replace(DefaultEventLoopGroup observed) {
        // Another thread may have replaced it while this one was waiting for the lock
        if (this.eventLoopGroup != observed) {
            return this.eventLoopGroup;
        }
        long terminated = 0;
        for (EventExecutor executor : observed) {
            if (isTerminated(executor)) {
                terminated++;
            }
        }
        this.retired.add(observed);
        this.terminatedExecutors.addAndGet(terminated);
        this.eventLoopGroup = newEventLoopGroup(observed.executorCount());
        return this.eventLoopGroup;
    }

    public static boolean hasTerminatedExecutor(DefaultEventLoopGroup group) {
        for (EventExecutor executor : group) {
            if (isTerminated(executor)) {
                return true;
            }
        }
        return false;
    }

    private static boolean isTerminated(EventExecutor executor) {
        return executor.isShuttingDown() || executor.isTerminated();
    }

    /**
     * @return how many player threads this transport has lost to a fatal event-loop error,
     * counted when the dead executor was replaced
     */
    public long terminatedExecutors() {
        return this.terminatedExecutors.get();
    }

    public void shutdown() {
        this.retired.add(this.eventLoopGroup);
        DefaultEventLoopGroup group;
        while ((group = this.retired.poll()) != null) {
            group.shutdownGracefully();
        }
    }
}

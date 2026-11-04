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

import io.netty.channel.DefaultEventLoop;
import io.netty.channel.DefaultEventLoopGroup;
import io.netty.channel.EventLoop;
import io.netty.util.concurrent.DefaultThreadFactory;
import io.netty.util.concurrent.EventExecutor;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.Queue;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ConcurrentLinkedQueue;
import java.util.concurrent.Executor;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicLong;
import java.util.function.Consumer;

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
 * whole group is retired and replaced, and the loss is reported once per retired generation
 * to the {@link Loss} listener. Sessions that are still on the retired group keep ticking;
 * the retired group is shut down when the transport stops.
 */
public final class PlayerEventExecutors {
    private final String threadName;
    /**
     * Told once about every generation that is retired because one of its threads had
     * terminated.
     */
    private final Consumer<Loss> listener;
    /**
     * Groups that were replaced because one of their threads had terminated. They are only
     * kept so that {@link #shutdown()} can stop the threads of sessions that are still on
     * them.
     */
    private final Queue<DefaultEventLoopGroup> retired = new ConcurrentLinkedQueue<>();
    private final AtomicLong terminatedExecutors = new AtomicLong();

    private volatile Generation generation;

    /**
     * A player thread group that was retired because one of its threads had terminated.
     *
     * @param generation the generation number of the group that was lost; the first group of a
     *                   transport is generation 1
     * @param terminatedExecutors the names of the player threads that terminated, for example
     *                            {@code Geyser NetherNet player thread #1}
     * @param sessions how many sessions had been handed an executor from the lost group
     */
    public record Loss(int generation, List<String> terminatedExecutors, long sessions) {
        public Loss {
            terminatedExecutors = List.copyOf(terminatedExecutors);
        }
    }

    public PlayerEventExecutors(String threadName) {
        this(threadName, 0, loss -> { });
    }

    public PlayerEventExecutors(String threadName, Consumer<Loss> listener) {
        this(threadName, 0, listener);
    }

    /**
     * @param threads the number of player threads, or 0 to use Netty's default
     */
    PlayerEventExecutors(String threadName, int threads) {
        this(threadName, threads, loss -> { });
    }

    PlayerEventExecutors(String threadName, int threads, Consumer<Loss> listener) {
        this.threadName = threadName;
        this.listener = listener != null ? listener : loss -> { };
        this.generation = newGeneration(1, threads);
    }

    /**
     * @return a live executor for a new session, replacing the group if any of its threads
     * has terminated
     */
    public EventLoop next() {
        Generation current = liveGeneration();
        current.sessionsHandedOut.incrementAndGet();
        return current.group.next();
    }

    /**
     * @return the group new sessions are bound to; replaced when a player thread in it has
     * terminated
     */
    public DefaultEventLoopGroup liveGroup() {
        return liveGeneration().group;
    }

    private Generation liveGeneration() {
        Generation observed = this.generation;
        if (!hasTerminatedExecutor(observed.group)) {
            return observed;
        }
        return replace(observed);
    }

    /**
     * Retires a group that lost a player thread and hands new sessions a fresh generation.
     * <p>
     * The loss is reported exactly once per generation: only the caller that performs the swap
     * reports it, so logins waiting on the lock never report the same generation twice.
     */
    private Generation replace(Generation observed) {
        Generation replacement;
        Loss loss;
        synchronized (this) {
            // Another thread may have replaced it while this one was waiting for the lock
            if (this.generation != observed) {
                return this.generation;
            }
            List<String> terminated = new ArrayList<>();
            for (EventExecutor executor : observed.group) {
                if (isTerminated(executor)) {
                    terminated.add(observed.nameOf(executor));
                }
            }
            this.retired.add(observed.group);
            this.terminatedExecutors.addAndGet(terminated.size());
            replacement = newGeneration(observed.number + 1, observed.group.executorCount());
            this.generation = replacement;
            loss = new Loss(observed.number, terminated, observed.sessionsHandedOut.get());
        }
        // Outside the lock: a slow listener must not hold up the logins of the new generation
        if (!loss.terminatedExecutors().isEmpty()) {
            this.listener.accept(loss);
        }
        return replacement;
    }

    /**
     * @param number the generation number, counting from 1
     * @param threads the number of player threads, or 0 to use Netty's default
     */
    private Generation newGeneration(int number, int threads) {
        String threadName = this.threadName;
        AtomicInteger threadIndex = new AtomicInteger();
        Map<EventExecutor, String> threadNames = new ConcurrentHashMap<>();
        // Each player thread is named after the transport and its index, so a reported loss
        // names the thread an operator will also find in a thread dump.
        DefaultEventLoopGroup group = new DefaultEventLoopGroup(threads, new DefaultThreadFactory(threadName)) {
            @Override
            protected EventLoop newChild(Executor executor, Object... args) {
                String name = threadName + " #" + threadIndex.incrementAndGet();
                EventLoop child = new DefaultEventLoop(this, new PlayerThreadFactory(name));
                threadNames.put(child, name);
                return child;
            }
        };
        return new Generation(number, group, threadNames);
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
        synchronized (this) {
            this.retired.add(this.generation.group);
        }
        DefaultEventLoopGroup group;
        while ((group = this.retired.poll()) != null) {
            group.shutdownGracefully();
        }
    }

    /**
     * One player thread group, and what it has served so far.
     */
    private static final class Generation {
        private final int number;
        private final DefaultEventLoopGroup group;
        /**
         * The thread name of each executor, so a loss can name the executors that terminated
         * after Netty has already discarded their threads.
         */
        private final Map<EventExecutor, String> threadNames;
        private final AtomicLong sessionsHandedOut = new AtomicLong();

        private Generation(int number, DefaultEventLoopGroup group, Map<EventExecutor, String> threadNames) {
            this.number = number;
            this.group = group;
            this.threadNames = threadNames;
        }

        private String nameOf(EventExecutor executor) {
            return this.threadNames.getOrDefault(executor, "player thread of generation " + this.number);
        }
    }

    /**
     * Names a player thread exactly as a {@link Loss} reports it. Netty's default factory would
     * append its own pool and thread ids, which makes the reported name impossible to recognize
     * in a thread dump.
     */
    private static final class PlayerThreadFactory extends DefaultThreadFactory {
        private final String name;

        private PlayerThreadFactory(String name) {
            super(name);
            this.name = name;
        }

        @Override
        protected Thread newThread(Runnable runnable, String ignored) {
            return super.newThread(runnable, this.name);
        }
    }
}

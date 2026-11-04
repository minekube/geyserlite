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
import io.netty.util.concurrent.EventExecutor;
import org.junit.jupiter.api.Test;

import java.util.List;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertSame;
import static org.junit.jupiter.api.Assertions.assertTrue;

/**
 * Player threads that die are counted and replaced, never reused, and the replacement is
 * reported exactly once per retired generation.
 *
 * @see PlayerThreadTerminationTest for the reproduction of the production failure
 */
class PlayerEventExecutorsTest {

    @Test
    void terminatedPlayerThreadsAreCountedAndReplaced() throws Exception {
        PlayerEventExecutors executors = new PlayerEventExecutors("Geyser test player thread", 2);
        try {
            DefaultEventLoopGroup first = executors.liveGroup();
            assertEquals(2, first.executorCount());

            assertEquals(2, terminateEveryThread(first), "both player threads must have terminated");
            assertTrue(PlayerEventExecutors.hasTerminatedExecutor(first));

            EventLoop next = executors.next();
            assertFalse(next.isTerminated(), "a terminated player thread must never be handed to a new session");
            assertEquals(2, executors.terminatedExecutors(), "the loss of a player thread must be counted");
            assertTrue(executors.liveGroup() != first, "the group that lost its threads must be replaced");
            assertEquals(2, executors.liveGroup().executorCount(), "the replacement keeps the transport's thread count");
        } finally {
            executors.shutdown();
        }
    }

    @Test
    void healthyPlayerThreadsAreReused() throws Exception {
        AtomicInteger lossReports = new AtomicInteger();
        PlayerEventExecutors executors =
            new PlayerEventExecutors("Geyser test player thread", 2, loss -> lossReports.incrementAndGet());
        try {
            DefaultEventLoopGroup group = executors.liveGroup();

            assertFalse(executors.next().isTerminated());
            assertFalse(executors.next().isTerminated());

            assertSame(group, executors.liveGroup(), "a healthy group must not be replaced");
            assertEquals(0, executors.terminatedExecutors(), "no player thread was lost");
            assertEquals(0, lossReports.get(), "a healthy generation must not be reported as lost");
        } finally {
            executors.shutdown();
        }
    }

    /**
     * The login path hands every session {@code PlayerEventExecutors.next()}
     * ({@code GeyserServerInitializer.initSession}), so the replacement a login triggers is the
     * only chance to report a dead generation on the NetherNet path. That report must fire once,
     * name the terminated player threads and say how many sessions had been bound to the retired
     * group — a loss invisible from the client side is what kept the 2026-11-01 NetherNet outage
     * silent for 35 hours.
     */
    @Test
    void aLostGenerationIsReportedExactlyOnceWithItsIdentityAndSessionCount() throws Exception {
        List<PlayerEventExecutors.Loss> reported = new CopyOnWriteArrayList<>();
        PlayerEventExecutors executors =
            new PlayerEventExecutors("Geyser test player thread", 2, reported::add);
        try {
            // Three logins on the healthy generation, exactly like initSession's next()
            for (int login = 0; login < 3; login++) {
                assertFalse(executors.next().isTerminated(), "the healthy generation must serve the login");
            }
            DefaultEventLoopGroup lost = executors.liveGroup();
            assertEquals(2, terminateEveryThread(lost), "both player threads must have terminated");

            assertFalse(executors.next().isTerminated(),
                "the login that follows the death must not get a terminated executor");

            assertEquals(1, reported.size(), "the retired generation must be reported exactly once");
            PlayerEventExecutors.Loss loss = reported.get(0);
            assertEquals(1, loss.generation(), "the first player thread generation is generation 1");
            assertEquals(2, loss.terminatedExecutors().size(),
                "the report must name every terminated player thread: " + loss.terminatedExecutors());
            assertTrue(loss.terminatedExecutors().contains("Geyser test player thread #1"),
                "the report must identify the terminated executor: " + loss.terminatedExecutors());
            assertTrue(loss.terminatedExecutors().contains("Geyser test player thread #2"),
                "the report must identify the terminated executor: " + loss.terminatedExecutors());
            assertEquals(3, loss.sessions(),
                "the report must carry how many sessions had been handed an executor from the retired group");

            // Every later login is served by the replacement and must not report the same loss again
            for (int login = 0; login < 5; login++) {
                assertFalse(executors.next().isTerminated());
            }
            assertEquals(1, reported.size(), "a generation must be reported once, not once per login");
        } finally {
            executors.shutdown();
        }
    }

    /**
     * A transport that loses its player threads twice must be told twice, with the generation
     * number advancing, so the report can be correlated with the Netty
     * {@code Unexpected exception from an event executor} that preceded it.
     */
    @Test
    void everyLostGenerationIsReported() throws Exception {
        List<PlayerEventExecutors.Loss> reported = new CopyOnWriteArrayList<>();
        PlayerEventExecutors executors =
            new PlayerEventExecutors("Geyser test player thread", 2, reported::add);
        try {
            assertEquals(2, terminateEveryThread(executors.liveGroup()));
            executors.next();

            // one more login on the replacement, then that generation dies as well
            assertFalse(executors.next().isTerminated());
            assertEquals(2, terminateEveryThread(executors.liveGroup()));
            executors.next();

            assertEquals(2, reported.size(), "each retired generation must be reported");
            assertEquals(1, reported.get(0).generation());
            assertEquals(2, reported.get(1).generation(), "the generation number must advance");
            assertEquals(2, reported.get(1).sessions(),
                "the second report carries the logins the replacement had served: the one that"
                    + " triggered it and the one after");
            assertEquals(4, executors.terminatedExecutors());
        } finally {
            executors.shutdown();
        }
    }

    private static int terminateEveryThread(DefaultEventLoopGroup group) throws InterruptedException {
        for (EventExecutor executor : group) {
            executor.execute(() -> {
                throw new OutOfMemoryError("simulated: Garbage-collected heap size exceeded.");
            });
        }

        int terminated = 0;
        for (EventExecutor executor : group) {
            if (executor.terminationFuture().await(10, TimeUnit.SECONDS)) {
                terminated++;
            }
        }
        return terminated;
    }
}

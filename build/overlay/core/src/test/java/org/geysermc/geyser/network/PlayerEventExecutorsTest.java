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

import java.util.concurrent.TimeUnit;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertSame;
import static org.junit.jupiter.api.Assertions.assertTrue;

/**
 * Player threads that die are counted and replaced, never reused.
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
        PlayerEventExecutors executors = new PlayerEventExecutors("Geyser test player thread", 2);
        try {
            DefaultEventLoopGroup group = executors.liveGroup();

            assertFalse(executors.next().isTerminated());
            assertFalse(executors.next().isTerminated());

            assertSame(group, executors.liveGroup(), "a healthy group must not be replaced");
            assertEquals(0, executors.terminatedExecutors(), "no player thread was lost");
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

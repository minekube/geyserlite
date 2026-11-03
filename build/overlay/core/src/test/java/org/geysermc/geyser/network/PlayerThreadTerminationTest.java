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
import org.geysermc.geyser.network.bedrock.nethernet.NetherNetChannelInitialiser;
import org.junit.jupiter.api.Test;

import java.util.concurrent.TimeUnit;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

/**
 * A player thread that dies must not take the whole transport with it.
 * <p>
 * On 2026-11-01 an {@link OutOfMemoryError} escaped the Netty run loop of both NetherNet
 * player threads of a long-lived server. Netty logged
 * {@code Unexpected exception from an event executor} once and terminated the executors.
 * Every NetherNet session created afterwards was bound to one of those terminated executors,
 * so {@code GeyserSession.connectDownstream} threw {@code RejectedExecutionException} when it
 * scheduled the session tick; {@code authenticate()} swallowed it, the client waited on
 * "generating world" until its own timeout, and the XUID stayed reserved. RakNet was
 * unaffected because it uses a different group, which is why the standing RakNet-only check
 * stayed green for 35 hours.
 * <p>
 * This test reproduces the executors' death exactly (a task that throws an
 * {@link OutOfMemoryError} inside the loop terminates the executor and nothing brings it
 * back) and asserts that the next session is not bound to a dead one.
 */
class PlayerThreadTerminationTest {

    /**
     * Mirrors how {@code GeyserSessionInitializer.initSession} picks the tick executor:
     * {@code getEventLoopGroup().next()}.
     */
    @Test
    void terminatedPlayerExecutorIsNotHandedToTheNextSession() throws Exception {
        GeyserServerInitializer initialiser = new NetherNetChannelInitialiser(null);
        try {
            DefaultEventLoopGroup group = initialiser.getEventLoopGroup();
            int threads = group.executorCount();
            assertTrue(threads > 0, "a transport must run at least one player thread");

            assertEquals(threads, terminateEveryThread(group),
                "the simulated fatal error must have terminated every player thread");

            EventLoop next = initialiser.getEventLoopGroup().next();
            assertFalse(next.isTerminated(),
                "a terminated player executor was handed to a new session: the session can never tick, "
                    + "the client waits on 'generating world' until its own timeout and its XUID stays "
                    + "reserved. A terminated executor must never be used for new sessions.");
        } finally {
            initialiser.getEventLoopGroup().shutdownGracefully().await(5, TimeUnit.SECONDS);
        }
    }

    /**
     * Kills every player thread the way the production heap OOM did and returns how many
     * terminated.
     */
    private static int terminateEveryThread(DefaultEventLoopGroup group) throws InterruptedException {
        for (EventExecutor executor : group) {
            executor.execute(() -> {
                throw new OutOfMemoryError("simulated: Garbage-collected heap size exceeded. "
                    + "Consider increasing the maximum Java heap size, for example with '-Xmx'.");
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

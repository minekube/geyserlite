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
import org.cloudburstmc.protocol.bedrock.BedrockServerSession;
import org.geysermc.geyser.GeyserImpl;
import org.geysermc.geyser.GeyserLogger;
import org.geysermc.geyser.network.bedrock.nethernet.NetherNetChannelInitialiser;
import org.geysermc.geyser.session.GeyserSession;
import org.junit.jupiter.api.Test;
import org.mockito.ArgumentCaptor;
import org.mockito.MockedConstruction;

import java.util.concurrent.TimeUnit;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;
import static org.mockito.Mockito.RETURNS_DEEP_STUBS;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.mockConstruction;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

/**
 * A player thread that dies must not take the whole transport with it, and the login that
 * triggers the replacement must report the loss.
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
 * The replacement happens inside the login path ({@code initSession} hands every session
 * {@code PlayerEventExecutors.next()}), so the report is asserted from there as well: a loss
 * that only the bind-time {@code getEventLoopGroup()} could describe is a loss nobody sees on
 * NetherNet, where the bind happens once at startup.
 */
class PlayerThreadTerminationTest {

    /**
     * The login path hands every session {@code PlayerEventExecutors.next()}, so the next
     * session must not be bound to a terminated executor.
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
            initialiser.shutdownPlayerThreads();
        }
    }

    /**
     * A login that finds the player threads dead replaces the group. That replacement is the
     * only observable moment of the loss on the NetherNet path (nothing calls the bind-time
     * getter again), so it must reach the transport's sever log with the identity of the
     * player threads it lost.
     * <p>
     * The mocked Geyser graph does not have to survive the whole login: {@code GeyserSession}
     * is constructed from registry caches whose static initialisers require a registered Geyser
     * API, so its construction is mocked out. {@code initSession} hands the session its executor
     * (and therefore reports the loss) before that constructor would run, and the report is what
     * this test asserts.
     */
    @Test
    void theLoginThatTriggersTheReplacementReportsTheLostPlayerThreads() throws Exception {
        GeyserImpl geyser = mock(GeyserImpl.class, RETURNS_DEEP_STUBS);
        GeyserLogger logger = mock(GeyserLogger.class);
        when(geyser.getLogger()).thenReturn(logger);
        BedrockServerSession bedrockSession = mock(BedrockServerSession.class, RETURNS_DEEP_STUBS);
        GeyserServerInitializer initialiser = new NetherNetChannelInitialiser(geyser);
        try {
            DefaultEventLoopGroup group = initialiser.getEventLoopGroup();
            int threads = group.executorCount();
            assertEquals(threads, terminateEveryThread(group),
                "the simulated fatal error must have terminated every player thread");

            // The production login path: initSession -> playerExecutors.next() -> replace().
            // GeyserSession is constructed from caches whose static initialisers need a fully
            // registered Geyser API, so it is mocked out: the hand-out (and the report) happens
            // before it and is the only thing this test asserts.
            try (MockedConstruction<GeyserSession> ignored = mockConstruction(GeyserSession.class)) {
                initialiser.initSession(bedrockSession);
            }

            ArgumentCaptor<String> report = ArgumentCaptor.forClass(String.class);
            verify(logger, times(1)).severe(report.capture());
            String line = report.getValue();
            assertTrue(line.contains("Geyser NetherNet player thread #1"),
                "the report of the loss must name the terminated player thread, got: " + line);
            assertTrue(line.contains("generation 1"),
                "the report must identify the generation that was lost, got: " + line);
        } finally {
            initialiser.shutdownPlayerThreads();
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

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

package org.geysermc.geyser.translator.protocol.bedrock.entity.player.input;

import net.kyori.adventure.key.Key;
import net.kyori.adventure.util.TriState;
import org.cloudburstmc.math.vector.Vector2f;
import org.cloudburstmc.math.vector.Vector3d;
import org.cloudburstmc.math.vector.Vector3f;
import org.geysermc.mcprotocollib.network.packet.Packet;
import org.cloudburstmc.protocol.bedrock.packet.PlayerAuthInputPacket;
import org.geysermc.api.Geyser;
import org.geysermc.geyser.GeyserImpl;
import org.geysermc.geyser.GeyserLogger;
import org.geysermc.geyser.api.GeyserApi;
import org.geysermc.geyser.api.util.Identifier;
import org.geysermc.geyser.entity.VanillaEntities;
import org.geysermc.geyser.entity.type.Entity;
import org.geysermc.geyser.entity.type.player.SessionPlayerEntity;
import org.geysermc.geyser.impl.IdentifierImpl;
import org.geysermc.geyser.level.BedrockDimension;
import org.geysermc.geyser.level.WorldManager;
import org.geysermc.geyser.level.block.type.Block;
import org.geysermc.geyser.level.physics.BoundingBox;
import org.geysermc.geyser.level.physics.CollisionManager;
import org.geysermc.geyser.level.physics.CollisionResult;
import org.geysermc.geyser.session.GeyserSession;
import org.geysermc.geyser.session.cache.BookEditCache;
import org.geysermc.geyser.session.cache.InputCache;
import org.geysermc.geyser.session.cache.SkullCache;
import org.geysermc.geyser.session.cache.TeleportCache;
import org.geysermc.geyser.session.cache.WorldBorder;
import org.geysermc.geyser.translator.collision.BlockCollision;
import org.geysermc.geyser.util.BlockUtils;
import org.geysermc.mcprotocollib.protocol.data.game.entity.player.GameMode;
import org.geysermc.mcprotocollib.protocol.packet.ingame.serverbound.player.ServerboundMovePlayerPosPacket;
import org.geysermc.mcprotocollib.protocol.packet.ingame.serverbound.player.ServerboundMovePlayerPosRotPacket;
import org.geysermc.mcprotocollib.protocol.packet.ingame.serverbound.player.ServerboundMovePlayerStatusOnlyPacket;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.mockito.Answers;
import org.mockito.MockedStatic;

import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.atomic.AtomicReference;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertTrue;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyBoolean;
import static org.mockito.ArgumentMatchers.anyFloat;
import static org.mockito.ArgumentMatchers.anyInt;
import static org.mockito.ArgumentMatchers.anyLong;
import static org.mockito.ArgumentMatchers.anyString;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.mockStatic;
import static org.mockito.Mockito.when;

/**
 * Fault C at the converter's own entry point: a converted player placed where the converter has no
 * terrain yet must not put a suspended sample on the wire.
 * <p>
 * The harness drives the real {@link BedrockMovePlayer#translate} entry point against a mocked session
 * whose world only knows the blocks it is told about, at 20 samples per second (one server tick each).
 * It models Paper 26.3's floating check from {@code ServerGamePacketListenerImpl}: {@code
 * clientIsFloating} is latched from the last forwarded movement sample (not on ground, not descending,
 * no block within the support distance) and {@code aboveGroundTickCount} is incremented once per
 * server tick while it stays latched; the backend disconnects with {@code multiplayer.disconnect.flying}
 * once it exceeds 80 ticks.
 * <p>
 * The previous implementation held only after a half-second grace and released the placement again
 * after a wall-clock budget, so the customer's 26 s backend stall and a terrain gap longer than
 * Paper's 4.05 s budget were both handed back to the backend and kicked. This harness pins the
 * shipped contract: suspended samples are withheld from the first one, the backend's counter is
 * disarmed once with a single falling sample, and the hold ends only on evidence (a provable
 * surface, or the client standing on something) - never on a timer.
 */
class BedrockMovePlayerSuspendedPlacementTest {

    /** Block state id the fake world and {@link BlockUtils#getCollision(int)} agree is a full block. */
    private static final int STONE = 7;
    private static final int SAMPLES_PER_SECOND = 20;
    /** Paper's {@code getMaximumFlyingTicks}: the disconnect happens on the 81st latched tick. */
    private static final int MAX_FLYING_TICKS = 80;
    /** The customer's stall: one Paper tick of 26375 ms (2026-10-31 22:24:36 log line). */
    private static final int STALL_SAMPLES = 26 * SAMPLES_PER_SECOND;
    /** Paper's support distance below the feet ({@code noBlocksAround}). */
    private static final double SUPPORT_DEPTH = 0.55;

    private static final float X = 5.5F;
    private static final float Z = 5.5F;

    private MockedStatic<BlockUtils> blockUtils;
    private MockedStatic<GeyserImpl> geyserImplStatics;
    private MockedStatic<Geyser> geyserStatics;
    private GeyserImpl geyserImpl;
    private float playerOffset;

    @BeforeEach
    void setUpStatics() {
        // Geyser's registries and entity types need GeyserImpl (pack directory) and the Geyser API
        // (identifier providers); no unit test bootstraps them, so install the minimal static
        // context the upstream GeyserMockContext uses.
        geyserImpl = mock(GeyserImpl.class, invocation -> {
            if (invocation.getMethod().getName().equals("provider")
                && invocation.getArgument(0, Class.class) == Identifier.class) {
                return new IdentifierImpl(Key.key(invocation.getArgument(1, String.class), invocation.getArgument(2, String.class)));
            }
            return Answers.RETURNS_DEFAULTS.answer(invocation);
        });
        when(geyserImpl.packDirectory()).thenReturn(Paths.get(System.getProperty("user.dir")));
        when(geyserImpl.configDirectory()).thenReturn(Paths.get(System.getProperty("user.dir")));
        when(geyserImpl.getLogger()).thenReturn(mock(GeyserLogger.class));

        geyserImplStatics = mockStatic(GeyserImpl.class);
        geyserImplStatics.when(GeyserImpl::getInstance).thenReturn(geyserImpl);
        geyserStatics = mockStatic(Geyser.class);
        geyserStatics.when(() -> Geyser.api(GeyserApi.class)).thenReturn(geyserImpl);
        playerOffset = VanillaEntities.PLAYER_ENTITY_OFFSET;

        // The collision registry is populated by the real bootstrap; the harness supplies the two
        // answers the placement logic needs (air has no collision, the fake stone is a full block).
        BlockCollision stoneCollision = mock(BlockCollision.class);
        when(stoneCollision.getBoundingBoxes()).thenReturn(new BoundingBox[]{
            new BoundingBox(Vector3d.from(0.5, 0.5, 0.5), 1.0, 1.0, 1.0)
        });
        blockUtils = mockStatic(BlockUtils.class);
        blockUtils.when(() -> BlockUtils.getCollision(anyInt()))
            .thenAnswer(invocation -> (int) invocation.getArgument(0) == STONE ? stoneCollision : null);
    }

    @AfterEach
    void closeStaticMocks() {
        if (blockUtils != null) {
            blockUtils.close();
        }
        if (geyserStatics != null) {
            geyserStatics.close();
        }
        if (geyserImplStatics != null) {
            geyserImplStatics.close();
        }
    }

    /**
     * The card's regression: terrain is unavailable for 1.5 s and is published well inside the
     * backend's 81-tick budget.
     */
    @Test
    @DisplayName("delayed terrain is bridged without advancing the floating counter, then grounded")
    void delayedTerrainIsBridgedAndGrounded() {
        Harness harness = new Harness(geyserImpl, playerOffset);
        // One full block three below the feet, published from sample 30 (1.5 s).
        harness.publishBlock(5, -21, 5, 30);

        harness.run(SAMPLES_PER_SECOND * 8);

        assertNull(harness.backend.kickTick,
            "the converted player was reported as flying for 81 ticks (kick at tick " + harness.backend.kickTick + ")");
        assertEquals(0, harness.airborneSamplesSince(0),
            "a suspended movement sample reached the backend before the column arrived");

        // The placement is grounded onto the published surface and the client adopts it.
        assertNotNull(harness.groundingTeleportY, "the placement was never grounded once terrain was published");
        assertEquals(-20.0, harness.groundingTeleportY, 1.0E-4);
        assertEquals(-20.0, harness.clientFeetY, 1.0E-4, "the client did not adopt the grounded position");
        assertTrue(harness.groundedBySample >= 30 && harness.groundedBySample <= 30 + SAMPLES_PER_SECOND,
            "grounding was not applied promptly after the terrain was published (sample " + harness.groundedBySample + ")");
    }

    /**
     * A held placement puts no suspended sample on the wire at all - not even during a grace period.
     * The only thing the converter sends is the single falling sample that disarms a counter the
     * placement itself may already have armed.
     */
    @Test
    @DisplayName("a held placement never puts a suspended sample on the wire")
    void heldPlacementSendsNoSuspendedSample() {
        Harness harness = new Harness(geyserImpl, playerOffset);
        int arrival = SAMPLES_PER_SECOND * 8;
        harness.publishBlock(5, -21, 5, arrival);

        harness.run(SAMPLES_PER_SECOND * 12);

        assertNull(harness.backend.kickTick, "the converted player was reported as flying (kick at tick "
            + harness.backend.kickTick + ") before the terrain arrived");
        for (Sample sample : harness.samples) {
            assertFalse(sample.latchesFloating(), "a sample that arms Paper's floating counter reached the"
                + " backend at sample " + sample.index() + ", before the column arrived at " + arrival);
        }
        // Exactly one falling sample: the counter disarm at the start of the hold.
        List<Sample> beforeArrival = harness.samplesBefore(arrival);
        assertEquals(1, beforeArrival.size(), "expected only the falling disarm sample before arrival, got "
            + beforeArrival);
        assertTrue(beforeArrival.get(0).descending(), "the disarm sample was not falling: " + beforeArrival.get(0));

        assertNotNull(harness.groundingTeleportY, "the placement was never grounded once the column arrived");
        assertEquals(-20.0, harness.groundingTeleportY, 1.0E-4);
        assertEquals(-20.0, harness.clientFeetY, 1.0E-4, "the client did not adopt the grounded position");
    }

    /**
     * The same placement with a terrain gap longer than Paper's own 81-tick (4.05 s) budget: the
     * shipped converter must bridge it instead of handing the placement back on a timer.
     */
    @Test
    @DisplayName("a terrain gap longer than the flying budget still ends on the surface")
    void terrainGapBeyondFlyingBudgetEndsOnTheSurface() {
        Harness harness = new Harness(geyserImpl, playerOffset);
        harness.publishBlock(5, -21, 5, 90);

        harness.run(SAMPLES_PER_SECOND * 8);

        assertNull(harness.backend.kickTick, "disconnected after " + harness.backend.kickTick
            + " ticks of a terrain gap that the converter could have bridged");
        assertEquals(0, harness.airborneSamplesSince(0),
            "airborne movement samples were still forwarded while the column was missing");
        assertNotNull(harness.groundingTeleportY, "the placement was never grounded once terrain was published");
        assertEquals(-20.0, harness.groundingTeleportY, 1.0E-4);
        assertEquals(-20.0, harness.clientFeetY, 1.0E-4);
    }

    /**
     * geyserlite runs in its own process, so its 20 Hz sample clock keeps running while the backend is
     * frozen. The customer's Paper produced a 26375 ms tick with TPS 0.1 and his placement-to-kick
     * window was still only 6 s. Nothing in the hold may be measured in wall-clock samples.
     */
    @Test
    @DisplayName("a 26 s backend stall with unprovable terrain does not hand the placement back")
    void aBackendStallDoesNotReleaseTheHeldPlacement() {
        Harness harness = new Harness(geyserImpl, playerOffset);
        // The column only arrives after the stall: nothing is provable while the backend is frozen.
        harness.publishBlock(5, -21, 5, 20 + STALL_SAMPLES + 20);
        harness.stall(20, 20 + STALL_SAMPLES);

        harness.run(SAMPLES_PER_SECOND * 40);

        assertNull(harness.backend.kickTick, "the converted player was disconnected for flying (tick "
            + harness.backend.kickTick + ") after a 26 s backend stall");
        assertEquals(0, harness.airborneSamplesSince(0),
            "airborne movement samples were handed to a frozen backend and latched on resume");
        assertNotNull(harness.groundingTeleportY, "the placement was never grounded once the column arrived");
        assertEquals(-20.0, harness.groundingTeleportY, 1.0E-4);
        assertEquals(-20.0, harness.clientFeetY, 1.0E-4, "the client did not adopt the grounded position");
    }

    /** The same stall, but longer than the customer's: 39 s. */
    @Test
    @DisplayName("a 39 s backend stall is still not evidence of a void")
    void aLongerBackendStallIsStillBridged() {
        Harness harness = new Harness(geyserImpl, playerOffset);
        harness.publishBlock(5, -21, 5, 20 + 39 * SAMPLES_PER_SECOND + 20);
        harness.stall(20, 20 + 39 * SAMPLES_PER_SECOND);

        harness.run(SAMPLES_PER_SECOND * 60);

        assertNull(harness.backend.kickTick, "disconnected for flying (tick " + harness.backend.kickTick + ")");
        assertEquals(0, harness.airborneSamplesSince(0),
            "airborne movement samples were handed to a frozen backend");
        assertNotNull(harness.groundingTeleportY, "the placement was never grounded once the column arrived");
        assertEquals(-20.0, harness.clientFeetY, 1.0E-4);
    }

    /**
     * A placement is the backend's own position (initial spawn, server switch, respawn), so the surface
     * under it is the answer even when it is farther down than the window used mid-session. Released
     * geyserlite grounds a placement 42 blocks above the ground (kanban t_1e9c9090); the rework that
     * introduced the UNKNOWN distinction must not narrow that back to the mid-session window.
     */
    @Test
    @DisplayName("a pending placement grounds onto a surface 42 blocks down")
    void pendingPlacementGroundsOntoSurfaceBelowThePlainWindow() {
        Harness harness = new Harness(geyserImpl, playerOffset);
        harness.publishBlock(5, -60, 5, 0);

        harness.run(SAMPLES_PER_SECOND * 3);

        assertNotNull(harness.groundingTeleportY, "did not ground a pending placement onto the surface below it");
        assertEquals(-59.0, harness.groundingTeleportY, 1.0E-4);
        assertNull(harness.backend.kickTick, "the placement was kicked instead of grounded");
    }

    /**
     * A surface farther down than the mid-session window is not a placement the converter may silently
     * relocate to while the player is already playing.
     */
    @Test
    @DisplayName("a settled session keeps the narrow search window")
    void settledSessionKeepsTheNarrowWindow() {
        Harness harness = new Harness(geyserImpl, playerOffset);
        harness.placementPending.set(false);
        harness.publishBlock(5, -60, 5, 0);

        harness.run(SAMPLES_PER_SECOND * 3);

        assertNull(harness.groundingTeleportY, "teleported a settled player onto a distant surface");
    }

    /**
     * A descending placement is never the suspended, motionless state; it must reach the backend
     * unmodified so gravity, fall damage and the void behave as for a Java client.
     */
    @Test
    @DisplayName("a descending placement is forwarded untouched")
    void descendingPlacementIsForwarded() {
        Harness harness = new Harness(geyserImpl, playerOffset);
        harness.deltaY = -0.08F;

        harness.run(SAMPLES_PER_SECOND * 3);

        assertNull(harness.groundingTeleportY, "teleported a placement that was genuinely falling");
        assertFalse(harness.samples.isEmpty(), "a falling placement was not forwarded at all");
        for (Sample sample : harness.samples) {
            assertFalse(sample.onGround(), "reported a descending placement as supported");
            assertFalse(sample.latchesFloating(), "a descending sample was modelled as floating");
        }
    }

    /**
     * Everything that can legitimately leave the ground keeps its old behaviour: no rewriting of the
     * samples, no teleport.
     */
    @Test
    @DisplayName("creative, spectator, flying, vehicle, water and climbing states are untouched")
    void exemptStatesAreUntouched() {
        for (Exemption exemption : Exemption.values()) {
            Harness harness = new Harness(geyserImpl, playerOffset);
            harness.publishBlock(5, -21, 5, 0);
            exemption.apply(harness);

            harness.run(SAMPLES_PER_SECOND * 3);

            assertNull(harness.groundingTeleportY, exemption + ": teleported an exempt state");
            if (exemption != Exemption.VEHICLE) {
                // A vehicle's movement is not a position sample of its own; every other exempt state
                // still reaches the backend unchanged.
                assertFalse(harness.samples.isEmpty(), exemption + ": withheld an exempt state's samples");
            }
        }
    }

    /**
     * A column the world really has sent, with nothing collidable in it, is a real void: the hold keeps
     * the sample off the wire so the backend's counter never arms. The backend's verdict that the
     * column is empty is not permission to hand it a suspended sample it cannot accept.
     */
    @Test
    @DisplayName("a loaded but empty column is held instead of handed to the backend")
    void aProvablyEmptyColumnIsHeld() {
        Harness harness = new Harness(geyserImpl, playerOffset);
        harness.publishColumn(5, 5, 0);

        harness.run(SAMPLES_PER_SECOND * 12);

        assertNull(harness.groundingTeleportY, "grounded a placement with no surface to stand on");
        assertEquals(0, harness.airborneSamplesSince(0),
            "a provably empty column handed the suspended sample to the backend");
        assertNull(harness.backend.kickTick, "the converted player was disconnected for flying");
    }

    /**
     * Terrain that never arrives at all: the hold does not expire, and nothing suspended reaches the
     * backend while it lasts.
     */
    @Test
    @DisplayName("terrain that never arrives keeps the sample off the wire")
    void unknownTerrainStaysHeld() {
        Harness harness = new Harness(geyserImpl, playerOffset);

        harness.run(SAMPLES_PER_SECOND * 30);

        assertEquals(0, harness.airborneSamplesSince(0),
            "a suspended sample reached the backend while the column was unknown");
        assertNull(harness.backend.kickTick, "the converted player was disconnected for flying");
        assertNull(harness.groundingTeleportY, "grounded a placement with no terrain to prove");
    }

    /**
     * The ordinary end of a hold (other than grounding): the column arrives with a surface exactly
     * level with the feet, so the client's own sample stops being suspended and is forwarded again.
     */
    @Test
    @DisplayName("a supported arrival releases the hold without a teleport")
    void aSupportedArrivalReleasesTheHoldWithoutTeleport() {
        Harness harness = new Harness(geyserImpl, playerOffset);
        // A full block whose top is level with the placement's feet: the client is supported again.
        harness.publishBlock(5, -19, 5, SAMPLES_PER_SECOND * 2);

        harness.run(SAMPLES_PER_SECOND * 5);

        assertNull(harness.groundingTeleportY, "a placement that is already supported must not be teleported");
        assertTrue(harness.firstSampleSince(SAMPLES_PER_SECOND * 2) > 0,
            "the hold never released after the column arrived");
    }

    private enum Exemption {
        CREATIVE {
            @Override
            void apply(Harness harness) {
                harness.gameMode.set(GameMode.CREATIVE);
            }
        },
        SPECTATOR {
            @Override
            void apply(Harness harness) {
                harness.gameMode.set(GameMode.SPECTATOR);
            }
        },
        FLYING {
            @Override
            void apply(Harness harness) {
                harness.flying.set(true);
            }
        },
        VEHICLE {
            @Override
            void apply(Harness harness) {
                harness.hasVehicle.set(true);
            }
        },
        WATER {
            @Override
            void apply(Harness harness) {
                harness.touchingWater.set(true);
            }
        },
        CLIMBING {
            @Override
            void apply(Harness harness) {
                harness.onClimbableBlock.set(true);
            }
        };

        abstract void apply(Harness harness);
    }

    /**
     * One movement sample as the backend saw it.
     * <p>
     * {@code latchesFloating} models what was measured on Paper 26.3-205: the latch is <b>geometric</b>.
     * A converter that reported {@code isOnGround = true} every second at a placement with no support
     * was still disconnected, i.e. the client's own on-ground flag does not clear
     * {@code clientIsFloating}. What matters is the backend's view of the position: airborne, not
     * descending, and nothing within 0.55 below the feet.
     */
    private record Sample(int index, boolean onGround, boolean supportedByWorld, boolean descending) {
        /** Paper's {@code clientIsFloating} latch: airborne, not descending, nothing under it. */
        boolean latchesFloating() {
            return !supportedByWorld && !descending;
        }
    }

    /**
     * Paper 26.3 {@code ServerGamePacketListenerImpl}: {@code clientIsFloating} latches from the last
     * movement sample, {@code aboveGroundTickCount} advances one per server tick while latched, and
     * the disconnect happens once it passes 80.
     */
    private static final class PaperFloatingCounter {
        private boolean clientIsFloating;
        private int aboveGroundTicks;
        private int tick;
        Integer kickTick;
        /** Set while the server's main thread is frozen: samples queue up instead of being applied. */
        boolean frozen;
        private Sample queuedWhileFrozen;

        void movementSample(Sample sample) {
            if (frozen) {
                // Paper's socket keeps reading; the movement sample is only applied by the main thread,
                // so the latch is taken from the last sample that was queued when the stall ends.
                queuedWhileFrozen = sample;
                return;
            }
            this.clientIsFloating = sample.latchesFloating();
        }

        /** The backend resumes: everything the socket buffered is applied in one go, then ticking. */
        void resume() {
            frozen = false;
            if (queuedWhileFrozen != null) {
                this.clientIsFloating = queuedWhileFrozen.latchesFloating();
                queuedWhileFrozen = null;
            }
        }

        void serverTick() {
            tick++;
            if (clientIsFloating) {
                if (++aboveGroundTicks > MAX_FLYING_TICKS && kickTick == null) {
                    kickTick = tick;
                }
            } else {
                aboveGroundTicks = 0;
            }
        }
    }

    private static final class Harness {
        final GeyserSession session = mock(GeyserSession.class);
        final GeyserLogger logger = mock(GeyserLogger.class);
        /** Every {@code debug} line the converter emitted during this scenario. */
        final List<String> debugLines = new ArrayList<>();
        /** Every {@code warning} line the converter emitted during this scenario. */
        final List<String> warningLines = new ArrayList<>();
        final SessionPlayerEntity entity = mock(SessionPlayerEntity.class);
        final CollisionManager collisionManager = mock(CollisionManager.class);
        final InputCache inputCache = mock(InputCache.class);
        final WorldManager worldManager = mock(WorldManager.class);
        final PaperFloatingCounter backend = new PaperFloatingCounter();

        final List<int[]> publishedBlocks = new ArrayList<>(); // x, y, z, firstVisibleSample
        final List<int[]> loadedColumns = new ArrayList<>(); // chunkX, chunkZ, firstLoadedSample
        final List<Sample> samples = new ArrayList<>();

        final AtomicInteger suspendedSamples = new AtomicInteger();
        final AtomicBoolean placementPending = new AtomicBoolean(true);
        final AtomicLong holdSince = new AtomicLong();
        final AtomicLong holdWarnedAt = new AtomicLong();
        final AtomicBoolean entityOnGround = new AtomicBoolean(false);
        final AtomicBoolean collidingVertically = new AtomicBoolean(false);
        final AtomicReference<TeleportCache> unconfirmedTeleport = new AtomicReference<>();
        final AtomicReference<GameMode> gameMode = new AtomicReference<>(GameMode.SURVIVAL);
        final AtomicBoolean flying = new AtomicBoolean(false);
        final AtomicBoolean hasVehicle = new AtomicBoolean(false);
        final AtomicBoolean touchingWater = new AtomicBoolean(false);
        final AtomicBoolean onClimbableBlock = new AtomicBoolean(false);
        final AtomicReference<Entity> vehicle = new AtomicReference<>();

        final float playerOffset;
        int sample;
        double clientFeetY = -18.0;
        double clientX = X;
        /** Where the converter last told the backend the player is (the real entity position). */
        double entityX = X;
        double entityFeetY = -18.0;
        /** Y of the last position packet the backend received, for its own "descending" reading. */
        /** Seeded with the placement's position: the backend placed the player there itself. */
        Double lastReportedFeetY = -18.0;
        float deltaY;
        float deltaX;
        /** Backend stall window in sample indices: the server neither ticks nor sends anything. */
        int stallFrom = Integer.MAX_VALUE;
        int stallUntil = -1;
        boolean stalled;
        Double groundingTeleportY;
        Double pendingTeleportFeetY;
        int groundedBySample = -1;
        // A position reminder is due every 20 samples while the converted client does not move.
        private int ticksSinceMovePacket;

        Harness(GeyserImpl geyser, float playerOffset) {
            this.playerOffset = playerOffset;
            when(geyser.getWorldManager()).thenReturn(worldManager);
            when(geyser.getLogger()).thenReturn(logger);
            doAnswer(invocation -> {
                debugLines.add(invocation.getArgument(0, String.class));
                return null;
            }).when(logger).debug(anyString());
            doAnswer(invocation -> {
                warningLines.add(invocation.getArgument(0, String.class));
                return null;
            }).when(logger).warning(anyString());

            when(session.getGeyser()).thenReturn(geyser);
            when(session.bedrockUsername()).thenReturn("labclient");
            when(session.isSpawned()).thenReturn(true);
            when(session.getPlayerEntity()).thenReturn(entity);
            when(session.getGameMode()).thenAnswer(invocation -> gameMode.get());
            when(session.isFlying()).thenAnswer(invocation -> flying.get());
            when(session.isNoClip()).thenReturn(false);
            when(session.isPlacementPending()).thenAnswer(invocation -> placementPending.get());
            doAnswer(invocation -> {
                placementPending.set(invocation.getArgument(0));
                return null;
            }).when(session).setPlacementPending(anyBoolean());
            when(session.getSuspendedAirborneSamples()).thenAnswer(invocation -> suspendedSamples.get());
            doAnswer(invocation -> {
                suspendedSamples.set(invocation.getArgument(0));
                return null;
            }).when(session).setSuspendedAirborneSamples(anyInt());
            when(session.getSuspendedHoldSince()).thenAnswer(invocation -> holdSince.get());
            doAnswer(invocation -> {
                holdSince.set(invocation.getArgument(0));
                return null;
            }).when(session).setSuspendedHoldSince(anyLong());
            when(session.getSuspendedHoldWarnedAt()).thenAnswer(invocation -> holdWarnedAt.get());
            doAnswer(invocation -> {
                holdWarnedAt.set(invocation.getArgument(0));
                return null;
            }).when(session).setSuspendedHoldWarnedAt(anyLong());
            when(session.getBedrockDimension()).thenReturn(BedrockDimension.OVERWORLD);
            when(session.getCollisionManager()).thenReturn(collisionManager);
            when(session.getInputCache()).thenReturn(inputCache);
            when(session.getBookEditCache()).thenReturn(mock(BookEditCache.class));
            when(session.getSkullCache()).thenReturn(mock(SkullCache.class));
            when(session.getWorldBorder()).thenReturn(mock(WorldBorder.class));

            when(collisionManager.isPlayerTouchingWater()).thenAnswer(invocation -> touchingWater.get());
            when(collisionManager.adjustPositionForBedrock(any())).thenAnswer(invocation -> invocation.getArgument(0));
            when(collisionManager.adjustBedrockPosition(any(), anyBoolean(), anyBoolean()))
                .thenAnswer(invocation -> {
                    Vector3f position = invocation.getArgument(0);
                    return new CollisionResult(Vector3d.from(position.getX(),
                        position.getY() - playerOffset, position.getZ()), TriState.NOT_SET);
                });

            when(inputCache.shouldSendPositionReminder()).thenAnswer(invocation -> ++ticksSinceMovePacket >= 20);
            doAnswer(invocation -> {
                ticksSinceMovePacket = 0;
                return null;
            }).when(inputCache).markPositionPacketSent();
            doAnswer(invocation -> null).when(inputCache).setLastHorizontalCollision(anyBoolean());
            when(inputCache.lastHorizontalCollision()).thenReturn(false);

            when(entity.bedrockPosition()).thenAnswer(invocation -> Vector3f.from((float) entityX, (float) entityFeetY + playerOffset, Z));
            when(entity.getBedPosition()).thenReturn(null);
            when(entity.getJavaYaw()).thenReturn(0.0F);
            when(entity.getYaw()).thenReturn(0.0F);
            when(entity.getHeadYaw()).thenReturn(0.0F);
            when(entity.getPitch()).thenReturn(0.0F);
            when(entity.getVehicle()).thenAnswer(invocation -> hasVehicle.get() ? vehicle() : null);
            when(entity.isOnClimbableBlock()).thenAnswer(invocation -> onClimbableBlock.get());
            when(entity.isCollidingVertically()).thenAnswer(invocation -> collidingVertically.get());
            when(entity.getLastTickEndVelocity()).thenReturn(Vector3f.ZERO);
            when(entity.isOnGround()).thenAnswer(invocation -> entityOnGround.get());
            when(entity.getLeftParrot()).thenReturn(null);
            when(entity.getRightParrot()).thenReturn(null);
            doAnswer(invocation -> {
                entityOnGround.set(invocation.getArgument(0));
                return null;
            }).when(entity).setOnGround(anyBoolean());
            doAnswer(invocation -> {
                collidingVertically.set(invocation.getArgument(0));
                return null;
            }).when(entity).setCollidingVertically(anyBoolean());
            doAnswer(invocation -> {
                Vector3f position = invocation.getArgument(0);
                boolean teleported = invocation.getArgument(4);
                if (groundingTeleportY == null && teleported
                    && Math.abs(position.getY() - (float) clientFeetY) > 0.05) {
                    groundingTeleportY = (double) position.getY();
                    groundedBySample = sample;
                }
                pendingTeleportFeetY = (double) position.getY();
                return null;
            }).when(entity).moveAbsolute(any(), anyFloat(), anyFloat(), anyBoolean(), anyBoolean());

            when(session.getUnconfirmedTeleport()).thenAnswer(invocation -> unconfirmedTeleport.get());
            doAnswer(invocation -> {
                unconfirmedTeleport.set(invocation.getArgument(0));
                return null;
            }).when(session).setUnconfirmedTeleport(any());
            doAnswer(invocation -> {
                // The client adopts the teleport it was sent, exactly like a real Bedrock client, and
                // its next auth input reports that position.
                if (pendingTeleportFeetY != null) {
                    clientFeetY = pendingTeleportFeetY;
                    pendingTeleportFeetY = null;
                }
                unconfirmedTeleport.set(null);
                return null;
            }).when(session).confirmTeleport(any());

            doAnswer(invocation -> {
                Packet sent = invocation.getArgument(0);
                Boolean onGround = onGroundOf(sent);
                if (onGround == null) {
                    return null;
                }
                Double reportedY = positionOf(sent);
                double feetY = reportedY != null ? reportedY : clientFeetY;
                // Paper reads "descending" from the positions it receives, not from the client's flag.
                boolean descending = feetY < lastReportedFeetY - 1.0E-3;
                Sample recorded = new Sample(sample, onGround, supportedAt(feetY), descending);
                samples.add(recorded);
                backend.movementSample(recorded);
                if (reportedY != null) {
                    lastReportedFeetY = feetY;
                    if (Math.abs(feetY - clientFeetY) < 1.0E-6) {
                        // The converter adopts the position it just reported as the entity position.
                        entityX = clientX;
                        entityFeetY = clientFeetY;
                    }
                }
                return null;
            }).when(session).sendDownstreamGamePacket(any());

            when(worldManager.getBlockAt(any(), anyInt(), anyInt(), anyInt()))
                .thenAnswer(invocation -> visible(invocation.getArgument(1), invocation.getArgument(2),
                    invocation.getArgument(3)) ? STONE : Block.JAVA_AIR_ID);
            when(worldManager.hasBlockData(any(), anyInt(), anyInt()))
                .thenAnswer(invocation -> columnLoaded(invocation.getArgument(1), invocation.getArgument(2)));
        }

        private Entity vehicle() {
            if (vehicle.get() == null) {
                vehicle.set(mock(Entity.class));
            }
            return vehicle.get();
        }

        void publishBlock(int x, int y, int z, int firstVisibleSample) {
            publishedBlocks.add(new int[]{x, y, z, firstVisibleSample});
            // A block can only be read once its column has been sent.
            publishColumn(x, z, firstVisibleSample);
        }

        /** The server streams this column from {@code firstLoadedSample} on (it may contain no blocks). */
        void publishColumn(int blockX, int blockZ, int firstLoadedSample) {
            loadedColumns.add(new int[]{blockX >> 4, blockZ >> 4, firstLoadedSample});
        }

        /** The backend stops ticking and sending anything between these sample indices. */
        void stall(int fromSample, int untilSample) {
            stallFrom = fromSample;
            stallUntil = untilSample;
        }

        private boolean columnLoaded(int x, int z) {
            for (int[] column : loadedColumns) {
                if (column[0] == (x >> 4) && column[1] == (z >> 4) && sample >= column[2]) {
                    return true;
                }
            }
            return false;
        }

        private boolean visible(int x, int y, int z) {
            for (int[] block : publishedBlocks) {
                if (block[0] == x && block[1] == y && block[2] == z && sample >= block[3]) {
                    return true;
                }
            }
            return false;
        }

        /** Paper's {@code noBlocksAround}: a collidable block within 0.55 below the feet. */
        private boolean supportedAt(double feetY) {
            int minX = (int) Math.floor(clientX - 0.3);
            int maxX = (int) Math.floor(clientX + 0.3);
            int minZ = (int) Math.floor(Z - 0.3);
            int maxZ = (int) Math.floor(Z + 0.3);
            int minY = (int) Math.floor(feetY - SUPPORT_DEPTH);
            int maxY = (int) Math.floor(feetY);
            for (int x = minX; x <= maxX; x++) {
                for (int z = minZ; z <= maxZ; z++) {
                    for (int y = minY; y <= maxY; y++) {
                        if (visible(x, y, z)) {
                            return true;
                        }
                    }
                }
            }
            return false;
        }

        void run(int ticks) {
            for (int i = 0; i < ticks; i++) {
                boolean frozen = i >= stallFrom && i < stallUntil;
                if (frozen != stalled) {
                    stalled = frozen;
                    backend.frozen = frozen;
                    if (!frozen) {
                        backend.resume();
                    }
                }
                if (deltaX != 0.0F) {
                    // The client keeps walking while it hovers: its samples are due every tick.
                    clientX += deltaX;
                }
                if (deltaY != 0.0F) {
                    // A falling client's own position moves, which is what the backend latches on.
                    clientFeetY += deltaY;
                }
                sample = i;
                BedrockMovePlayer.translate(session, packet());
                if (!stalled) {
                    backend.serverTick();
                }
            }
        }

        /** Samples the backend would latch as floating, after the given sample index. */
        int airborneSamplesSince(int firstSample) {
            int count = 0;
            for (Sample sample : samples) {
                if (sample.index() >= firstSample && sample.latchesFloating()) {
                    count++;
                }
            }
            return count;
        }

        int firstSampleSince(int firstSample) {
            for (Sample sample : samples) {
                if (sample.index() >= firstSample) {
                    return sample.index();
                }
            }
            return -1;
        }

        List<Sample> samplesBefore(int index) {
            List<Sample> before = new ArrayList<>();
            for (Sample sample : samples) {
                if (sample.index() < index) {
                    before.add(sample);
                }
            }
            return before;
        }

        private PlayerAuthInputPacket packet() {
            PlayerAuthInputPacket packet = new PlayerAuthInputPacket();
            packet.setPosition(Vector3f.from((float) clientX, (float) clientFeetY + playerOffset, Z));
            packet.setDelta(Vector3f.from(deltaX, deltaY, 0.0F));
            packet.setRotation(Vector3f.from(0.0F, 0.0F, 0.0F));
            packet.setInteractRotation(Vector2f.ZERO);
            packet.setTick(sample);
            return packet;
        }

        private static Boolean onGroundOf(Packet packet) {
            if (packet instanceof ServerboundMovePlayerPosPacket pos) {
                return pos.isOnGround();
            }
            if (packet instanceof ServerboundMovePlayerPosRotPacket posRot) {
                return posRot.isOnGround();
            }
            if (packet instanceof ServerboundMovePlayerStatusOnlyPacket statusOnly) {
                return statusOnly.isOnGround();
            }
            return null;
        }

        private static Double positionOf(Packet packet) {
            if (packet instanceof ServerboundMovePlayerPosPacket pos) {
                return pos.getY();
            }
            if (packet instanceof ServerboundMovePlayerPosRotPacket posRot) {
                return posRot.getY();
            }
            return null;
        }
    }
}

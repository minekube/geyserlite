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

import net.kyori.adventure.util.TriState;
import org.geysermc.api.Geyser;
import org.geysermc.geyser.api.GeyserApi;
import org.geysermc.geyser.impl.IdentifierImpl;
import net.kyori.adventure.key.Key;
import org.mockito.Answers;
import org.mockito.invocation.InvocationOnMock;
import org.cloudburstmc.math.vector.Vector2f;
import org.cloudburstmc.math.vector.Vector3d;
import org.cloudburstmc.math.vector.Vector3f;
import org.cloudburstmc.protocol.bedrock.packet.PlayerAuthInputPacket;
import org.geysermc.geyser.GeyserImpl;
import org.geysermc.geyser.GeyserLogger;
import org.geysermc.geyser.api.util.Identifier;
import org.geysermc.geyser.entity.VanillaEntities;
import org.geysermc.geyser.entity.type.Entity;
import org.geysermc.geyser.entity.type.player.SessionPlayerEntity;
import org.geysermc.geyser.level.BedrockDimension;
import org.geysermc.geyser.level.WorldManager;
import org.geysermc.geyser.level.block.type.Block;
import org.geysermc.geyser.level.physics.BoundingBox;
import org.geysermc.geyser.level.physics.CollisionManager;
import org.geysermc.geyser.level.physics.CollisionResult;
import org.geysermc.geyser.session.GeyserSession;
import org.geysermc.geyser.session.cache.BookEditCache;
import org.geysermc.geyser.session.cache.ChunkCache;
import org.geysermc.geyser.session.cache.InputCache;
import org.geysermc.geyser.session.cache.SkullCache;
import org.geysermc.geyser.session.cache.TeleportCache;
import org.geysermc.geyser.session.cache.WorldBorder;
import org.geysermc.geyser.translator.collision.BlockCollision;
import org.geysermc.geyser.util.BlockUtils;
import org.geysermc.geyser.util.PlacementGrounding;
import org.geysermc.mcprotocollib.network.packet.Packet;
import org.geysermc.mcprotocollib.protocol.data.game.entity.player.GameMode;
import org.geysermc.mcprotocollib.protocol.packet.ingame.serverbound.player.ServerboundMovePlayerPosPacket;
import org.geysermc.mcprotocollib.protocol.packet.ingame.serverbound.player.ServerboundMovePlayerPosRotPacket;
import org.geysermc.mcprotocollib.protocol.packet.ingame.serverbound.player.ServerboundMovePlayerStatusOnlyPacket;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
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
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.mockStatic;
import static org.mockito.Mockito.when;

/**
 * Fault C, delayed-terrain arm: a converted player whose placement has no provable terrain under it
 * must not keep the Java backend's floating counter running.
 * <p>
 * The harness drives the real {@link BedrockMovePlayer#translate} entry point against a mocked
 * session whose world only knows the blocks it is told about, at 20 samples per second (one server
 * tick each). It also models Paper 26.3's own floating check from
 * {@code ServerGamePacketListenerImpl}: {@code clientIsFloating} is latched from the last movement
 * sample (not on ground, not descending, no block within the support distance) and
 * {@code aboveGroundTickCount} is incremented once per server tick while it stays latched; the
 * backend disconnects with {@code multiplayer.disconnect.flying} once it exceeds 80 ticks.
 * <p>
 * RED on the released converter: the no-surface branch forwards the suspended sample, the backend
 * latches and disconnects after 81 ticks. GREEN: the sample is reported as supported while terrain
 * is not yet provable, the placement is grounded as soon as it is, and the counter never reaches the
 * limit.
 */
class BedrockMovePlayerSuspendedPlacementTest {

    /** Block state id the fake world and {@link BlockUtils#getCollision(int)} agree is a full block. */
    private static final int STONE = 7;
    private static final int SAMPLES_PER_SECOND = 20;
    /** Paper's {@code getMaximumFlyingTicks}: the disconnect happens on the 81st latched tick. */
    private static final int MAX_FLYING_TICKS = 80;
    /** The converter's own grace before it starts holding an unprovable placement. */
    private static final int GRACE_SAMPLES = SAMPLES_PER_SECOND / 2;
    /** The customer's stall: one Paper tick of 26375 ms (2026-10-31 22:24:36 log line). */
    private static final int STALL_SAMPLES = 26 * SAMPLES_PER_SECOND;
    /** The hold's budget in backend-alive time: 60 s, >= 5x the observed stall and ~15x 81 ticks. */
    private static final int HOLD_ALIVE_CAP_SAMPLES = 60 * SAMPLES_PER_SECOND;
    /** Sample index at which the consecutive-suspended-sample counter reaches its threshold. */
    private static final int HOLD_START_SAMPLE = GRACE_SAMPLES - 1;
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
     * The card's regression: terrain is unavailable for longer than the 0.5 s latch and is published
     * 1.5 s after placement, well inside the backend's 81-tick budget.
     */
    @Test
    @DisplayName("delayed terrain is bridged without advancing the floating counter, then grounded")
    void delayedTerrainIsBridgedAndGrounded() {
        Harness harness = new Harness(geyserImpl, playerOffset);
        // One full block three below the feet, published from sample 30 (1.5 s).
        harness.publishBlock(5, -21, 5, 30);

        harness.run(SAMPLES_PER_SECOND * 8);

        // RED on the released converter: the suspended sample is forwarded, Paper latches it and
        // disconnects after 81 ticks. The fixed converter never lets the counter run.
        assertNull(harness.backend.kickTick,
            "the converted player was reported as flying for 81 ticks (kick at tick " + harness.backend.kickTick + ")");
        assertEquals(0, harness.airborneSamplesSince(GRACE_SAMPLES),
            "airborne movement samples were still forwarded after the 0.5 s grace");

        // The placement is grounded onto the published surface and the client adopts it.
        assertNotNull(harness.groundingTeleportY, "the placement was never grounded once terrain was published");
        assertEquals(-20.0, harness.groundingTeleportY, 1.0E-4);
        assertEquals(-20.0, harness.clientFeetY, 1.0E-4, "the client did not adopt the grounded position");
        assertTrue(harness.groundedBySample >= 30 && harness.groundedBySample <= 30 + SAMPLES_PER_SECOND,
            "grounding was not applied promptly after the terrain was published (sample " + harness.groundedBySample + ")");
    }

    /**
     * Round-5 finding, the reason the shipped hold cannot save the customer: a held placement may not
     * put <i>any</i> sample on the wire. Paper 26.3's {@code clientIsFloating} does not read the
     * client's on-ground flag, so the 0.5 s grace and the 1 Hz "supported" reminder are themselves what
     * arms the 81-tick counter at an unsupported height. In the lab the v0.7.3 converter sent exactly
     * that reminder ({@code ServerboundMovePlayerPos} at {@code y=103.0}, flag {@code 0x01}) once a
     * second and was disconnected at tp+4.7 s with 8 s of terrain still outstanding.
     * <p>
     * RED on the shipped converter: the grace samples and the reminder are forwarded.
     */
    @Test
    @DisplayName("a held placement puts no movement sample on the wire")
    void heldPlacementSendsNothingToTheBackend() {
        Harness harness = new Harness(geyserImpl, playerOffset);
        // Terrain arrives 8 s late: the arm-5 cell where all three builds were kicked at tp+4..5 s.
        int arrival = SAMPLES_PER_SECOND * 8;
        harness.publishBlock(5, -21, 5, arrival);

        harness.run(SAMPLES_PER_SECOND * 12);

        assertNull(harness.backend.kickTick, "the converted player was reported as flying (kick at tick "
            + harness.backend.kickTick + ") before the terrain arrived");
        for (Sample sample : harness.samples) {
            assertTrue(sample.index() >= arrival, "a movement sample reached the backend at sample "
                + sample.index() + ", before the column arrived at " + arrival);
        }

        // ... and the arrival of the column ends the silence on the surface itself.
        assertNotNull(harness.groundingTeleportY, "the placement was never grounded once the column arrived");
        assertEquals(-20.0, harness.groundingTeleportY, 1.0E-4);
        assertEquals(-20.0, harness.clientFeetY, 1.0E-4, "the client did not adopt the grounded position");
    }

    /**
     * The card's requested RED test, run against the released v0.7.3 gate: a column the session has
     * been sent must make {@code surfaceBelow(..., placement = true)} report a provable surface.
     * <p>
     * This is GREEN on v0.7.3 source, and round 5's probe agrees: after the column arrived, an RCON
     * teleport 0.25 blocks up in the same column immediately produced
     * "Grounding suspended converted player at (60000.5, 103.25, 0.5) to (60000.5, 101.0, 0.5)".
     * The gate is therefore not what kept the hold unresolved; see {@link #heldPlacementSendsNothingToTheBackend}.
     * The test stays as the contract for the gate: delivered means provable, not resident-by-accident.
     */
    @Test
    @DisplayName("a delivered column makes a pending placement's surface provable")
    void deliveredColumnMakesPlacementSurfaceProvable() {
        Harness harness = new Harness(geyserImpl, playerOffset);
        harness.publishBlock(5, -21, 5, 0);
        harness.sample = 0;

        PlacementGrounding.Surface surface = PlacementGrounding.surfaceBelow(harness.session,
            Vector3d.from(X, harness.clientFeetY, Z), true);

        assertTrue(surface.provable(), "a column the server has sent was not provable");
        assertNotNull(surface.grounded(), "no surface was found in a delivered column that has one");
        assertEquals(-20.0, surface.grounded().getY(), 1.0E-4);

        // The void-vs-unknown distinction the hold depends on: a column that was never sent is not
        // "empty", so the hold must keep waiting rather than read the empty default as a void.
        Harness unsent = new Harness(geyserImpl, playerOffset);
        unsent.sample = 0;
        PlacementGrounding.Surface unknown = PlacementGrounding.surfaceBelow(unsent.session,
            Vector3d.from(X, unsent.clientFeetY, Z), true);
        assertFalse(unknown.provable(), "an unsent column was reported as the world's own answer");
        assertNull(unknown.grounded(), "an unsent column produced a surface out of the empty default");
    }

    /**
     * The same placement with a terrain gap longer than Paper's own 81-tick (4.05 s) budget: the
     * released converter has already latched the floating check by then and is disconnected, even
     * though the terrain does turn up.
     */
    @Test
    @DisplayName("a terrain gap longer than the flying budget still ends on the surface")
    void terrainGapBeyondFlyingBudgetEndsOnTheSurface() {
        Harness harness = new Harness(geyserImpl, playerOffset);
        harness.publishBlock(5, -21, 5, 90);

        harness.run(SAMPLES_PER_SECOND * 8);

        assertNull(harness.backend.kickTick, "disconnected after " + harness.backend.kickTick
            + " ticks of a terrain gap that the converter could have bridged");
        assertEquals(0, harness.airborneSamplesSince(GRACE_SAMPLES),
            "airborne movement samples were still forwarded after the 0.5 s grace");
        assertNotNull(harness.groundingTeleportY, "the placement was never grounded once terrain was published");
        assertEquals(-20.0, harness.groundingTeleportY, 1.0E-4);
        assertEquals(-20.0, harness.clientFeetY, 1.0E-4);
    }

    /**
     * The card's regression. geyserlite runs in its own process, so its 20 Hz sample clock keeps
     * running while the backend is frozen; a hold measured in wall-clock samples therefore expires
     * inside a stall, and every sample it hands over after that is queued for a server that reads the
     * last one as flight the moment it resumes. The customer's Paper produced a 26375 ms tick with
     * TPS 0.1 and his placement-to-kick window was still only 6 s: the stall is longer than the whole
     * 5.5 s budget the released fix shipped with.
     * <p>
     * Here the backend stops ticking for 26 s ({@link #STALL_SAMPLES}) right after the placement,
     * streams nothing (no chunk, no packet), and publishes the column only after it resumes. The
     * converted player must survive and end on the surface.
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
        assertEquals(0, harness.airborneSamplesSince(GRACE_SAMPLES),
            "airborne movement samples were handed to a frozen backend and latched on resume");
        assertNotNull(harness.groundingTeleportY, "the placement was never grounded once the column arrived");
        assertEquals(-20.0, harness.groundingTeleportY, 1.0E-4);
        assertEquals(-20.0, harness.clientFeetY, 1.0E-4, "the client did not adopt the grounded position");
    }

    /** The same stall, but longer than the customer's and longer than any wall-clock budget: 39 s. */
    @Test
    @DisplayName("a 39 s backend stall is still not evidence of a void")
    void aLongerBackendStallIsStillBridged() {
        Harness harness = new Harness(geyserImpl, playerOffset);
        harness.publishBlock(5, -21, 5, 20 + 39 * SAMPLES_PER_SECOND + 20);
        harness.stall(20, 20 + 39 * SAMPLES_PER_SECOND);

        harness.run(SAMPLES_PER_SECOND * 60);

        assertNull(harness.backend.kickTick, "disconnected for flying (tick " + harness.backend.kickTick + ")");
        assertEquals(0, harness.airborneSamplesSince(GRACE_SAMPLES),
            "airborne movement samples were handed to a frozen backend");
        assertNotNull(harness.groundingTeleportY, "the placement was never grounded once the column arrived");
        assertEquals(-20.0, harness.clientFeetY, 1.0E-4);
    }

    /**
     * A column the world really has sent, with nothing collidable in it, is a real void: the backend
     * reads the same blocks, so its verdict is the honest one and must not be masked.
     */
    @Test
    @DisplayName("a loaded but empty column is a real void and goes back to the backend")
    void aProvablyEmptyColumnIsReleased() {
        Harness harness = new Harness(geyserImpl, playerOffset);
        harness.publishColumn(5, 5, 0);

        harness.run(SAMPLES_PER_SECOND * 12);

        assertNull(harness.groundingTeleportY, "grounded a placement with no surface to stand on");
        int releasedAt = harness.firstAirborneSampleSince(HOLD_START_SAMPLE);
        assertTrue(releasedAt >= HOLD_START_SAMPLE && releasedAt <= HOLD_START_SAMPLE + SAMPLES_PER_SECOND,
            "a provably empty column was masked until sample " + releasedAt
                + " instead of being handed back to the backend");
        assertNotNull(harness.backend.kickTick, "the released placement was still masked from the backend");
    }

    /**
     * Terrain that stays unknown while the backend keeps ticking and talking: the hold cannot last
     * forever, but its budget is backend-alive time, so it expires only after 60 s of it (>= 60 s is
     * the card's floor), roughly fifteen times Paper's own 81-tick budget.
     */
    @Test
    @DisplayName("terrain that stays unknown for 60 s of backend time is then released")
    void unknownTerrainIsReleasedAfterSixtySecondsOfBackendTime() {
        Harness harness = new Harness(geyserImpl, playerOffset);

        harness.run(SAMPLES_PER_SECOND * 75);

        int releasedAt = harness.firstAirborneSampleSince(HOLD_START_SAMPLE);
        assertTrue(releasedAt > 0, "never released the placement back to the backend");
        assertTrue(releasedAt >= HOLD_ALIVE_CAP_SAMPLES, "released at sample " + releasedAt
            + " of a backend that was alive the whole time, before the 60 s cap");
        assertTrue(releasedAt <= HOLD_ALIVE_CAP_SAMPLES + SAMPLES_PER_SECOND * 2,
            "held the placement to sample " + releasedAt + ", far beyond the 60 s cap");
        assertNotNull(harness.backend.kickTick, "the released placement was still masked from the backend");
    }

    /**
     * A client that starts moving is simulating physics of its own, so it is no longer holding the
     * frozen position a client with no terrain produces. That motion must not be hidden.
     */
    @Test
    @DisplayName("a suspended client that starts moving is handed back")
    void aMovingClientIsReleased() {
        Harness harness = new Harness(geyserImpl, playerOffset);
        harness.deltaX = 0.2F;

        harness.run(SAMPLES_PER_SECOND * 8);

        int releasedAt = harness.firstAirborneSampleSince(HOLD_START_SAMPLE);
        assertTrue(releasedAt == HOLD_START_SAMPLE,
            "a moving client stayed masked until sample " + releasedAt);
        assertTrue(harness.clientX > X + 1.0, "the client did not actually move: " + harness.clientX);
        assertNotNull(harness.backend.kickTick, "the moving client was still masked from the backend");
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
        assertNull(harness.backend.kickTick, "a descending placement was reported as flying");
        for (Sample sample : harness.samples) {
            assertFalse(sample.onGround(), "reported a descending placement as supported");
        }
    }

    /**
     * A placement that lands far below the feet, outside the 32-block window used mid-session, is
     * still the world's own surface: the placement search looks down the whole column.
     */
    @Test
    @DisplayName("a pending placement grounds onto a surface 42 blocks down")
    void placementGroundsOntoSurfaceBelowThePlainWindow() {
        Harness harness = new Harness(geyserImpl, playerOffset);
        harness.publishBlock(5, -60, 5, 0);

        harness.run(SAMPLES_PER_SECOND * 3);

        assertNotNull(harness.groundingTeleportY, "did not ground a pending placement onto the surface below it");
        assertEquals(-59.0, harness.groundingTeleportY, 1.0E-4);
        assertNull(harness.backend.kickTick, "the placement was kicked instead of grounded");
    }

    /**
     * A surface that is farther down than the mid-session search window is not a placement the
     * converter may silently relocate to while the player is already playing.
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
            for (Sample sample : harness.samples) {
                assertFalse(sample.onGround(), exemption + ": reported an exempt state as supported");
            }
        }
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
     * {@code latchesFloating} models what round 5 measured on Paper 26.3-205 rather than what the
     * vanilla source reads: the latch is <b>geometric</b>. In the lab a converter that reported
     * {@code isOnGround = true} every second at a placement with no support (9 x
     * {@code ServerboundMovePlayerPos} at {@code (60000.5, 103.0, 0.5)} with flag byte {@code 0x01},
     * tp+0.9 s .. tp+4.9 s) was still disconnected at tp+4.7 s with "kicked for floating too long",
     * i.e. the client's own on-ground flag does not clear {@code clientIsFloating}. What matters is
     * the backend's view of the position: airborne, not descending, and nothing within 0.55 below the
     * feet.
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
        final SessionPlayerEntity entity = mock(SessionPlayerEntity.class);
        final CollisionManager collisionManager = mock(CollisionManager.class);
        final InputCache inputCache = mock(InputCache.class);
        final WorldManager worldManager = mock(WorldManager.class);
        final ChunkCache chunkCache = mock(ChunkCache.class);
        final PaperFloatingCounter backend = new PaperFloatingCounter();

        final List<int[]> publishedBlocks = new ArrayList<>(); // x, y, z, firstVisibleSample
        final List<int[]> loadedColumns = new ArrayList<>(); // chunkX, chunkZ, firstLoadedSample
        final List<Sample> samples = new ArrayList<>();

        final AtomicInteger suspendedSamples = new AtomicInteger();
        final AtomicBoolean placementPending = new AtomicBoolean(true);
        final AtomicBoolean entityOnGround = new AtomicBoolean(false);
        final AtomicBoolean collidingVertically = new AtomicBoolean(false);
        final AtomicReference<TeleportCache> unconfirmedTeleport = new AtomicReference<>();
        final AtomicReference<GameMode> gameMode = new AtomicReference<>(GameMode.SURVIVAL);
        final AtomicBoolean flying = new AtomicBoolean(false);
        final AtomicBoolean hasVehicle = new AtomicBoolean(false);
        final AtomicBoolean touchingWater = new AtomicBoolean(false);
        final AtomicBoolean onClimbableBlock = new AtomicBoolean(false);
        final AtomicReference<Entity> vehicle = new AtomicReference<>();

        final AtomicInteger holdLiveSamples = new AtomicInteger();
        final AtomicLong holdPacketMark = new AtomicLong();
        final AtomicLong downstreamPackets = new AtomicLong();

        final float playerOffset;
        int sample;
        double clientFeetY = -18.0;
        double clientX = X;
        /** Where the converter last told the backend the player is (the real entity position). */
        double entityX = X;
        double entityFeetY = -18.0;
        float deltaY;
        float deltaX;
        /** Backend stall window in sample indices: the server neither ticks nor sends anything. */
        int stallFrom = Integer.MAX_VALUE;
        int stallUntil = -1;
        boolean stalled;
        Double groundingTeleportY;
        Double pendingTeleportFeetY;
        int groundedBySample = -1;
        // The position reminder is already due when the placement lands: the instrumented converter
        // forwarded the placement immediately after spawn (Paper latched from its first sample).
        private int ticksSinceMovePacket = 19;

        Harness(GeyserImpl geyser, float playerOffset) {
            this.playerOffset = playerOffset;
            when(geyser.getWorldManager()).thenReturn(worldManager);
            when(geyser.getLogger()).thenReturn(mock(GeyserLogger.class));

            when(session.getGeyser()).thenReturn(geyser);
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
            when(session.getBedrockDimension()).thenReturn(BedrockDimension.OVERWORLD);
            when(session.getCollisionManager()).thenReturn(collisionManager);
            when(session.getInputCache()).thenReturn(inputCache);
            // The converter can only distinguish "this column has no surface" from "this column was
            // never sent" through the world manager + chunk cache: a standalone session caches chunks.
            when(worldManager.hasOwnChunkCache()).thenReturn(false);
            when(session.getChunkCache()).thenReturn(chunkCache);
            // The overworld's cached height: section -4 (y=-64) upwards, 24 sections tall.
            when(chunkCache.getChunkMinY()).thenReturn(BedrockDimension.OVERWORLD.minY() >> 4);
            when(chunkCache.getChunkHeightY()).thenReturn(24);
            when(chunkCache.isChunkLoaded(anyInt(), anyInt()))
                .thenAnswer(invocation -> columnLoaded(invocation.getArgument(0), invocation.getArgument(1)));
            when(session.downstreamPacketCount()).thenAnswer(invocation -> downstreamPackets.get());
            doAnswer(invocation -> {
                downstreamPackets.incrementAndGet();
                return null;
            }).when(session).noteDownstreamPacket();
            when(session.getSuspendedHoldLiveSamples()).thenAnswer(invocation -> holdLiveSamples.get());
            doAnswer(invocation -> {
                holdLiveSamples.set(invocation.getArgument(0));
                return null;
            }).when(session).setSuspendedHoldLiveSamples(anyInt());
            when(session.getSuspendedHoldPacketMark()).thenAnswer(invocation -> holdPacketMark.get());
            doAnswer(invocation -> {
                holdPacketMark.set(invocation.getArgument(0));
                return null;
            }).when(session).setSuspendedHoldPacketMark(anyLong());
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

            // Geyser's own position reminder cadence: one sample per second while the converted
            // client does not move (InputCache.shouldSendPositionReminder()).
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
                Boolean onGround = onGroundOf(invocation.getArgument(0));
                if (onGround != null) {
                    Sample recorded = new Sample(sample, onGround, supportedByWorld(), deltaY < -0.03125F);
                    samples.add(recorded);
                    backend.movementSample(recorded);
                    // The converter adopts the position it just reported as the entity position.
                    entityX = clientX;
                    entityFeetY = clientFeetY;
                }
                return null;
            }).when(session).sendDownstreamGamePacket(any());

            when(worldManager.getBlockAt(any(), anyInt(), anyInt(), anyInt()))
                .thenAnswer(invocation -> visible(invocation.getArgument(1), invocation.getArgument(2),
                    invocation.getArgument(3)) ? STONE : Block.JAVA_AIR_ID);
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

        private boolean columnLoaded(int chunkX, int chunkZ) {
            for (int[] column : loadedColumns) {
                if (column[0] == chunkX && column[1] == chunkZ && sample >= column[2]) {
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
        private boolean supportedByWorld() {
            int minX = (int) Math.floor(clientX - 0.3);
            int maxX = (int) Math.floor(clientX + 0.3);
            int minZ = (int) Math.floor(Z - 0.3);
            int maxZ = (int) Math.floor(Z + 0.3);
            int minY = (int) Math.floor(clientFeetY - SUPPORT_DEPTH);
            int maxY = (int) Math.floor(clientFeetY);
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
                sample = i;
                BedrockMovePlayer.translate(session, packet());
                if (!stalled) {
                    // A ticking backend sends this session at least one packet per tick (time update,
                    // entity tracking, keepalive): that is the converter's only liveness evidence.
                    downstreamPackets.incrementAndGet();
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

        int firstAirborneSampleSince(int firstSample) {
            for (Sample sample : samples) {
                if (sample.index() >= firstSample && sample.latchesFloating()) {
                    return sample.index();
                }
            }
            return -1;
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
    }
}

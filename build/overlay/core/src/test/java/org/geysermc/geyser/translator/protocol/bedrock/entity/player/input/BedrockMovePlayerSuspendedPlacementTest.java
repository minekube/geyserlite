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
import org.geysermc.geyser.session.cache.InputCache;
import org.geysermc.geyser.session.cache.SkullCache;
import org.geysermc.geyser.session.cache.TeleportCache;
import org.geysermc.geyser.session.cache.WorldBorder;
import org.geysermc.geyser.translator.collision.BlockCollision;
import org.geysermc.geyser.util.BlockUtils;
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
     * Fail-safe for terrain that never becomes available: the converter bridges a bounded window and
     * then hands the placement back to the backend instead of pinning it forever.
     */
    @Test
    @DisplayName("terrain that never appears is released after a bounded grace")
    void unavailableTerrainIsReleasedAfterABoundedGrace() {
        Harness harness = new Harness(geyserImpl, playerOffset);

        harness.run(SAMPLES_PER_SECOND * 12);

        assertNull(harness.groundingTeleportY, "grounded a placement with no surface to stand on");
        int releasedAt = harness.firstAirborneSampleSince(GRACE_SAMPLES);
        assertTrue(releasedAt > 0, "never released the placement back to the backend");
        double releasedAfterSeconds = (releasedAt - GRACE_SAMPLES) / (double) SAMPLES_PER_SECOND;
        assertTrue(releasedAfterSeconds <= 6.0,
            "held the placement for " + releasedAfterSeconds + " s, beyond the bounded grace");
        // Once released, the backend's own verdict applies (vanilla behaviour for a converted client
        // that keeps claiming to hover with nothing under it).
        assertNotNull(harness.backend.kickTick, "the released placement was still masked from the backend");
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

    /** One movement sample as the backend saw it. */
    private record Sample(int index, boolean onGround, boolean supportedByWorld, boolean descending) {
        /** Paper's {@code clientIsFloating} latch: airborne, not descending, nothing under it. */
        boolean latchesFloating() {
            return !onGround && !supportedByWorld && !descending;
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

        void movementSample(Sample sample) {
            this.clientIsFloating = sample.latchesFloating();
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
        final PaperFloatingCounter backend = new PaperFloatingCounter();

        final List<int[]> publishedBlocks = new ArrayList<>(); // x, y, z, firstVisibleSample
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

        final float playerOffset;
        int sample;
        double clientFeetY = -18.0;
        float deltaY;
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

            when(entity.bedrockPosition()).thenAnswer(invocation -> Vector3f.from(X, (float) clientFeetY + playerOffset, Z));
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
            int minX = (int) Math.floor(X - 0.3);
            int maxX = (int) Math.floor(X + 0.3);
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
                sample = i;
                BedrockMovePlayer.translate(session, packet());
                backend.serverTick();
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
            packet.setPosition(Vector3f.from(X, (float) clientFeetY + playerOffset, Z));
            packet.setDelta(Vector3f.from(0.0F, deltaY, 0.0F));
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

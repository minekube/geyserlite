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

package org.geysermc.geyser.entity.type;

import org.cloudburstmc.protocol.bedrock.packet.MoveEntityDeltaPacket;
import org.geysermc.geyser.entity.VanillaEntities;
import org.geysermc.geyser.entity.spawn.EntitySpawnContext;
import org.junit.jupiter.api.Test;

import java.util.UUID;

import static org.geysermc.geyser.scoreboard.network.util.AssertUtils.assertNextPacketMatch;
import static org.geysermc.geyser.scoreboard.network.util.AssertUtils.assertNoNextPacket;
import static org.geysermc.geyser.scoreboard.network.util.GeyserMockContextScoreboard.mockContextScoreboard;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

class EntityMovementTest {
    @Test
    void suppressesNoOpMovementWithoutDroppingGroundTransitions() {
        mockContextScoreboard(context -> {
            EntitySpawnContext spawnContext = EntitySpawnContext.DUMMY_CONTEXT.apply(
                    context.session(), UUID.randomUUID(), VanillaEntities.ARMOR_STAND
            );
            spawnContext.geyserId(2L);
            Entity entity = new Entity(spawnContext);
            entity.setValid(true);

            entity.moveRelative(0, 0, 0, 0, 0, 0, false);
            assertNoNextPacket(context);

            entity.moveRelative(0, 0, 0, 0, 0, 0, true);
            assertNextPacketMatch(context, MoveEntityDeltaPacket.class,
                    packet -> assertTrue(packet.getFlags().contains(MoveEntityDeltaPacket.Flag.ON_GROUND)));

            entity.moveRelative(0, 0, 0, 0, 0, 0, true);
            assertNoNextPacket(context);

            entity.moveRelative(0, 0, 0, 0, 0, 0, false);
            assertNextPacketMatch(context, MoveEntityDeltaPacket.class,
                    packet -> assertFalse(packet.getFlags().contains(MoveEntityDeltaPacket.Flag.ON_GROUND)));
        });
    }
}

package org.geysermc.geyser.registry.populator.conversion;

import org.cloudburstmc.nbt.NbtMap;
import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.geysermc.geyser.scoreboard.network.util.GeyserMockContext.mockContext;

class LegacyBlockRemapperTest {
    @Test
    void oldPaletteUsesItsOwnRemapperForNewJavaBlocks() {
        NbtMap poplar = NbtMap.builder()
                .putString("name", "minecraft:poplar_planks")
                .putCompound("states", NbtMap.EMPTY)
                .build();
        mockContext(context -> assertEquals("minecraft:pale_oak_planks",
                ICanHasMoreStates.Old.INSTANCE.remapBlock(poplar).getString("name")));
    }
}

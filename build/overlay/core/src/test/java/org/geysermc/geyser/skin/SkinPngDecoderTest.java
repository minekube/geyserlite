/*
 * Copyright (c) 2026 Minekube. https://minekube.com
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

package org.geysermc.geyser.skin;

import org.junit.jupiter.api.Test;

import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.DataInputStream;
import java.io.DataOutputStream;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Arrays;
import java.util.Base64;
import java.util.concurrent.atomic.AtomicReference;
import java.util.zip.CRC32;

import static org.junit.jupiter.api.Assertions.assertArrayEquals;
import static org.junit.jupiter.api.Assertions.assertDoesNotThrow;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

final class SkinPngDecoderTest {
    private static final byte[] TWO_PIXEL_RGBA = Base64.getDecoder().decode(
            "iVBORw0KGgoAAAANSUhEUgAAAAIAAAABCAYAAAD0In+KAAAAD0lEQVR4nGP4z8DwHwgbABB5A359Y87XAAAAAElFTkSuQmCC");
    private static final byte[] TWO_PIXEL_PALETTE = Base64.getDecoder().decode(
            "iVBORw0KGgoAAAANSUhEUgAAAAIAAAABCAMAAADD/I+4AAAABlBMVEX/AAAA/wDSh+9xAAAAC0lEQVR4nGNgYAQAAAQAAr96P0oAAAAASUVORK5CYII=");
    private static final byte[] TWO_PIXEL_PALETTE_TRANSPARENCY = Base64.getDecoder().decode(
            "iVBORw0KGgoAAAANSUhEUgAAAAIAAAABCAMAAADD/I+4AAAABlBMVEX/AAAA/wDSh+9xAAAAAnRSTlMAgJsrThgAAAALSURBVHicY2BgBAAABAACv3o/SgAAAABJRU5ErkJggg==");
    private static final byte[] TWO_PIXEL_16_BIT_TRANSPARENCY = Base64.getDecoder().decode(
            "iVBORw0KGgoAAAANSUhEUgAAAAIAAAABEAIAAAAr0DSeAAAABnRSTlP//wAAAABABmvRAAAAD0lEQVR4nGP4/58BDEA0AB3vA/2Q9PvnAAAAAElFTkSuQmCC");
    private static final byte[] SKIN_64_X_32 = Base64.getDecoder().decode(
            "iVBORw0KGgoAAAANSUhEUgAAAEAAAAAgCAYAAACinX6EAAAAHklEQVR4nO3BAQ0AAADCoPdPbQ8HFAAAAAAAAADwbiAgAAFXlYP5AAAAAElFTkSuQmCC");
    private static final byte[] SKIN_64_X_64 = Base64.getDecoder().decode(
            "iVBORw0KGgoAAAANSUhEUgAAAEAAAABACAYAAACqaXHeAAAAJ0lEQVR4nO3BAQ0AAADCoPdPbQ43oAAAAAAAAAAAAAAAAAAAAIB3A0BAAAGP8slRAAAAAElFTkSuQmCC");
    private static final byte[] TOO_WIDE = Base64.getDecoder().decode(
            "iVBORw0KGgoAAAANSUhEUgAAAEEAAAABCAYAAACBr8MpAAAADElEQVR4nGNgGAUMAAEFAAGekBwbAAAAAElFTkSuQmCC");
    private static final byte[] TOO_TALL = Base64.getDecoder().decode(
            "iVBORw0KGgoAAAANSUhEUgAAAAEAAABBCAYAAAAQTc7lAAAADUlEQVR4nGNgGAUUAwABRQABIBPmYQAAAABJRU5ErkJggg==");
    private static final byte[] TOO_WIDE_INTERLACED = Base64.getDecoder().decode(
            "iVBORw0KGgoAAAANSUhEUgAAAEEAAAABCAYAAAH2qPO/AAAACElEQVR4nAMAAAAAAUgGidIAAAAASUVORK5CYII=");

    @Test
    void decodesRgbaWithoutAwt() throws Exception {
        SkinPngDecoder.Image image = SkinPngDecoder.decode(new ByteArrayInputStream(TWO_PIXEL_RGBA));

        assertEquals(2, image.width());
        assertEquals(1, image.height());
        assertArrayEquals(new byte[] {
                (byte) 255, 0, 0, (byte) 255,
                0, (byte) 255, 0, (byte) 128
        }, image.rgba());
    }

    @Test
    void decodesOpaquePalettePngAsRgba() throws Exception {
        SkinPngDecoder.Image image = SkinPngDecoder.decode(new ByteArrayInputStream(TWO_PIXEL_PALETTE));

        assertArrayEquals(new byte[] {
                (byte) 255, 0, 0, (byte) 255,
                0, (byte) 255, 0, (byte) 255
        }, image.rgba());
    }

    @Test
    void decodesPaletteTransparency() throws Exception {
        assertArrayEquals(new byte[] {
                (byte) 255, 0, 0, 0,
                0, (byte) 255, 0, (byte) 128
        }, decode(TWO_PIXEL_PALETTE_TRANSPARENCY).rgba());
    }

    @Test
    void preserves16BitTransparency() throws Exception {
        assertArrayEquals(new byte[] {
                (byte) 255, 0, 0, 0,
                0, (byte) 255, 0, (byte) 255
        }, decode(TWO_PIXEL_16_BIT_TRANSPARENCY).rgba());
    }

    @Test
    void rejectsMalformedPng() {
        assertThrows(IOException.class, () -> decode(new byte[] {1, 2, 3, 4}));
    }

    @Test
    void rejectsTruncatedPng() {
        assertThrows(IOException.class, () -> decode(Arrays.copyOf(TWO_PIXEL_RGBA, TWO_PIXEL_RGBA.length - 10)));
    }

    @Test
    void rejectsEncodedDataAboveSafetyLimit() throws Exception {
        assertThrows(IOException.class, () -> decode(withOversizedTextChunk(SKIN_64_X_32)));
    }

    @Test
    void rejectsCriticalIdatAboveSafetyLimit() throws Exception {
        assertThrows(IOException.class, () -> decode(withOversizedIdatChunk(SKIN_64_X_32)));
    }

    @Test
    void rejectsOversizedDeclaredCriticalChunksBeforePngjAllocation() {
        for (String type : new String[] {"IHDR", "PLTE", "IEND"}) {
            IOException exception = assertThrows(IOException.class,
                    () -> decode(withDeclaredChunkLength(TWO_PIXEL_PALETTE, type,
                            SkinPngDecoder.MAX_ENCODED_BYTES + 1)));
            assertTrue(exception.getMessage().contains("chunk length exceeds safety limit"), type);
        }
    }

    @Test
    void appliesPlayerSkinAlphaMask() {
        byte[] rgba = new byte[64 * 32 * 4];
        byte[] mask = new byte[64 * 32 / 8];
        mask[1] = 1;

        byte[] prepared = SkinPngDecoder.applyAlphaMask(new SkinPngDecoder.Image(64, 32, rgba), mask);

        assertEquals(0, prepared[3]);
        assertEquals((byte) 0xFF, prepared[8 * 4 + 3]);
    }

    @Test
    void cacheWriteFailureIsReportedWithoutEscaping() throws Exception {
        Path unwritableTarget = Files.createTempDirectory("skin-cache-target");
        AtomicReference<IOException> failure = new AtomicReference<>();

        assertDoesNotThrow(() -> SkinPngDecoder.writeCache(unwritableTarget, new byte[] {1}, failure::set));
        assertNotNull(failure.get());
    }

    @Test
    void acceptsPlayerSkinDimensionLimits() throws Exception {
        assertEquals(32, decode(SKIN_64_X_32).height());
        assertEquals(64, decode(SKIN_64_X_64).height());
    }

    @Test
    void rejectsWidthAbovePlayerSkinLimit() {
        assertThrows(IOException.class, () -> decode(TOO_WIDE));
    }

    @Test
    void rejectsHeightAbovePlayerSkinLimit() {
        assertThrows(IOException.class, () -> decode(TOO_TALL));
    }

    @Test
    void rejectsInterlacedWidthAboveLimitBeforeRowsAreDecoded() {
        assertThrows(IOException.class, () -> decode(TOO_WIDE_INTERLACED));
    }

    private static byte[] withOversizedTextChunk(byte[] png) throws IOException {
        byte[] type = {'t', 'E', 'X', 't'};
        byte[] payload = new byte[SkinPngDecoder.MAX_ENCODED_BYTES];
        CRC32 crc = new CRC32();
        crc.update(type);
        crc.update(payload);

        ByteArrayOutputStream bytes = new ByteArrayOutputStream(png.length + payload.length + 12);
        try (DataOutputStream output = new DataOutputStream(bytes)) {
            output.write(png, 0, 33); // PNG signature and IHDR chunk
            output.writeInt(payload.length);
            output.write(type);
            output.write(payload);
            output.writeInt((int) crc.getValue());
            output.write(png, 33, png.length - 33);
        }
        return bytes.toByteArray();
    }

    private static byte[] withOversizedIdatChunk(byte[] png) throws IOException {
        ByteArrayOutputStream bytes = new ByteArrayOutputStream(png.length + SkinPngDecoder.MAX_ENCODED_BYTES);
        bytes.write(png, 0, 8);
        try (DataInputStream input = new DataInputStream(new ByteArrayInputStream(png, 8, png.length - 8));
             DataOutputStream output = new DataOutputStream(bytes)) {
            while (input.available() > 0) {
                int length = input.readInt();
                byte[] type = input.readNBytes(4);
                byte[] payload = input.readNBytes(length);
                input.readInt();
                if (Arrays.equals(type, new byte[] {'I', 'D', 'A', 'T'})) {
                    payload = Arrays.copyOf(payload, payload.length + SkinPngDecoder.MAX_ENCODED_BYTES);
                }
                CRC32 crc = new CRC32();
                crc.update(type);
                crc.update(payload);
                output.writeInt(payload.length);
                output.write(type);
                output.write(payload);
                output.writeInt((int) crc.getValue());
            }
        }
        return bytes.toByteArray();
    }

    private static byte[] withDeclaredChunkLength(byte[] png, String targetType, int declaredLength) {
        byte[] modified = png.clone();
        int offset = 8;
        while (offset + 12 <= modified.length) {
            int length = ((modified[offset] & 0xFF) << 24)
                    | ((modified[offset + 1] & 0xFF) << 16)
                    | ((modified[offset + 2] & 0xFF) << 8)
                    | (modified[offset + 3] & 0xFF);
            String type = new String(modified, offset + 4, 4, java.nio.charset.StandardCharsets.US_ASCII);
            if (targetType.equals(type)) {
                modified[offset] = (byte) (declaredLength >>> 24);
                modified[offset + 1] = (byte) (declaredLength >>> 16);
                modified[offset + 2] = (byte) (declaredLength >>> 8);
                modified[offset + 3] = (byte) declaredLength;
                return modified;
            }
            offset += 12 + length;
        }
        throw new IllegalArgumentException("Missing chunk " + targetType);
    }

    private static SkinPngDecoder.Image decode(byte[] png) throws IOException {
        return SkinPngDecoder.decode(new ByteArrayInputStream(png));
    }
}

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

import ar.com.hjg.pngj.ImageInfo;
import ar.com.hjg.pngj.ImageLineHelper;
import ar.com.hjg.pngj.ImageLineInt;
import ar.com.hjg.pngj.PngReader;
import ar.com.hjg.pngj.chunks.PngChunkPLTE;
import ar.com.hjg.pngj.chunks.PngChunkTRNS;

import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.function.Consumer;

/**
 * Decodes PNGs without AWT so player skins work in GeyserLite native images.
 */
final class SkinPngDecoder {
    static final int MAX_ENCODED_BYTES = 4 << 20;
    private static final int MAX_WIDTH = 64;
    private static final int MAX_HEIGHT = 64;
    private static final byte[] PNG_SIGNATURE = {(byte) 0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'};
    private static final int CHUNK_IHDR = 0x49484452;
    private static final int CHUNK_IEND = 0x49454E44;

    private SkinPngDecoder() {
    }

    static Image decode(InputStream stream) throws IOException {
        byte[] png = stream.readNBytes(MAX_ENCODED_BYTES + 1);
        if (png.length > MAX_ENCODED_BYTES) {
            throw new IOException("PNG exceeds " + MAX_ENCODED_BYTES + " bytes");
        }
        validateChunkTable(png);

        PngReader reader;
        try {
            reader = new PngReader(new ByteArrayInputStream(png), false);
        } catch (RuntimeException e) {
            throw new IOException("Invalid PNG", e);
        }

        try {
            ImageInfo info = reader.imgInfo;
            reader.setMaxTotalBytesRead(MAX_ENCODED_BYTES);
            if (info.cols > MAX_WIDTH || info.rows > MAX_HEIGHT) {
                throw new IOException("PNG dimensions exceed safety limit: " + info.cols + "x" + info.rows);
            }

            PngChunkPLTE palette = reader.getMetadata().getPLTE();
            PngChunkTRNS transparency = reader.getMetadata().getTRNS();
            byte[] rgba = new byte[Math.multiplyExact(Math.multiplyExact(info.cols, info.rows), 4)];

            for (int y = 0; y < info.rows; y++) {
                ImageLineInt line = (ImageLineInt) reader.readRow(y);
                decodeRow(info, line, palette, transparency, rgba, y * info.cols * 4);
            }
            reader.end();
            return new Image(info.cols, info.rows, rgba);
        } catch (RuntimeException e) {
            throw new IOException("Failed to decode PNG", e);
        } finally {
            reader.close();
        }
    }

    private static void decodeRow(ImageInfo info, ImageLineInt line, PngChunkPLTE palette,
                                  PngChunkTRNS transparency, byte[] rgba, int outputOffset) throws IOException {
        if (info.indexed) {
            if (palette == null) {
                throw new IOException("Indexed PNG is missing a palette");
            }
            int[] samples = ImageLineHelper.palette2rgb(line, palette, transparency, null);
            int channels = transparency == null ? 3 : 4;
            for (int x = 0; x < info.cols; x++) {
                int sampleOffset = x * channels;
                rgba[outputOffset++] = (byte) samples[sampleOffset];
                rgba[outputOffset++] = (byte) samples[sampleOffset + 1];
                rgba[outputOffset++] = (byte) samples[sampleOffset + 2];
                rgba[outputOffset++] = (byte) (channels == 4 ? samples[sampleOffset + 3] : 255);
            }
            return;
        }

        int[] samples = line.getScanline();
        int sampleOffset = 0;
        int transparentGray = transparency != null && info.greyscale ? transparency.getGray() : -1;
        int[] transparentRgb = transparency != null && !info.greyscale ? transparency.getRGB() : null;

        for (int x = 0; x < info.cols; x++) {
            int red;
            int green;
            int blue;
            int alpha;
            if (info.greyscale) {
                int graySample = samples[sampleOffset++];
                int gray = scaleToByte(graySample, info.bitDepth);
                red = green = blue = gray;
                alpha = info.alpha ? scaleToByte(samples[sampleOffset++], info.bitDepth)
                        : graySample == transparentGray ? 0 : 255;
            } else {
                int redSample = samples[sampleOffset++];
                int greenSample = samples[sampleOffset++];
                int blueSample = samples[sampleOffset++];
                red = scaleToByte(redSample, info.bitDepth);
                green = scaleToByte(greenSample, info.bitDepth);
                blue = scaleToByte(blueSample, info.bitDepth);
                alpha = info.alpha ? scaleToByte(samples[sampleOffset++], info.bitDepth)
                        : transparentRgb != null
                        && redSample == transparentRgb[0]
                        && greenSample == transparentRgb[1]
                        && blueSample == transparentRgb[2] ? 0 : 255;
            }

            rgba[outputOffset++] = (byte) red;
            rgba[outputOffset++] = (byte) green;
            rgba[outputOffset++] = (byte) blue;
            rgba[outputOffset++] = (byte) alpha;
        }
    }

    private static int scaleToByte(int sample, int bitDepth) {
        if (bitDepth == 8) {
            return sample;
        }
        int maximum = (1 << bitDepth) - 1;
        return (sample * 255 + maximum / 2) / maximum;
    }

    static byte[] applyAlphaMask(Image image, byte[] mask) {
        byte[] data = image.rgba();
        for (int pixel = 0; pixel < image.width() * image.height(); pixel++) {
            if ((((mask[pixel >> 3] & 0xFF) >> (pixel & 0x7)) & 1) != 0) {
                data[pixel * 4 + 3] = (byte) 0xFF;
            }
        }
        return data;
    }

    static boolean writeCache(Path path, byte[] png, Consumer<IOException> onFailure) {
        try {
            Files.write(path, png);
            return true;
        } catch (IOException exception) {
            onFailure.accept(exception);
            return false;
        }
    }

    private static void validateChunkTable(byte[] png) throws IOException {
        if (png.length < PNG_SIGNATURE.length) {
            throw new IOException("Invalid PNG signature");
        }
        for (int index = 0; index < PNG_SIGNATURE.length; index++) {
            if (png[index] != PNG_SIGNATURE[index]) {
                throw new IOException("Invalid PNG signature");
            }
        }

        int offset = PNG_SIGNATURE.length;
        boolean firstChunk = true;
        boolean foundEnd = false;
        while (offset < png.length) {
            if (png.length - offset < 12) {
                throw new IOException("Truncated PNG chunk");
            }
            long length = Integer.toUnsignedLong(readInt(png, offset));
            if (length > MAX_ENCODED_BYTES) {
                throw new IOException("PNG chunk length exceeds safety limit");
            }
            long chunkEnd = (long) offset + 12 + length;
            if (chunkEnd > png.length) {
                throw new IOException("Truncated PNG chunk");
            }

            int type = readInt(png, offset + 4);
            if (firstChunk) {
                if (type != CHUNK_IHDR || length != 13) {
                    throw new IOException("PNG must start with a 13-byte IHDR chunk");
                }
                int width = readInt(png, offset + 8);
                int height = readInt(png, offset + 12);
                if (width <= 0 || height <= 0 || width > MAX_WIDTH || height > MAX_HEIGHT) {
                    throw new IOException("PNG dimensions exceed safety limit: " + width + "x" + height);
                }
                firstChunk = false;
            }

            offset = (int) chunkEnd;
            if (type == CHUNK_IEND) {
                if (length != 0 || offset != png.length) {
                    throw new IOException("Invalid PNG end chunk");
                }
                foundEnd = true;
            }
        }
        if (!foundEnd) {
            throw new IOException("PNG is missing an end chunk");
        }
    }

    private static int readInt(byte[] bytes, int offset) {
        return (bytes[offset] & 0xFF) << 24
                | (bytes[offset + 1] & 0xFF) << 16
                | (bytes[offset + 2] & 0xFF) << 8
                | bytes[offset + 3] & 0xFF;
    }

    record Image(int width, int height, byte[] rgba) {
    }
}

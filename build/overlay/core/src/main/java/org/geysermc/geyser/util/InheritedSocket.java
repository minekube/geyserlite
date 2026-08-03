/*
 * geyserlite — inherited local IPC descriptor adoption.
 *
 * SPDX-License-Identifier: MIT
 */
package org.geysermc.geyser.util;

import java.io.FileDescriptor;
import java.io.FileInputStream;
import java.io.IOException;
import java.lang.reflect.Field;
import sun.misc.Unsafe;

final class InheritedSocket {
    private static final Unsafe UNSAFE = unsafe();
    private static final long FD_OFFSET = fileDescriptorOffset();

    private InheritedSocket() {}

    static FileInputStream openInput(int fd) throws IOException {
        if (fd < 0) {
            throw new IOException("invalid inherited descriptor");
        }
        FileDescriptor descriptor = new FileDescriptor();
        UNSAFE.putInt(descriptor, FD_OFFSET, fd);
        return new FileInputStream(descriptor);
    }

    private static Unsafe unsafe() {
        try {
            Field field = Unsafe.class.getDeclaredField("theUnsafe");
            field.setAccessible(true);
            return (Unsafe) field.get(null);
        } catch (ReflectiveOperationException error) {
            throw new ExceptionInInitializerError(error);
        }
    }

    private static long fileDescriptorOffset() {
        try {
            return UNSAFE.objectFieldOffset(FileDescriptor.class.getDeclaredField("fd"));
        } catch (ReflectiveOperationException error) {
            throw new ExceptionInInitializerError(error);
        }
    }
}

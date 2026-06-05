// SPDX-License-Identifier: MIT
package geyserlite

import (
	"strconv"
	"strings"
	"testing"
)

func TestDefaultJVMArgsDirectMemoryHasProductionSafeFloor(t *testing.T) {
	t.Parallel()

	const minDirectMemoryBytes = 64 * 1024 * 1024

	args := DefaultJVMArgs()
	nettyMaxDirectMemory := jvmSystemProperty(args, "-Dio.netty.maxDirectMemory=")
	if nettyMaxDirectMemory == "" {
		t.Fatal("DefaultJVMArgs missing -Dio.netty.maxDirectMemory")
	}
	gotNettyBytes, err := strconv.Atoi(nettyMaxDirectMemory)
	if err != nil {
		t.Fatalf("-Dio.netty.maxDirectMemory is not bytes: %q", nettyMaxDirectMemory)
	}
	if gotNettyBytes < minDirectMemoryBytes {
		t.Fatalf("-Dio.netty.maxDirectMemory = %d, want at least %d", gotNettyBytes, minDirectMemoryBytes)
	}

	maxDirectMemorySize := jvmSystemProperty(args, "-XX:MaxDirectMemorySize=")
	if maxDirectMemorySize == "" {
		t.Fatal("DefaultJVMArgs missing -XX:MaxDirectMemorySize")
	}
	gotMaxBytes, err := parseMemorySize(maxDirectMemorySize)
	if err != nil {
		t.Fatalf("-XX:MaxDirectMemorySize is invalid: %v", err)
	}
	if gotMaxBytes < minDirectMemoryBytes {
		t.Fatalf("-XX:MaxDirectMemorySize = %d, want at least %d", gotMaxBytes, minDirectMemoryBytes)
	}
}

func jvmSystemProperty(args []string, prefix string) string {
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix)
		}
	}
	return ""
}

func parseMemorySize(s string) (int, error) {
	multiplier := 1
	switch {
	case strings.HasSuffix(s, "m"), strings.HasSuffix(s, "M"):
		multiplier = 1024 * 1024
		s = s[:len(s)-1]
	case strings.HasSuffix(s, "g"), strings.HasSuffix(s, "G"):
		multiplier = 1024 * 1024 * 1024
		s = s[:len(s)-1]
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	return n * multiplier, nil
}

// SPDX-License-Identifier: MIT
//go:build geyserlite_embed && linux && arm64

package geyserlite

import _ "embed"

//go:embed assets/geyserlite-linux-arm64
var embeddedBinaryLinuxARM64 []byte

//go:embed assets/libgeyserlite-linux-arm64.so
var embeddedLibraryLinuxARM64 []byte

func extractEmbeddedBinary() (string, bool, error) {
	return extractEmbeddedAsset(embeddedBinaryLinuxARM64, "geyserlite", true)
}

func extractEmbeddedLibrary() (string, bool, error) {
	return extractEmbeddedAsset(embeddedLibraryLinuxARM64, "libgeyserlite.so", false)
}

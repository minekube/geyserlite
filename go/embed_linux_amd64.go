// SPDX-License-Identifier: MIT
//go:build geyserlite_embed && linux && amd64

package geyserlite

import _ "embed"

//go:embed assets/geyserlite-linux-amd64
var embeddedBinaryLinuxAMD64 []byte

//go:embed assets/libgeyserlite-linux-amd64.so
var embeddedLibraryLinuxAMD64 []byte

func extractEmbeddedBinary() (string, bool, error) {
	return extractEmbeddedAsset(embeddedBinaryLinuxAMD64, "geyserlite", true)
}

func extractEmbeddedLibrary() (string, bool, error) {
	return extractEmbeddedAsset(embeddedLibraryLinuxAMD64, "libgeyserlite.so", false)
}

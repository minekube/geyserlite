// SPDX-License-Identifier: MIT
//go:build !geyserlite_embed

package geyserlite

// extractEmbeddedBinary and extractEmbeddedLibrary are stubs when the embed
// build tag is not set. The real per-arch implementations live in
// embed_<os>_<arch>.go behind //go:build geyserlite_embed.

func extractEmbeddedBinary() (string, bool, error)  { return "", false, nil }
func extractEmbeddedLibrary() (string, bool, error) { return "", false, nil }

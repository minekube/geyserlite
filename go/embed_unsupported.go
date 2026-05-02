// SPDX-License-Identifier: MIT
//go:build geyserlite_embed && !(linux && (amd64 || arm64))

// When -tags geyserlite_embed is set on a target we don't ship an asset for,
// fall through to the on-disk lookup with an explanatory error message.

package geyserlite

func extractEmbeddedBinary() (string, bool, error)  { return "", false, nil }
func extractEmbeddedLibrary() (string, bool, error) { return "", false, nil }

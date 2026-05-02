// SPDX-License-Identifier: MIT
package geyserlite

import (
	"crypto/rand"
	"fmt"
)

// GenerateFloodgateKey returns 16 random bytes suitable as a Floodgate
// AES-128 key.
//
// The upstream Geyser README's openssl example using
//
//	openssl genpkey -algorithm RSA
//
// is wrong — that produces an RSA private key (~1700 bytes). Floodgate uses
// AES-128, which is exactly 16 random bytes. Cf. Gate's
// pkg/edition/bedrock/geyser/floodgate/cipher.go:38 which checks
// len(key) ∈ {16, 24, 32}.
func GenerateFloodgateKey() ([]byte, error) {
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("geyserlite: generate floodgate key: %w", err)
	}
	return key, nil
}

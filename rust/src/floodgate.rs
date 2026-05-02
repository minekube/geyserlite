// SPDX-License-Identifier: MIT

/// Generate a Floodgate AES-128 key (16 random bytes).
///
/// The upstream Geyser README's openssl example using
/// `openssl genpkey -algorithm RSA` is wrong — that's an RSA private key.
/// Floodgate uses AES-128, which is exactly 16 random bytes. Cf. Gate's
/// `pkg/edition/bedrock/geyser/floodgate/cipher.go:38` which checks
/// `len(key) ∈ {16, 24, 32}`.
pub fn generate_floodgate_key() -> Vec<u8> {
    use rand::TryRngCore;
    let mut buf = vec![0u8; 16];
    rand::rngs::OsRng
        .try_fill_bytes(&mut buf)
        .expect("OS RNG should not fail");
    buf
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn key_is_16_bytes() {
        assert_eq!(generate_floodgate_key().len(), 16);
    }

    #[test]
    fn keys_differ() {
        let a = generate_floodgate_key();
        let b = generate_floodgate_key();
        assert_ne!(a, b);
    }
}

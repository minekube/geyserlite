// SPDX-License-Identifier: MIT

pub(crate) fn hex_lower(bytes: &[u8]) -> String {
    const HEX: &[u8; 16] = b"0123456789abcdef";

    let mut out = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        out.push(HEX[(byte >> 4) as usize] as char);
        out.push(HEX[(byte & 0x0f) as usize] as char);
    }
    out
}

#[cfg(test)]
mod tests {
    use super::hex_lower;

    #[test]
    fn encodes_lowercase_hex() {
        assert_eq!(hex_lower(&[0x00, 0x0f, 0x10, 0xab, 0xff]), "000f10abff");
    }
}

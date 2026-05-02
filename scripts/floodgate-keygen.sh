#!/usr/bin/env bash
# Generate a Floodgate AES-128 key (16 raw bytes).
#
# The upstream Geyser README's openssl example
#   openssl genpkey -algorithm RSA -out key.pem
# is WRONG. That generates an RSA private key (~1700 bytes). Floodgate uses
# AES-128, which is exactly 16 random bytes.
#
# Usage:
#   ./floodgate-keygen.sh > key.bin
#   ./floodgate-keygen.sh /path/to/key.bin
set -euo pipefail
out="${1:-/dev/stdout}"

if [[ "$out" != "/dev/stdout" ]]; then
    head -c 16 /dev/urandom > "$out"
    chmod 600 "$out"
    echo "wrote 16-byte AES-128 Floodgate key to $out" >&2
else
    head -c 16 /dev/urandom
fi

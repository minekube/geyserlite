# Floodgate keys

> **TL;DR**: Floodgate uses **AES-128** (16 raw bytes). The upstream Geyser
> README's `openssl genpkey -algorithm RSA` example is **wrong**.

## Generating a key

```sh
head -c 16 /dev/urandom > key.bin
chmod 600 key.bin
```

Or use the helper script in this repo:

```sh
./scripts/floodgate-keygen.sh /path/to/key.bin
```

In code:

```go
key, _ := geyserlite.GenerateFloodgateKey()  // Go
```

```rust
let key = geyserlite::generate_floodgate_key();  // Rust
```

All produce the same thing: 16 cryptographically-random bytes.

## Why the README's openssl example is wrong

The upstream Geyser bedrock example
([README.md](https://github.com/GeyserMC/Geyser/blob/master/.examples/bedrock/README.md))
shows:

```sh
openssl genpkey -algorithm RSA -out /data/key.pem -pkcs8
```

That generates an **RSA private key** — about 1700 PEM-encoded bytes,
intended for the legacy Floodgate plugin's RSA-based protocol.

Modern Floodgate (and Gate's `bedrock.enabled` integration) use **AES-128**.
The plugin code at
[`pkg/edition/bedrock/geyser/floodgate/cipher.go:38`](https://github.com/minekube/gate/blob/master/pkg/edition/bedrock/geyser/floodgate/cipher.go)
in Gate explicitly checks:

```go
if len(key) != 16 && len(key) != 24 && len(key) != 32 {
    return nil, errors.New("invalid key length for AES: must be 16, 24, or 32 bytes")
}
```

So feeding it an RSA PEM file fails immediately:

```
failed to initialize floodgate: failed to create cipher: invalid key length for AES: must be 16, 24, or 32 bytes
```

This bit us during initial development. Use 16 raw bytes.

## Sharing the key

Both ends of the Floodgate handshake — Geyser and the Java upstream — need
the same key bytes. Common patterns:

- **Loopback** (Geyser and Gate on the same machine): one file mounted into
  both processes / containers.
- **Different machines**: distribute via your secrets store. The key is
  symmetric, so wherever you put it must be guarded as carefully as the
  upstream's Java auth secrets.

In Fly.io: store as base64 in `fly secrets set FLOODGATE_KEY_BASE64=$(base64 < key.bin)`,
and have the container's entrypoint decode it to a tmpfs file before launching
Geyser.

## Rotation

Currently: kill-and-restart everything that uses the key. Hot reload is
deferred (see ROADMAP open questions).

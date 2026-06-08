# `rust/assets/`

Per-target native artifacts used by the Rust crate. Linux artifacts can be
`include_bytes!`'d into the crate when built with the `embed` Cargo feature.
Windows currently uses the subprocess executable as a release download or
explicit `Options::binary_path`.

These files are **never committed**. Run `task embed:fetch` (or
`scripts/fetch-embed-assets.sh`) before `cargo build --features embed`.

## Fetched release layout

```text
rust/assets/
├── geyserlite-linux-amd64           native binary (subprocess mode)
├── geyserlite-linux-arm64
├── geyserlite-windows-amd64.exe     native binary (subprocess mode)
├── libgeyserlite-linux-amd64.so     shared library (embedded mode)
└── libgeyserlite-linux-arm64.so
```

Windows embedded DLL assets are not part of the expected layout yet.
Windows consumers should use subprocess mode with `geyserlite-windows-amd64.exe`.

## Why not commit them

Each blob is ~107 MB. crates.io enforces a 10 MB package size limit;
embedding the blobs directly would make the crate impossible to publish
even once. Instead, `embed` is a build-time feature that resolves to
locally-fetched binaries.

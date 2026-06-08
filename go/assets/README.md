# `go/assets/`

Per-arch native artifacts that get `//go:embed`'d into the Go binary when
built with the `geyserlite_embed` build tag.

These files are **never committed** — they're downloaded by
`task embed:fetch` (or your own equivalent) from a published GitHub
Release before building.

## Expected layout

```
go/assets/
├── geyserlite-linux-amd64           native binary (subprocess mode)
├── geyserlite-linux-arm64
├── geyserlite-windows-amd64.exe     native binary (subprocess mode)
├── libgeyserlite-linux-amd64.so     shared library (embedded mode)
├── libgeyserlite-linux-arm64.so
└── checksums.txt                    sha256 manifest from the release
```

Windows embedded DLL assets are not part of the expected layout yet.
Windows consumers should use subprocess mode with `geyserlite-windows-amd64.exe`.

## Why not commit them

Each blob is ~107 MB; 4 archives × 107 MB = ~430 MB committed to the Go
module. Module proxy caches and `go get` would slow significantly for
all consumers, even those who don't use embed mode.

Instead: the build-tag-gated `//go:embed` directive resolves to these
files **only** when both the tag is set *and* the file exists. Without the
tag, the files don't matter; with the tag but missing files, the
compiler emits a clear "no matching files" error pointing here.

## Fetching

```sh
task embed:fetch          # downloads the latest release into go/assets/ + rust/assets/
task embed:fetch -- v0.5  # specific tag
```

The `task` target is a thin wrapper around `scripts/fetch-embed-assets.sh`.

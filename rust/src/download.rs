// SPDX-License-Identifier: MIT
//! Auto-download path — compiled only with `--features download`.
//!
//! Mirrors `go/download.go`: fetch the release asset matching the runtime
//! arch into the user cache dir, verifying its sha256 against the
//! release's `checksums.txt` manifest.

use std::fs;
use std::io::{Read, Write};
use std::path::PathBuf;
use std::time::Duration;

use sha2::{Digest, Sha256};

use crate::error::{Error, Result};
use crate::hex::hex_lower;
use crate::options::Options;
use crate::paths::cache_root;
use crate::version::{DEFAULT_DOWNLOAD_BASE, DEFAULT_VERSION};

#[derive(Clone, Copy)]
pub(crate) enum AssetKind {
    Binary,
    Library,
}

pub(crate) async fn download_asset(opts: &Options, kind: AssetKind) -> Result<PathBuf> {
    let version = opts.version.as_deref().unwrap_or(DEFAULT_VERSION);
    let base = opts
        .mirror
        .as_deref()
        .unwrap_or(DEFAULT_DOWNLOAD_BASE)
        .trim_end_matches('/');
    let asset_name = asset_for_target(kind)?;

    let cache_dir = cache_root()?.join("geyserlite").join(version);
    let legacy_cached_path = cache_dir.join(asset_name);

    let expected = fetch_expected_sha(base, version, asset_name).await?;
    let cached_path = verified_download_path(&cache_dir, asset_name, &expected);

    if matching_sha(&cached_path, &expected) {
        return Ok(cached_path);
    }

    // Reuse the pre-content-addressed cache layout when it already contains
    // the expected asset. New downloads use cached_path, which avoids replacing
    // a version-stable executable that may still be running on Windows.
    if matching_sha(&legacy_cached_path, &expected) {
        return Ok(legacy_cached_path);
    }

    fs::create_dir_all(cached_path.parent().unwrap_or(&cache_dir))?;
    let url = format!("{base}/{version}/{asset_name}");
    let tmp = tempfile::Builder::new()
        .prefix(asset_name)
        .suffix(".tmp")
        .tempfile_in(cached_path.parent().unwrap_or(&cache_dir))?;
    download_to(&url, tmp.path()).await?;

    let got = stream_sha(tmp.path())?;
    if got != expected {
        return Err(Error::ChecksumMismatch {
            asset: asset_name.into(),
            got,
            want: expected,
        });
    }
    if let AssetKind::Binary = kind {
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let mut perm = fs::metadata(tmp.path())?.permissions();
            perm.set_mode(0o755);
            fs::set_permissions(tmp.path(), perm)?;
        }
    }
    if matching_sha(&cached_path, &expected) {
        return Ok(cached_path);
    }
    if let Err(err) = tmp.persist_noclobber(&cached_path) {
        if matching_sha(&cached_path, &expected) {
            return Ok(cached_path);
        }
        return Err(Error::Io(err.error));
    }
    Ok(cached_path)
}

fn verified_download_path(dir: &std::path::Path, asset_name: &str, expected_sha: &str) -> PathBuf {
    dir.join(expected_sha).join(asset_name)
}

fn matching_sha(path: &std::path::Path, expected_sha: &str) -> bool {
    path.is_file() && stream_sha(path).is_ok_and(|got| got == expected_sha)
}

fn asset_for_target(kind: AssetKind) -> Result<&'static str> {
    asset_for(std::env::consts::OS, std::env::consts::ARCH, kind)
}

fn asset_for(os: &str, arch: &str, kind: AssetKind) -> Result<&'static str> {
    match (os, arch, kind) {
        ("linux", "x86_64", AssetKind::Binary) => Ok("geyserlite-linux-amd64"),
        ("linux", "x86_64", AssetKind::Library) => Ok("libgeyserlite-linux-amd64.so"),
        ("linux", "aarch64", AssetKind::Binary) => Ok("geyserlite-linux-arm64"),
        ("linux", "aarch64", AssetKind::Library) => Ok("libgeyserlite-linux-arm64.so"),
        ("windows", "x86_64", AssetKind::Binary) => Ok("geyserlite-windows-amd64.exe"),
        _ => Err(Error::UnsupportedTarget),
    }
}

async fn fetch_expected_sha(base: &str, version: &str, asset_name: &str) -> Result<String> {
    let url = format!("{base}/{version}/checksums.txt");
    let body = http_get_text(&url).await?;
    for line in body.lines() {
        let mut fields = line.split_whitespace();
        let (Some(sha), Some(name)) = (fields.next(), fields.next()) else {
            continue;
        };
        // sha256sum -b emits "*<filename>"
        let name = name.strip_prefix('*').unwrap_or(name);
        if name == asset_name || name.ends_with(&format!("/{asset_name}")) {
            return Ok(sha.to_ascii_lowercase());
        }
    }
    Err(Error::Io(std::io::Error::other(format!(
        "{asset_name} not listed in checksums.txt for {version}"
    ))))
}

async fn http_get_text(url: &str) -> Result<String> {
    let client = reqwest::Client::builder()
        .timeout(Duration::from_secs(30))
        .build()
        .map_err(reqwest_err)?;
    let resp = client.get(url).send().await.map_err(reqwest_err)?;
    if !resp.status().is_success() {
        return Err(Error::Http {
            status: resp.status().as_u16(),
            url: url.into(),
        });
    }
    resp.text().await.map_err(reqwest_err)
}

async fn download_to(url: &str, dest: &std::path::Path) -> Result<()> {
    let client = reqwest::Client::builder()
        .timeout(Duration::from_secs(300))
        .build()
        .map_err(reqwest_err)?;
    let mut resp = client.get(url).send().await.map_err(reqwest_err)?;
    if !resp.status().is_success() {
        return Err(Error::Http {
            status: resp.status().as_u16(),
            url: url.into(),
        });
    }
    let mut f = fs::OpenOptions::new()
        .create(true)
        .truncate(true)
        .write(true)
        .open(dest)?;
    while let Some(chunk) = resp.chunk().await.map_err(reqwest_err)? {
        f.write_all(&chunk)?;
    }
    Ok(())
}

fn reqwest_err(e: reqwest::Error) -> Error {
    Error::Io(std::io::Error::other(e))
}

/// Stream a file through Sha256 with a 64 KiB buffer — caps RAM regardless
/// of file size. Used post-download to verify against the manifest.
fn stream_sha(path: &std::path::Path) -> Result<String> {
    let mut f = fs::File::open(path)?;
    let mut hasher = Sha256::new();
    let mut buf = [0u8; 64 * 1024];
    loop {
        let n = f.read(&mut buf)?;
        if n == 0 {
            break;
        }
        hasher.update(&buf[..n]);
    }
    Ok(hex_lower(&hasher.finalize()))
}

#[cfg(test)]
mod tests {
    use std::path::Path;

    use super::{AssetKind, asset_for, verified_download_path};

    #[test]
    fn verified_download_path_includes_expected_sha() {
        let dir = Path::new("/cache/geyserlite/v1.2.3");
        let asset_name = "geyserlite-windows-amd64.exe";
        let expected_sha = "a".repeat(64);

        let got = verified_download_path(dir, asset_name, &expected_sha);

        assert_eq!(
            got,
            dir.join(&expected_sha).join("geyserlite-windows-amd64.exe")
        );
    }

    #[test]
    fn asset_for_release_targets() {
        let cases = [
            (
                "linux amd64 binary",
                "linux",
                "x86_64",
                AssetKind::Binary,
                Ok("geyserlite-linux-amd64"),
            ),
            (
                "linux arm64 library",
                "linux",
                "aarch64",
                AssetKind::Library,
                Ok("libgeyserlite-linux-arm64.so"),
            ),
            (
                "windows amd64 binary",
                "windows",
                "x86_64",
                AssetKind::Binary,
                Ok("geyserlite-windows-amd64.exe"),
            ),
            (
                "windows library unsupported",
                "windows",
                "x86_64",
                AssetKind::Library,
                Err(()),
            ),
            (
                "windows arm64 unsupported",
                "windows",
                "aarch64",
                AssetKind::Binary,
                Err(()),
            ),
            (
                "darwin unsupported",
                "macos",
                "aarch64",
                AssetKind::Binary,
                Err(()),
            ),
        ];

        for (name, os, arch, kind, want) in cases {
            let got = asset_for(os, arch, kind);
            match (got, want) {
                (Ok(got), Ok(want)) => assert_eq!(got, want, "{name}"),
                (Err(_), Err(())) => {}
                (got, want) => panic!("{name}: got {got:?}, want {want:?}"),
            }
        }
    }
}

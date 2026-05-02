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
    let base = opts.mirror.as_deref().unwrap_or(DEFAULT_DOWNLOAD_BASE).trim_end_matches('/');
    let asset_name = asset_for_target(kind)?;

    let cache_dir = cache_root()?.join("geyserlite").join(version);
    let cached_path = cache_dir.join(asset_name);

    // Trust the version-keyed dirname for warm-cache hits — full re-hash on
    // every Server::start would re-read 100 MB just to confirm what the
    // file's parent dir already names. Fresh downloads still verify (below).
    if cached_path.is_file() {
        return Ok(cached_path);
    }

    fs::create_dir_all(&cache_dir)?;
    let url = format!("{base}/{version}/{asset_name}");
    let tmp = cached_path.with_extension("tmp");
    download_to(&url, &tmp).await?;

    let expected = fetch_expected_sha(base, version, asset_name).await?;
    let got = stream_sha(&tmp)?;
    if got != expected {
        let _ = fs::remove_file(&tmp);
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
            let mut perm = fs::metadata(&tmp)?.permissions();
            perm.set_mode(0o755);
            fs::set_permissions(&tmp, perm)?;
        }
    }
    fs::rename(&tmp, &cached_path)?;
    Ok(cached_path)
}

#[cfg(all(target_os = "linux", target_arch = "x86_64"))]
fn asset_for_target(kind: AssetKind) -> Result<&'static str> {
    Ok(match kind {
        AssetKind::Binary => "geyserlite-linux-amd64",
        AssetKind::Library => "libgeyserlite-linux-amd64.so",
    })
}

#[cfg(all(target_os = "linux", target_arch = "aarch64"))]
fn asset_for_target(kind: AssetKind) -> Result<&'static str> {
    Ok(match kind {
        AssetKind::Binary => "geyserlite-linux-arm64",
        AssetKind::Library => "libgeyserlite-linux-arm64.so",
    })
}

#[cfg(not(all(target_os = "linux", any(target_arch = "x86_64", target_arch = "aarch64"))))]
fn asset_for_target(_kind: AssetKind) -> Result<&'static str> {
    Err(Error::UnsupportedTarget)
}

async fn fetch_expected_sha(base: &str, version: &str, asset_name: &str) -> Result<String> {
    let url = format!("{base}/{version}/checksums.txt");
    let body = http_get_text(&url).await?;
    for line in body.lines() {
        let mut fields = line.split_whitespace();
        let (Some(sha), Some(name)) = (fields.next(), fields.next()) else { continue };
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
    Ok(format!("{:x}", hasher.finalize()))
}

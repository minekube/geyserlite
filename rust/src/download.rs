// SPDX-License-Identifier: MIT
//! Auto-download path — compiled only with `--features download`.
//!
//! Mirrors `go/download.go`: fetch the release asset matching the runtime
//! arch into `dirs::cache_dir()`, verifying its sha256 against the
//! release's `checksums.txt` manifest. Idempotent — same sha → reuse.

use std::fs;
use std::io::Write;
use std::path::PathBuf;
use std::time::Duration;

use sha2::{Digest, Sha256};

use crate::error::{Error, Result};
use crate::options::Options;
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

    let expected_sha = fetch_expected_sha(base, version, asset_name).await?;

    if let Ok(data) = std::fs::read(&cached_path) {
        let got = sha256_hex(&data);
        if got == expected_sha {
            return Ok(cached_path);
        }
    }

    fs::create_dir_all(&cache_dir).map_err(Error::Io)?;
    let url = format!("{base}/{version}/{asset_name}");
    let tmp = cached_path.with_extension("tmp");
    download_to(&url, &tmp).await?;
    let got_sha = sha256_hex(&fs::read(&tmp).map_err(Error::Io)?);
    if got_sha != expected_sha {
        let _ = fs::remove_file(&tmp);
        return Err(Error::Io(std::io::Error::other(format!(
            "sha256 mismatch for {asset_name}: got {got_sha}, want {expected_sha}",
        ))));
    }
    if let AssetKind::Binary = kind {
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let mut perm = fs::metadata(&tmp).map_err(Error::Io)?.permissions();
            perm.set_mode(0o755);
            fs::set_permissions(&tmp, perm).map_err(Error::Io)?;
        }
    }
    fs::rename(&tmp, &cached_path).map_err(Error::Io)?;
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
    Err(Error::Io(std::io::Error::other(
        "auto-download supports linux amd64/arm64 only; set Options.binary_path or Options.library_path manually",
    )))
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
        return Err(Error::Io(std::io::Error::other(format!("http {} for {}", resp.status(), url))));
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
        return Err(Error::Io(std::io::Error::other(format!("http {} for {}", resp.status(), url))));
    }
    let mut f = fs::OpenOptions::new()
        .create(true)
        .truncate(true)
        .write(true)
        .open(dest)
        .map_err(Error::Io)?;
    while let Some(chunk) = resp.chunk().await.map_err(reqwest_err)? {
        f.write_all(&chunk).map_err(Error::Io)?;
    }
    Ok(())
}

fn reqwest_err(e: reqwest::Error) -> Error {
    Error::Io(std::io::Error::other(e))
}

fn cache_root() -> Result<PathBuf> {
    if let Ok(p) = std::env::var("XDG_CACHE_HOME") {
        if !p.is_empty() {
            return Ok(PathBuf::from(p));
        }
    }
    if let Some(home) = std::env::var_os("HOME") {
        let mut p = PathBuf::from(home);
        p.push(".cache");
        return Ok(p);
    }
    Err(Error::Io(std::io::Error::other("cannot determine user cache dir")))
}

fn sha256_hex(b: &[u8]) -> String {
    let mut hasher = Sha256::new();
    hasher.update(b);
    let sum = hasher.finalize();
    const HEX: &[u8; 16] = b"0123456789abcdef";
    let mut s = String::with_capacity(sum.len() * 2);
    for &x in sum.iter() {
        s.push(HEX[(x >> 4) as usize] as char);
        s.push(HEX[(x & 0x0f) as usize] as char);
    }
    s
}

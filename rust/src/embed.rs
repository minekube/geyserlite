// SPDX-License-Identifier: MIT
//! Embedded native asset support — compiled only with `--features embed`.
//!
//! Mirrors `go/embed_*.go`: per-target `include_bytes!` of the ELF + .so,
//! self-extracting to the user cache dir on first start.

use std::fs;
use std::io::Write;
use std::path::PathBuf;

use sha2::{Digest, Sha256};

use crate::error::{Error, Result};

/// Returns the on-disk path of the extracted asset, writing it from the
/// embedded blob if not already present. The cache key is the blob's
/// sha256, so the same build always reuses the same cached file across
/// invocations and different builds get different cache entries.
pub(crate) fn extract_asset(blob: &[u8], name: &str, executable: bool) -> Result<Option<PathBuf>> {
    if blob.is_empty() {
        return Ok(None);
    }
    let mut hasher = Sha256::new();
    hasher.update(blob);
    let sha = hex(&hasher.finalize());

    let mut dir = cache_dir()?;
    dir.push("geyserlite");
    dir.push(&sha);
    let path = dir.join(name);

    if let Ok(meta) = fs::metadata(&path) {
        if meta.len() as usize == blob.len() {
            return Ok(Some(path));
        }
    }

    fs::create_dir_all(&dir).map_err(Error::Io)?;
    let tmp = path.with_extension("tmp");
    {
        let mut f = fs::OpenOptions::new()
            .create(true)
            .truncate(true)
            .write(true)
            .open(&tmp)
            .map_err(Error::Io)?;
        f.write_all(blob).map_err(Error::Io)?;
    }
    #[cfg(unix)]
    if executable {
        use std::os::unix::fs::PermissionsExt;
        let mut perm = fs::metadata(&tmp).map_err(Error::Io)?.permissions();
        perm.set_mode(0o755);
        fs::set_permissions(&tmp, perm).map_err(Error::Io)?;
    }
    #[cfg(not(unix))]
    let _ = executable;

    fs::rename(&tmp, &path).map_err(Error::Io)?;
    Ok(Some(path))
}

fn cache_dir() -> Result<PathBuf> {
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

fn hex(b: &[u8]) -> String {
    const HEX: &[u8; 16] = b"0123456789abcdef";
    let mut s = String::with_capacity(b.len() * 2);
    for &x in b {
        s.push(HEX[(x >> 4) as usize] as char);
        s.push(HEX[(x & 0x0f) as usize] as char);
    }
    s
}

// ── Per-target embedded blobs ────────────────────────────────────────────

#[cfg(all(target_os = "linux", target_arch = "x86_64"))]
pub(crate) const EMBEDDED_BINARY: &[u8] = include_bytes!("../assets/geyserlite-linux-amd64");
#[cfg(all(target_os = "linux", target_arch = "x86_64"))]
pub(crate) const EMBEDDED_LIBRARY: &[u8] = include_bytes!("../assets/libgeyserlite-linux-amd64.so");

#[cfg(all(target_os = "linux", target_arch = "aarch64"))]
pub(crate) const EMBEDDED_BINARY: &[u8] = include_bytes!("../assets/geyserlite-linux-arm64");
#[cfg(all(target_os = "linux", target_arch = "aarch64"))]
pub(crate) const EMBEDDED_LIBRARY: &[u8] = include_bytes!("../assets/libgeyserlite-linux-arm64.so");

#[cfg(not(all(target_os = "linux", any(target_arch = "x86_64", target_arch = "aarch64"))))]
pub(crate) const EMBEDDED_BINARY: &[u8] = &[];
#[cfg(not(all(target_os = "linux", any(target_arch = "x86_64", target_arch = "aarch64"))))]
pub(crate) const EMBEDDED_LIBRARY: &[u8] = &[];

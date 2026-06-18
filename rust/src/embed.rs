// SPDX-License-Identifier: MIT
//! Embedded native asset support — compiled only with `--features embed`.
//!
//! Mirrors `go/embed_*.go`: per-target `include_bytes!` of the ELF + .so,
//! self-extracting to the user cache dir on first start.

use std::fs;
use std::io::Write;
use std::path::PathBuf;

use sha2::{Digest, Sha256};

use crate::error::Result;
use crate::hex::hex_lower;
use crate::paths::cache_root;

/// Returns the on-disk path of the extracted asset, writing it from the
/// embedded blob if not already present. The cache key is the blob's
/// sha256, so the same build always reuses the same cached file across
/// invocations and different builds get different cache entries.
pub(crate) fn extract_asset(blob: &[u8], name: &str, executable: bool) -> Result<Option<PathBuf>> {
    if blob.is_empty() {
        return Ok(None);
    }
    let sha = hex_lower(&Sha256::digest(blob));

    let mut dir = cache_root()?;
    dir.push("geyserlite");
    dir.push(&sha);
    let path = dir.join(name);

    if let Ok(meta) = fs::metadata(&path) {
        if meta.len() as usize == blob.len() {
            return Ok(Some(path));
        }
    }

    fs::create_dir_all(&dir)?;
    let tmp = path.with_extension("tmp");
    {
        let mut f = fs::OpenOptions::new()
            .create(true)
            .truncate(true)
            .write(true)
            .open(&tmp)?;
        f.write_all(blob)?;
    }
    #[cfg(unix)]
    if executable {
        use std::os::unix::fs::PermissionsExt;
        let mut perm = fs::metadata(&tmp)?.permissions();
        perm.set_mode(0o755);
        fs::set_permissions(&tmp, perm)?;
    }
    #[cfg(not(unix))]
    let _ = executable;

    fs::rename(&tmp, &path)?;
    Ok(Some(path))
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

#[cfg(not(all(
    target_os = "linux",
    any(target_arch = "x86_64", target_arch = "aarch64")
)))]
pub(crate) const EMBEDDED_BINARY: &[u8] = &[];
#[cfg(not(all(
    target_os = "linux",
    any(target_arch = "x86_64", target_arch = "aarch64")
)))]
pub(crate) const EMBEDDED_LIBRARY: &[u8] = &[];

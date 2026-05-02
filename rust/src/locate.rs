// SPDX-License-Identifier: MIT
//! Locate the native ELF binary or shared library at runtime.

use std::path::{Path, PathBuf};

use crate::error::{Error, Result};
use crate::options::Options;

/// Resolution order:
///   1. opts.binary_path
///   2. $GEYSERLITE_BINARY
///   3. embedded blob (with --features embed)
///   4. $PATH lookup
///   5. auto-download (with --features download, opts.offline=false)
pub(crate) async fn locate_binary(opts: &Options) -> Result<PathBuf> {
    if let Some(p) = &opts.binary_path {
        let p = PathBuf::from(p);
        ensure_executable(&p)?;
        return Ok(p);
    }
    if let Ok(env) = std::env::var("GEYSERLITE_BINARY") {
        if !env.is_empty() {
            let p = PathBuf::from(env);
            ensure_executable(&p)?;
            return Ok(p);
        }
    }
    if let Some(p) = extract_embedded_binary()? {
        return Ok(p);
    }
    if let Ok(p) = which("geyserlite") {
        return Ok(p);
    }
    if !opts.offline {
        if let Ok(p) = try_download_binary(opts).await {
            return Ok(p);
        }
    }
    Err(Error::NoBinary)
}

/// Resolution order mirrors [`locate_binary`] with system lib dirs in
/// place of `$PATH`.
pub(crate) async fn locate_library(opts: &Options) -> Result<PathBuf> {
    if let Some(p) = &opts.library_path {
        let p = PathBuf::from(p);
        ensure_file(&p)?;
        return Ok(p);
    }
    if let Ok(env) = std::env::var("GEYSERLITE_LIBRARY") {
        if !env.is_empty() {
            let p = PathBuf::from(env);
            ensure_file(&p)?;
            return Ok(p);
        }
    }
    if let Some(p) = extract_embedded_library()? {
        return Ok(p);
    }
    for dir in system_lib_dirs() {
        let p = dir.join(library_name());
        if p.is_file() {
            return Ok(p);
        }
    }
    if !opts.offline {
        if let Ok(p) = try_download_library(opts).await {
            return Ok(p);
        }
    }
    Err(Error::NoLibrary)
}

#[cfg(target_os = "linux")]
fn library_name() -> &'static str {
    "libgeyserlite.so"
}
#[cfg(target_os = "macos")]
fn library_name() -> &'static str {
    "libgeyserlite.dylib"
}
#[cfg(target_os = "windows")]
fn library_name() -> &'static str {
    "geyserlite.dll"
}

fn system_lib_dirs() -> Vec<PathBuf> {
    let mut dirs = vec![PathBuf::from("/usr/local/lib"), PathBuf::from("/usr/lib")];
    if let Ok(env) = std::env::var("LD_LIBRARY_PATH") {
        for d in env.split(':').filter(|d| !d.is_empty()) {
            dirs.insert(0, PathBuf::from(d));
        }
    }
    dirs
}

fn ensure_executable(p: &Path) -> Result<()> {
    let meta = std::fs::metadata(p)?;
    if !meta.is_file() {
        return Err(Error::Io(std::io::Error::other(format!(
            "not a regular file: {}",
            p.display()
        ))));
    }
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        if meta.permissions().mode() & 0o111 == 0 {
            return Err(Error::Io(std::io::Error::other(format!(
                "not executable: {}",
                p.display()
            ))));
        }
    }
    Ok(())
}

fn ensure_file(p: &Path) -> Result<()> {
    let meta = std::fs::metadata(p)?;
    if !meta.is_file() {
        return Err(Error::Io(std::io::Error::other(format!(
            "not a regular file: {}",
            p.display()
        ))));
    }
    Ok(())
}

fn which(name: &str) -> Result<PathBuf> {
    let path_env = std::env::var_os("PATH").ok_or(Error::NoBinary)?;
    for dir in std::env::split_paths(&path_env) {
        let p = dir.join(name);
        if p.is_file() && ensure_executable(&p).is_ok() {
            return Ok(p);
        }
    }
    Err(Error::NoBinary)
}

#[cfg(not(feature = "embed"))]
fn extract_embedded_binary() -> Result<Option<PathBuf>> {
    Ok(None)
}
#[cfg(not(feature = "embed"))]
fn extract_embedded_library() -> Result<Option<PathBuf>> {
    Ok(None)
}

#[cfg(feature = "embed")]
fn extract_embedded_binary() -> Result<Option<PathBuf>> {
    crate::embed::extract_asset(crate::embed::EMBEDDED_BINARY, "geyserlite", true)
}
#[cfg(feature = "embed")]
fn extract_embedded_library() -> Result<Option<PathBuf>> {
    crate::embed::extract_asset(crate::embed::EMBEDDED_LIBRARY, "libgeyserlite.so", false)
}

#[cfg(feature = "download")]
async fn try_download_binary(opts: &Options) -> Result<PathBuf> {
    crate::download::download_asset(opts, crate::download::AssetKind::Binary).await
}
#[cfg(feature = "download")]
async fn try_download_library(opts: &Options) -> Result<PathBuf> {
    crate::download::download_asset(opts, crate::download::AssetKind::Library).await
}
#[cfg(not(feature = "download"))]
async fn try_download_binary(_opts: &Options) -> Result<PathBuf> {
    Err(Error::NoBinary)
}
#[cfg(not(feature = "download"))]
async fn try_download_library(_opts: &Options) -> Result<PathBuf> {
    Err(Error::NoLibrary)
}

// SPDX-License-Identifier: MIT
//! Tiny path helpers shared between embed and download modes.

use std::path::PathBuf;

use crate::error::{Error, Result};

/// User cache dir per the XDG Base Directory spec, with a `$HOME/.cache`
/// fallback. We don't pull in the `dirs` crate for ~10 lines of glue.
pub(crate) fn cache_root() -> Result<PathBuf> {
    if let Ok(p) = std::env::var("XDG_CACHE_HOME")
        && !p.is_empty()
    {
        return Ok(PathBuf::from(p));
    }
    if let Some(home) = std::env::var_os("HOME") {
        let mut p = PathBuf::from(home);
        p.push(".cache");
        return Ok(p);
    }
    Err(Error::Io(std::io::Error::other(
        "cannot determine user cache dir",
    )))
}

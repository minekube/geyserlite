// SPDX-License-Identifier: MIT
use thiserror::Error;

/// Errors returned by this crate.
#[derive(Debug, Error)]
pub enum Error {
    /// `Options::upstream` was empty.
    #[error("geyserlite: Options.upstream is required")]
    UpstreamRequired,

    /// `Options::floodgate_key` was not 16 bytes.
    ///
    /// Floodgate uses AES-128. The upstream Geyser README's `openssl genpkey
    /// -algorithm RSA` example is wrong — that's an RSA key, not AES.
    #[error("geyserlite: floodgate_key must be 16 bytes (AES-128)")]
    InvalidFloodgateKey,

    /// `Server::start` was called twice.
    #[error("geyserlite: server already started")]
    AlreadyStarted,

    /// The native ELF binary couldn't be located for [`super::Mode::Subprocess`].
    #[error("geyserlite: ELF binary not found (set Options.binary_path, $GEYSERLITE_BINARY, or build with --features embed)")]
    NoBinary,

    /// `libgeyserlite.so` couldn't be located for [`super::Mode::Embedded`].
    #[error("geyserlite: libgeyserlite.so not found (set Options.library_path, $GEYSERLITE_LIBRARY, or build with --features embed)")]
    NoLibrary,

    /// libloading or @CEntryPoint resolution failed.
    #[error("geyserlite: dlopen/symbol lookup: {0}")]
    Library(#[from] libloading::Error),

    /// libgeyserlite.so returned a non-zero status code from a lifecycle call.
    #[error("geyserlite: native call '{call}' returned {rc}")]
    NativeCall { call: &'static str, rc: i32 },

    /// Subprocess exited with a non-zero status.
    #[error("geyserlite: subprocess exited with status {0}")]
    Subprocess(i32),

    /// Subprocess restart policy exhausted.
    #[error("geyserlite: max restarts ({0}) exceeded")]
    MaxRestarts(usize),

    /// Generic IO failure.
    #[error("geyserlite: io: {0}")]
    Io(#[from] std::io::Error),
}

pub type Result<T> = std::result::Result<T, Error>;

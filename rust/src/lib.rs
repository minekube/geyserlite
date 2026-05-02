// SPDX-License-Identifier: MIT
#![doc = include_str!("../README.md")]

//! `geyserlite` — embed GeyserMC's Bedrock-Java translation in Rust programs.
//!
//! Default mode loads `libgeyserlite.so` via [`libloading`] and calls
//! `@CEntryPoint`-exported functions directly. Subprocess mode (opt-in) spawns
//! the standalone ELF via [`tokio::process`].
//!
//! # Quick start
//!
//! ```no_run
//! use geyserlite::{Server, Options, AuthType};
//!
//! # async fn run() -> anyhow::Result<()> {
//! let key = geyserlite::generate_floodgate_key();
//! Server::new(Options {
//!     listen: ":19132".into(),
//!     upstream: "127.0.0.1:25567".into(),
//!     auth_type: AuthType::Floodgate,
//!     floodgate_key: key,
//!     ..Default::default()
//! })?
//! .start()
//! .await?;
//! # Ok(())
//! # }
//! ```
//!
//! See the [project ROADMAP](https://github.com/minekube/geyserlite/blob/main/ROADMAP.md)
//! for milestones. This crate is **pre-v0.3 scaffolding**; the public API below
//! is the intended shape, but most function bodies return `not implemented`.

use thiserror::Error;

/// How Geyser authenticates Bedrock players to the upstream Java server.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub enum AuthType {
    /// AES-128 shared key + Bedrock UUID. Requires Floodgate-aware upstream (e.g. Gate).
    #[default]
    Floodgate,
    /// Forward Microsoft auth; upstream handles it.
    Online,
    /// Trust the Bedrock username; upstream is in offline mode.
    Offline,
}

/// How the Rust crate invokes Geyser.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub enum Mode {
    /// Load `libgeyserlite.so` via [`libloading`]. No subprocess. Lowest overhead.
    /// Native crash kills the host.
    #[default]
    Embedded,
    /// Spawn the geyserlite ELF via [`tokio::process`]. Crash-isolated.
    Subprocess,
}

/// Configuration for a [`Server`].
#[derive(Debug, Clone, Default)]
pub struct Options {
    /// Listen address for incoming Bedrock UDP. Defaults to `":19132"`.
    pub listen: String,
    /// Upstream Java MC address. Required.
    pub upstream: String,
    /// Auth type. Default: [`AuthType::Floodgate`].
    pub auth_type: AuthType,
    /// 16 raw bytes; required if `auth_type` is Floodgate.
    pub floodgate_key: Vec<u8>,
    /// MOTD shown to Bedrock clients.
    pub motd: Motd,
    /// Embedded vs subprocess. Default: [`Mode::Embedded`].
    pub mode: Mode,
    /// Override auto-located `libgeyserlite.so`. Embedded mode only.
    pub library_path: Option<String>,
    /// Override auto-located ELF. Subprocess mode only.
    pub binary_path: Option<String>,
    /// Override default tuned JVM args. Subprocess mode only.
    pub jvm_args: Option<Vec<String>>,
}

/// Two-line Bedrock MOTD.
#[derive(Debug, Clone, Default)]
pub struct Motd {
    pub line1: String,
    pub line2: String,
}

/// A managed geyserlite instance.
pub struct Server {
    // unexported fields
    _opts: Options,
}

/// Errors returned by this crate.
#[derive(Debug, Error)]
pub enum Error {
    #[error("not implemented (pre-v0.3 scaffolding)")]
    NotImplemented,
    #[error("io: {0}")]
    Io(#[from] std::io::Error),
    #[error("library load: {0}")]
    LibLoad(String),
}

pub type Result<T> = std::result::Result<T, Error>;

impl Server {
    /// Construct a server from [`Options`]. Does not start it.
    pub fn new(opts: Options) -> Result<Self> {
        Ok(Self { _opts: opts })
    }

    /// Start running. Returns when shutdown is requested or an unrecoverable error occurs.
    pub async fn start(&self) -> Result<()> {
        Err(Error::NotImplemented)
    }

    /// Request graceful shutdown.
    pub async fn stop(&self) -> Result<()> {
        Err(Error::NotImplemented)
    }

    /// Liveness probe.
    pub fn healthy(&self) -> bool {
        false
    }
}

/// Generate a Floodgate AES-128 key (16 random bytes).
///
/// **Don't** use the upstream Geyser README's `openssl genpkey -algorithm RSA`
/// example — that produces an RSA key, but Floodgate uses AES-128.
pub fn generate_floodgate_key() -> Vec<u8> {
    use rand::TryRngCore;
    let mut buf = vec![0u8; 16];
    rand::rngs::OsRng
        .try_fill_bytes(&mut buf)
        .expect("OS RNG should not fail");
    buf
}

/// Returns `"fly-global-services"` on Fly.io machines, `"0.0.0.0"` elsewhere.
///
/// Fly's UDP edge NATs external traffic to this hostname inside the container.
pub fn fly_global_services() -> &'static str {
    if std::env::var("FLY_APP_NAME").is_ok() {
        "fly-global-services"
    } else {
        "0.0.0.0"
    }
}

/// The tuned JVM/runtime args used by the shipped `libgeyserlite.so` at build time.
/// Useful for `Options.jvm_args` in subprocess mode.
pub fn default_jvm_args() -> Vec<String> {
    [
        "-Xmx64m",
        "-XX:MaxHeapFree=4m",
        "-XX:+CollectYoungGenerationSeparately",
        "-XX:ActiveProcessorCount=1",
        "-Dio.netty.maxDirectMemory=16777216",
        "-XX:MaxDirectMemorySize=16m",
        "-Dio.netty.allocator.type=unpooled",
        "-Dio.netty.allocator.numHeapArenas=1",
        "-Dio.netty.allocator.numDirectArenas=1",
        "-Dio.netty.eventLoopThreads=2",
        "-Dio.netty.recycler.maxCapacityPerThread=0",
        "-Dio.netty.leakDetection.level=disabled",
        "-Djava.util.concurrent.ForkJoinPool.common.parallelism=1",
        "-Dlog4j2.disableJmx=true",
    ]
    .iter()
    .map(|s| (*s).to_string())
    .collect()
}

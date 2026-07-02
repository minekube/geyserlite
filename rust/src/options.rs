// SPDX-License-Identifier: MIT
use std::time::Duration;

use crate::error::{Error, Result};

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

impl AuthType {
    pub(crate) fn as_str(&self) -> &'static str {
        match self {
            AuthType::Floodgate => "floodgate",
            AuthType::Online => "online",
            AuthType::Offline => "offline",
        }
    }
}

/// How the Rust crate invokes Geyser.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub enum Mode {
    /// Load `libgeyserlite.so` via [`libloading`]. No subprocess. Lowest overhead.
    /// Native crash kills the host.
    #[default]
    Embedded,
    /// Spawn the geyserlite native binary via [`tokio::process`]. Crash-isolated.
    Subprocess,
}

/// Two-line Bedrock MOTD.
#[derive(Debug, Clone, Default)]
pub struct Motd {
    pub line1: String,
    pub line2: String,
}

/// Subprocess restart-on-crash policy. Has no effect in [`Mode::Embedded`].
#[derive(Debug, Clone, Copy)]
pub struct RestartPolicy {
    pub min_backoff: Duration,
    pub max_backoff: Duration,
    /// 0 means infinite.
    pub max_retries: usize,
}

impl Default for RestartPolicy {
    fn default() -> Self {
        Self {
            min_backoff: Duration::from_secs(1),
            max_backoff: Duration::from_secs(60),
            max_retries: 0,
        }
    }
}

/// Configuration for a [`crate::Server`].
#[derive(Debug, Clone, Default)]
pub struct Options {
    /// Listen address for incoming Bedrock UDP. Defaults to `":19132"`.
    /// Use [`crate::fly_global_services`] on Fly.io.
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
    /// Override auto-located native binary. Subprocess mode only.
    pub binary_path: Option<String>,
    /// Override default tuned JVM args. Subprocess mode only.
    /// `None` = use [`crate::default_jvm_args`].
    pub jvm_args: Option<Vec<String>>,
    /// Subprocess restart-on-crash policy. `None` = sane defaults.
    pub restart_policy: Option<RestartPolicy>,
    /// Time to wait for graceful shutdown before SIGKILL. Default 30s.
    pub shutdown_timeout: Option<Duration>,

    /// Geyserlite release tag (e.g. `"v0.5.0"`) to fetch in the
    /// auto-download path. `None` = [`crate::DEFAULT_VERSION`]. Ignored
    /// if a binary or library is supplied via path / env / embed.
    pub version: Option<String>,

    /// Override the GitHub Release base URL. Useful for air-gapped /
    /// regulated environments. `None` = [`crate::DEFAULT_DOWNLOAD_BASE`].
    pub mirror: Option<String>,

    /// Disable the auto-download path. With `offline = true` the locator
    /// must succeed via path / env / embed / system search.
    pub offline: bool,

    /// Arbitrary YAML structure deep-merged into the generated Geyser
    /// `config.yml` AFTER the typed fields above (`listen`, `upstream`,
    /// `auth_type`, `motd`) have been applied. The escape hatch for
    /// any Geyser setting the typed surface doesn't model — `mtu`,
    /// `xbox-achievements-enabled`, `passthrough-motd`, `max-players`,
    /// anything in Geyser's `config.yml`.
    ///
    /// Nested maps merge recursively (so you can override e.g. just
    /// `bedrock.compression-level` without touching the rest of `bedrock`);
    /// leaf values overwrite. Apply your overrides last by passing them
    /// here rather than rewriting the whole config — that way the
    /// baseline-bumping that ships with new geyserlite versions still
    /// reaches you for the keys you didn't touch.
    pub config_overrides: serde_yaml_ng::Mapping,
}

impl Options {
    /// Validate and fill in defaults. Returns the cleaned-up copy.
    pub(crate) fn validated(mut self) -> Result<Self> {
        if self.upstream.is_empty() {
            return Err(Error::UpstreamRequired);
        }
        if self.auth_type == AuthType::Floodgate && self.floodgate_key.len() != 16 {
            return Err(Error::InvalidFloodgateKey);
        }
        if self.listen.is_empty() {
            self.listen = ":19132".into();
        }
        // Validate endpoints strictly: reject malformed host:port strings,
        // non-numeric ports, and out-of-range ports. Mirrors the Go
        // Options.validate() behavior.
        if let Err(e) = crate::config::split_host_port(&self.listen, "", 0) {
            let listen = &self.listen;
            return Err(crate::Error::Io(std::io::Error::other(format!(
                "geyserlite: invalid listen {listen:?}: {e}"
            ))));
        }
        if let Err(e) = crate::config::split_host_port(&self.upstream, "", 0) {
            let upstream = &self.upstream;
            return Err(crate::Error::Io(std::io::Error::other(format!(
                "geyserlite: invalid upstream {upstream:?}: {e}"
            ))));
        }
        if self.shutdown_timeout.is_none() {
            self.shutdown_timeout = Some(Duration::from_secs(30));
        }
        if self.jvm_args.is_none() {
            self.jvm_args = Some(crate::default_jvm_args());
        }
        if self.restart_policy.is_none() {
            self.restart_policy = Some(RestartPolicy::default());
        }
        Ok(self)
    }
}

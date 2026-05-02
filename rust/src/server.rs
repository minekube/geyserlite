// SPDX-License-Identifier: MIT
use std::sync::Arc;
use std::sync::atomic::{AtomicBool, Ordering};

use tokio_util::sync::CancellationToken;

use crate::embedded::EmbeddedRunner;
use crate::error::Result;
use crate::options::{Mode, Options};
use crate::subprocess::SubprocessRunner;

/// A managed geyserlite instance.
pub struct Server {
    pub(crate) opts: Options,
    pub(crate) healthy: Arc<AtomicBool>,
    pub(crate) cancel: CancellationToken,
}

impl Server {
    /// Construct a server from [`Options`]. Does not start it.
    pub fn new(opts: Options) -> Result<Self> {
        let opts = opts.validated()?;
        Ok(Self {
            opts,
            healthy: Arc::new(AtomicBool::new(false)),
            cancel: CancellationToken::new(),
        })
    }

    /// Start the server. Resolves when [`Server::stop`] is called or the
    /// underlying Geyser exits unrecoverably.
    pub async fn start(&self) -> Result<()> {
        match self.opts.mode {
            Mode::Embedded => EmbeddedRunner.run(self).await,
            Mode::Subprocess => SubprocessRunner.run(self).await,
        }
    }

    /// Request graceful shutdown. Idempotent.
    pub async fn stop(&self) {
        self.cancel.cancel();
    }

    /// Liveness probe.
    pub fn healthy(&self) -> bool {
        self.healthy.load(Ordering::Relaxed)
    }
}

// SPDX-License-Identifier: MIT
use std::sync::Arc;
use std::sync::atomic::{AtomicBool, Ordering};

use tokio_util::sync::CancellationToken;

use crate::embedded::EmbeddedRunner;
use crate::error::{Error, Result};
use crate::options::{Mode, Options};
use crate::subprocess::SubprocessRunner;

/// A managed geyserlite instance.
pub struct Server {
    pub(crate) opts: Options,
    pub(crate) healthy: Arc<AtomicBool>,
    pub(crate) cancel: CancellationToken,
    started: AtomicBool,
}

impl Server {
    /// Construct a server from [`Options`]. Does not start it.
    pub fn new(opts: Options) -> Result<Self> {
        let opts = opts.validated()?;
        Ok(Self {
            opts,
            healthy: Arc::new(AtomicBool::new(false)),
            cancel: CancellationToken::new(),
            started: AtomicBool::new(false),
        })
    }

    /// Start the server. Resolves when [`Server::stop`] is called or the
    /// underlying Geyser exits unrecoverably. Returns
    /// [`Error::AlreadyStarted`] if called while a previous run is
    /// still in progress. After `start` resolves, the server may be
    /// started again.
    pub async fn start(&self) -> Result<()> {
        if self.started.swap(true, Ordering::AcqRel) {
            return Err(Error::AlreadyStarted);
        }
        let result = match self.opts.mode {
            Mode::Embedded => EmbeddedRunner.run(self).await,
            Mode::Subprocess => SubprocessRunner.run(self).await,
        };
        // Reset so the server can be restarted.
        self.started.store(false, Ordering::Release);
        result
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

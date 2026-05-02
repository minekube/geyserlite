// SPDX-License-Identifier: MIT
use std::time::Duration;

/// Exponential backoff used by the subprocess supervisor between restart attempts.
pub(crate) struct Backoff {
    min: Duration,
    max: Duration,
    cur: Duration,
}

impl Backoff {
    pub fn new(min: Duration, max: Duration) -> Self {
        let min = if min.is_zero() { Duration::from_secs(1) } else { min };
        let max = if max < min { Duration::from_secs(60) } else { max };
        Self { min, max, cur: min }
    }
    pub fn next(&mut self) -> Duration {
        let d = self.cur;
        self.cur = (self.cur * 2).min(self.max);
        d
    }
    #[allow(dead_code)]
    pub fn reset(&mut self) {
        self.cur = self.min;
    }
}

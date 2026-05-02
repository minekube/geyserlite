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
        let min = if min.is_zero() {
            Duration::from_secs(1)
        } else {
            min
        };
        let max = if max < min {
            Duration::from_secs(60)
        } else {
            max
        };
        Self { min, max, cur: min }
    }
    pub fn next(&mut self) -> Duration {
        let d = self.cur;
        self.cur = (self.cur * 2).min(self.max);
        d
    }
    pub fn reset(&mut self) {
        self.cur = self.min;
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn exponential_with_cap() {
        let mut b = Backoff::new(Duration::from_secs(1), Duration::from_secs(8));
        assert_eq!(b.next(), Duration::from_secs(1));
        assert_eq!(b.next(), Duration::from_secs(2));
        assert_eq!(b.next(), Duration::from_secs(4));
        assert_eq!(b.next(), Duration::from_secs(8)); // capped
        assert_eq!(b.next(), Duration::from_secs(8));
    }

    #[test]
    fn reset_returns_to_min() {
        let mut b = Backoff::new(Duration::from_secs(1), Duration::from_secs(8));
        b.next();
        b.next();
        b.next();
        b.reset();
        assert_eq!(b.next(), Duration::from_secs(1));
    }

    #[test]
    fn zero_inputs_get_sane_defaults() {
        let mut b = Backoff::new(Duration::ZERO, Duration::ZERO);
        assert!(b.next() > Duration::ZERO);
    }
}

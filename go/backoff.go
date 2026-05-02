// SPDX-License-Identifier: MIT
package geyserlite

import "time"

// backoff is a non-thread-safe exponential backoff used by the subprocess
// supervisor between restart attempts. Doubles each next() call, caps at max,
// resets on a clean run.
type backoff struct {
	min, max, cur time.Duration
}

func newBackoff(minD, maxD time.Duration) *backoff {
	if minD <= 0 {
		minD = time.Second
	}
	if maxD <= 0 || maxD < minD {
		maxD = 60 * time.Second
	}
	return &backoff{min: minD, max: maxD, cur: minD}
}

func (b *backoff) next() time.Duration {
	d := b.cur
	b.cur *= 2
	if b.cur > b.max {
		b.cur = b.max
	}
	return d
}

func (b *backoff) peek() time.Duration { return b.cur }

func (b *backoff) reset() { b.cur = b.min }

// SPDX-License-Identifier: MIT
package geyserlite

import (
	"testing"
	"time"
)

func TestBackoffExponential(t *testing.T) {
	t.Parallel()
	b := newBackoff(time.Second, 8*time.Second)
	got := []time.Duration{b.next(), b.next(), b.next(), b.next(), b.next()}
	want := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second, // capped
		8 * time.Second,
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("step %d: got %s, want %s", i, got[i], w)
		}
	}
}

func TestBackoffReset(t *testing.T) {
	t.Parallel()
	b := newBackoff(time.Second, 8*time.Second)
	b.next()
	b.next()
	b.next()
	b.reset()
	if got := b.next(); got != time.Second {
		t.Errorf("after reset, got %s, want 1s", got)
	}
}

func TestBackoffDefaults(t *testing.T) {
	t.Parallel()
	// Zero / inverted values fall back to sensible defaults.
	b := newBackoff(0, 0)
	if d := b.next(); d <= 0 {
		t.Errorf("zero min should default to non-zero, got %s", d)
	}
}

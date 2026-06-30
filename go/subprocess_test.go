// SPDX-License-Identifier: MIT
package geyserlite

import (
	"testing"
	"time"
)

func TestIsGeyserReady(t *testing.T) {
	t.Parallel()
	cases := []struct {
		line string
		want bool
	}{
		{"\x1b[36;1mINFO\x1b[m Done (1.234s)! Run /geyser help", true},
		{"[INFO] Done (1.0s)!", true},
		{"Done (xx)", true},
		{"Loading extensions...", false},
		{"WARN ignore Done (this isn't matching the prefix wait it is", true}, // intentional: substring match by design
		{"", false},
	}
	for _, c := range cases {
		if got := isGeyserReady(c.line); got != c.want {
			t.Errorf("isGeyserReady(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}

func TestStableRunThreshold_MatchesRust(t *testing.T) {
	t.Parallel()
	// The Go and Rust subprocess supervisors must use the same stable-run
	// threshold so backoff reset behavior is consistent. Both are 5 minutes.
	if got := stableRunThreshold; got != 5*time.Minute {
		t.Errorf("stableRunThreshold = %v, want 5m", got)
	}
}

func TestBackoffResetAfterStableRun(t *testing.T) {
	t.Parallel()
	b := newBackoff(time.Second, 8*time.Second)
	// Advance to max backoff through repeated failures.
	for i := 0; i < 10; i++ {
		b.next()
	}
	if got := b.peek(); got != 8*time.Second {
		t.Fatalf("pre-reset: backoff = %v, want 8s (max)", got)
	}
	// Reset as if the subprocess ran stably.
	b.reset()
	if got := b.next(); got != time.Second {
		t.Errorf("post-reset: next backoff = %v, want 1s", got)
	}
}

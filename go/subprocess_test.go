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

func TestStripANSI(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"SGR color", "\x1b[32mDone\x1b[0m", "Done"},
		{"erase line", "\x1b[2KDone", "Done"},
		{"cursor position", "abc\x1b[Hdef", "abcdef"},
		{"no ANSI", "plain text", "plain text"},
		{"truncated ESC[", "truncated \x1b[", "truncated \x1b["},
		{"utf8 preserved", "utf8 café \x1b[31mred\x1b[0m", "utf8 café red"},
		{"empty", "", ""},
		{"multiple sequences", "\x1b[1m\x1b[31mB\x1b[0m", "B"},
	}
	for _, tt := range tests {
		if got := stripANSI(tt.in); got != tt.want {
			t.Errorf("%s: stripANSI(%q) = %q, want %q", tt.name, tt.in, got, tt.want)
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

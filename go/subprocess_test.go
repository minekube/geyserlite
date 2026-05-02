// SPDX-License-Identifier: MIT
package geyserlite

import "testing"

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

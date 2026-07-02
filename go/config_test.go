// SPDX-License-Identifier: MIT
package geyserlite

import (
	"reflect"
	"testing"
)

func TestBuildConfigMap_TypedOptionsApply(t *testing.T) {
	t.Parallel()
	cfg, err := buildConfigMap(Options{
		Listen:   "10.0.0.1:25599",
		Upstream: "192.168.1.5:25567",
		AuthType: Floodgate,
		MOTD:     MOTD{Line1: "alpha", Line2: "beta"},
	})
	if err != nil {
		t.Fatal(err)
	}
	bedrock := cfg["bedrock"].(map[string]any)
	if got := bedrock["address"]; got != "10.0.0.1" {
		t.Errorf("bedrock.address = %v, want 10.0.0.1", got)
	}
	if got := bedrock["port"]; got != 25599 {
		t.Errorf("bedrock.port = %v, want 25599", got)
	}
	if got := bedrock["motd1"]; got != "alpha" {
		t.Errorf("bedrock.motd1 = %v, want alpha", got)
	}
	java := cfg["java"].(map[string]any)
	if got := java["address"]; got != "192.168.1.5" {
		t.Errorf("java.address = %v, want 192.168.1.5", got)
	}
	if got := java["auth-type"]; got != "floodgate" {
		t.Errorf("java.auth-type = %v, want floodgate", got)
	}
	if got := cfg["floodgate-key-file"]; got != "key.bin" {
		t.Errorf("floodgate-key-file = %v, want key.bin (set under Floodgate)", got)
	}
}

func TestBuildConfigMap_OverridesWinDeepMerge(t *testing.T) {
	t.Parallel()
	cfg, err := buildConfigMap(Options{
		Listen:   ":19132",
		Upstream: "127.0.0.1:25565",
		ConfigOverrides: map[string]any{
			// Nested override touches a single key; siblings under
			// `bedrock` (e.g. server-name) must survive.
			"bedrock": map[string]any{
				"compression-level": 9,
			},
			// Top-level addition.
			"passthrough-motd": true,
			// Top-level overwrite.
			"max-players": 50,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	bedrock := cfg["bedrock"].(map[string]any)
	if got := bedrock["compression-level"]; got != 9 {
		t.Errorf("compression-level = %v, want 9 (override)", got)
	}
	if got, ok := bedrock["server-name"]; !ok || got == "" {
		t.Errorf("bedrock.server-name dropped by partial-bedrock override; got %v", got)
	}
	if got := cfg["passthrough-motd"]; got != true {
		t.Errorf("passthrough-motd = %v, want true", got)
	}
	if got := cfg["max-players"]; got != 50 {
		t.Errorf("max-players = %v, want 50", got)
	}
}

func TestBuildConfigMap_OverrideBeatsTypedOption(t *testing.T) {
	t.Parallel()
	// ConfigOverrides is applied AFTER typed options, so it wins.
	cfg, err := buildConfigMap(Options{
		Listen:   "10.0.0.1:19132",
		Upstream: "127.0.0.1:25565",
		ConfigOverrides: map[string]any{
			"bedrock": map[string]any{
				"port": 19999,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg["bedrock"].(map[string]any)["port"]; got != 19999 {
		t.Errorf("override should win, got port = %v", got)
	}
}

func TestMergeMap_Recursive(t *testing.T) {
	t.Parallel()
	dst := map[string]any{
		"a": map[string]any{"x": 1, "y": 2},
		"b": "leaf",
	}
	src := map[string]any{
		"a": map[string]any{"y": 99, "z": 3},
		"c": "new",
	}
	mergeMap(dst, src)
	want := map[string]any{
		"a": map[string]any{"x": 1, "y": 99, "z": 3},
		"b": "leaf",
		"c": "new",
	}
	if !reflect.DeepEqual(dst, want) {
		t.Errorf("merged = %#v, want %#v", dst, want)
	}
}

func TestMergeMap_LeafOverwritesMap(t *testing.T) {
	t.Parallel()
	// If src has a leaf where dst has a map, src wins (no merge).
	dst := map[string]any{"a": map[string]any{"x": 1}}
	src := map[string]any{"a": "string"}
	mergeMap(dst, src)
	if got := dst["a"]; got != "string" {
		t.Errorf("leaf-replaces-map: got %v, want string", got)
	}
}

func TestSplitHostPort(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in       string
		wantHost string
		wantPort int
		wantErr  bool
	}{
		// Valid shorthand forms.
		{"", "0.0.0.0", 19132, false},
		{":19133", "0.0.0.0", 19133, false},
		{"10.0.0.1:25567", "10.0.0.1", 25567, false},
		{"[::1]:25567", "::1", 25567, false},
		{"[::1]", "::1", 19132, false},
		{"localhost", "localhost", 19132, false},
		{"localhost:25565", "localhost", 25565, false},
		{"[::1]:19132", "::1", 19132, false},
		// Invalid — must fail loudly, no silent fallback.
		{":abc", "", 0, true},
		{"127.0.0.1:abc", "", 0, true},
		{"127.0.0.1:99999", "", 0, true},
		{"127.0.0.1:0", "", 0, true},
		{"host:12x", "", 0, true},
		{"host:-1", "", 0, true},
		{"[::1", "", 0, true},       // unclosed bracket
		{"[::1]abc", "", 0, true},   // junk after bracket
		{"host:port:notvalid", "", 0, true},
	}
	for _, tt := range tests {
		host, port, err := splitHostPort(tt.in, "0.0.0.0", 19132)
		if tt.wantErr {
			if err == nil {
				t.Errorf("splitHostPort(%q) = (%q, %d, nil), want error", tt.in, host, port)
			}
			continue
		}
		if err != nil {
			t.Errorf("splitHostPort(%q) unexpected error: %v", tt.in, err)
			continue
		}
		if host != tt.wantHost || port != tt.wantPort {
			t.Errorf("splitHostPort(%q) = (%q, %d), want (%q, %d)",
				tt.in, host, port, tt.wantHost, tt.wantPort)
		}
	}
}

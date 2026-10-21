// SPDX-License-Identifier: MIT
package geyserlite

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
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

// TestBuildConfigMap_RendersHaproxyToggleAtGeyser2Path pins the Java-side
// HAProxy toggle to the config path the bundled Geyser build actually reads.
//
// Geyser 2.x reads it from advanced.java.use-haproxy-protocol
// (GeyserConfig.AdvancedJavaConfig.useHaproxyProtocol). Its own v5 migration
// only rewrites the pre-2.0 root key remote.use-proxy-protocol
// (ConfigMigrations.addVersion(5)); a key at java.use-proxy-protocol is in no
// migration path and is silently dropped. When that happened, the header was
// never sent and Gate's Geyser listener (which required one) closed every
// managed Bedrock join with "proxyproto: proxy protocol signature not present".
//
// Asserting the final path - and the absence of the legacy ones - keeps the
// render correct without depending on any migration being applied by the
// Geyser build that consumes it.
func TestBuildConfigMap_RendersHaproxyToggleAtGeyser2Path(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		auth AuthType
		want bool
	}{
		{name: "floodgate sends the header", auth: Floodgate, want: true},
		{name: "online mode does not", auth: Online, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := buildConfigMap(Options{
				Listen:   ":19132",
				Upstream: "127.0.0.1:25567",
				AuthType: tc.auth,
			})
			if err != nil {
				t.Fatal(err)
			}

			advanced, ok := cfg["advanced"].(map[string]any)
			if !ok {
				t.Fatalf("rendered config has no advanced section; got %v", cfg["advanced"])
			}
			javaAdvanced, ok := advanced["java"].(map[string]any)
			if !ok {
				t.Fatalf("rendered config has no advanced.java section; got %v", advanced["java"])
			}
			got, ok := javaAdvanced["use-haproxy-protocol"]
			if !ok {
				t.Fatalf("advanced.java.use-haproxy-protocol missing from rendered config; "+
					"Geyser 2.x ignores every other path (advanced.java = %v)", javaAdvanced)
			}
			if got != tc.want {
				t.Errorf("advanced.java.use-haproxy-protocol = %v, want %v", got, tc.want)
			}

			// The legacy paths must stay absent: Geyser drops both silently, so
			// rendering either one again is the same outage.
			if java, ok := cfg["java"].(map[string]any); ok {
				if legacy, ok := java["use-proxy-protocol"]; ok {
					t.Errorf("rendered config still carries the pre-2.0 java.use-proxy-protocol = %v, "+
						"which Geyser 2.x drops without an error", legacy)
				}
			}
			if remote, ok := cfg["remote"]; ok {
				t.Errorf("rendered config carries a pre-v5 remote section (%v); "+
					"the toggle must be at its final path, not reachable only through a migration", remote)
			}

			// Prove the emitted YAML (what Geyser parses) carries the toggle too,
			// not just the in-memory map.
			out, err := yaml.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if want := fmt.Sprintf("use-haproxy-protocol: %t", tc.want); !strings.Contains(string(out), want) {
				t.Errorf("rendered YAML is missing %q:\n%s", want, out)
			}
		})
	}
}

func TestBuildConfigMap_DoesNotForwardPlayerPing(t *testing.T) {
	t.Parallel()
	// forward-player-ping makes Geyser answer Java keepalive/ping packets only
	// when the Bedrock client echoes NetworkStackLatency. A client that stops
	// echoing (backgrounded app, lossy UDP leg) then starves the Java backend's
	// keepalive and the player is kicked with "Timed out" ~30s later. Keep the
	// upstream default (false) so MCPL answers keepalives on its own.
	cfg, err := buildConfigMap(Options{Listen: ":19132", Upstream: "127.0.0.1:25565"})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := cfg["forward-player-ping"]
	if !ok {
		t.Fatal("forward-player-ping missing from rendered config")
	}
	if got != false {
		t.Errorf("forward-player-ping = %v, want false", got)
	}
}

func TestBuildConfigMap_ForwardPlayerPingOverrideWins(t *testing.T) {
	t.Parallel()
	// Operators can still opt back in explicitly.
	cfg, err := buildConfigMap(Options{
		Listen:   ":19132",
		Upstream: "127.0.0.1:25565",
		ConfigOverrides: map[string]any{
			"forward-player-ping": true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg["forward-player-ping"]; got != true {
		t.Errorf("explicit override should win, got = %v", got)
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
	}{
		{"", "0.0.0.0", 19132},
		{":19133", "0.0.0.0", 19133},
		{"10.0.0.1:25567", "10.0.0.1", 25567},
		{"[::1]:25567", "::1", 25567},
		{"[::1]", "::1", 19132},
		{"localhost", "localhost", 19132},
		{"host:notaport", "host", 19132},
	}
	for _, tt := range tests {
		host, port := splitHostPort(tt.in, "0.0.0.0", 19132)
		if host != tt.wantHost || port != tt.wantPort {
			t.Errorf("splitHostPort(%q) = (%q, %d), want (%q, %d)",
				tt.in, host, port, tt.wantHost, tt.wantPort)
		}
	}
}

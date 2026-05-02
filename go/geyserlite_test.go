// SPDX-License-Identifier: MIT
package geyserlite

import (
	"errors"
	"strings"
	"testing"
)

func TestOptionsValidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		opts Options
		want error
	}{
		{
			name: "missing upstream",
			opts: Options{},
			want: ErrUpstreamRequired,
		},
		{
			name: "floodgate without key",
			opts: Options{Upstream: "127.0.0.1:25567"},
			want: ErrInvalidFloodgateKey,
		},
		{
			name: "floodgate with wrong-size key",
			opts: Options{Upstream: "127.0.0.1:25567", FloodgateKey: []byte("too short")},
			want: ErrInvalidFloodgateKey,
		},
		{
			name: "offline auth needs no key",
			opts: Options{Upstream: "127.0.0.1:25567", AuthType: Offline},
			want: nil,
		},
		{
			name: "valid floodgate",
			opts: Options{
				Upstream:     "127.0.0.1:25567",
				AuthType:     Floodgate,
				FloodgateKey: make([]byte, 16),
			},
			want: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := tc.opts.validate()
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestOptionsValidateDefaults(t *testing.T) {
	t.Parallel()
	opts, err := Options{
		Upstream:     "127.0.0.1:25567",
		AuthType:     Floodgate,
		FloodgateKey: make([]byte, 16),
	}.validate()
	if err != nil {
		t.Fatal(err)
	}
	if opts.Listen != ":19132" {
		t.Errorf("Listen default: got %q, want %q", opts.Listen, ":19132")
	}
	if opts.ShutdownTimeout == 0 {
		t.Error("ShutdownTimeout should default")
	}
	if opts.JVMArgs == nil {
		t.Error("JVMArgs should default")
	}
	if opts.RestartPolicy == nil {
		t.Error("RestartPolicy should default")
	}
}

func TestGenerateFloodgateKey(t *testing.T) {
	t.Parallel()
	k1, err := GenerateFloodgateKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(k1) != 16 {
		t.Fatalf("len = %d, want 16", len(k1))
	}
	k2, err := GenerateFloodgateKey()
	if err != nil {
		t.Fatal(err)
	}
	// Astronomically unlikely to collide.
	if string(k1) == string(k2) {
		t.Fatal("two consecutive keys are equal")
	}
}

func TestNewRequiresUpstream(t *testing.T) {
	t.Parallel()
	_, err := New(Options{})
	if !errors.Is(err, ErrUpstreamRequired) {
		t.Fatalf("got %v, want ErrUpstreamRequired", err)
	}
}

func TestSplitHostPort(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, host, port string
	}{
		{"127.0.0.1:25567", "127.0.0.1", "25567"},
		{":19132", "0.0.0.0", "19132"},
		{"example.com:1234", "example.com", "1234"},
		{"[::1]:25567", "::1", "25567"},
		{"fly-global-services:19132", "fly-global-services", "19132"},
	}
	for _, c := range cases {
		host, port := splitHostPort(c.in, "0.0.0.0", "19132")
		if host != c.host || port != c.port {
			t.Errorf("splitHostPort(%q) = (%q, %q), want (%q, %q)", c.in, host, port, c.host, c.port)
		}
	}
}

func TestRenderConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	opts, err := Options{
		Upstream:     "127.0.0.1:25567",
		AuthType:     Floodgate,
		FloodgateKey: make([]byte, 16),
		Listen:       "fly-global-services:19132",
		MOTD:         MOTD{Line1: "test", Line2: "msg"},
	}.validate()
	if err != nil {
		t.Fatal(err)
	}
	if err := renderConfig(dir, opts); err != nil {
		t.Fatal(err)
	}
	contents, err := readFile(dir + "/config.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"address: fly-global-services",
		"port: 19132",
		"address: 127.0.0.1",
		"port: 25567",
		"auth-type: floodgate",
		"floodgate-key-file: key.bin",
		"motd1: 'test'",
	} {
		if !strings.Contains(contents, want) {
			t.Errorf("config.yml missing %q", want)
		}
	}
}

func readFile(p string) (string, error) {
	b, err := readFileBytes(p)
	return string(b), err
}

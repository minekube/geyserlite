// SPDX-License-Identifier: MIT
package gate

import (
	"context"
	"strings"
	"testing"

	geyserlite "go.minekube.com/geyserlite"
)

func TestNew_DisabledReturnsNilNil(t *testing.T) {
	t.Parallel()
	b, err := New(Config{Enabled: false}, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if b != nil {
		t.Fatalf("want nil Bedrock, got %v", b)
	}
}

func TestNilReceiverIsNoOp(t *testing.T) {
	t.Parallel()
	var b *Bedrock
	if err := b.Start(context.Background()); err != nil {
		t.Fatalf("nil Start: %v", err)
	}
	if err := b.Stop(context.Background()); err != nil {
		t.Fatalf("nil Stop: %v", err)
	}
	if b.Healthy() {
		t.Fatalf("nil Bedrock should not be Healthy")
	}
}

func TestConfigToOptions_FloodgateHex(t *testing.T) {
	t.Parallel()
	c := Config{
		Enabled:      true,
		Listen:       "0.0.0.0:19132",
		Upstream:     "127.0.0.1:25567",
		AuthType:     "floodgate",
		FloodgateKey: "0123456789abcdef0123456789abcdef",
		MOTD:         MOTDConfig{Line1: "a", Line2: "b"},
	}
	opts, err := c.toOptions(nil)
	if err != nil {
		t.Fatalf("toOptions: %v", err)
	}
	if opts.AuthType != geyserlite.Floodgate {
		t.Fatalf("AuthType = %v", opts.AuthType)
	}
	if len(opts.FloodgateKey) != 16 {
		t.Fatalf("FloodgateKey len = %d, want 16", len(opts.FloodgateKey))
	}
	if opts.MOTD.Line1 != "a" || opts.MOTD.Line2 != "b" {
		t.Fatalf("MOTD round-trip failed: %+v", opts.MOTD)
	}
}

func TestConfigToOptions_ZeroPrefix(t *testing.T) {
	t.Parallel()
	c := Config{Enabled: true, Upstream: "x:1", FloodgateKey: "0x" + strings.Repeat("ff", 16), AuthType: "floodgate"}
	opts, err := c.toOptions(nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	for i, b := range opts.FloodgateKey {
		if b != 0xff {
			t.Fatalf("byte %d = %#x, want 0xff", i, b)
		}
	}
}

func TestConfigToOptions_OnlineSkipsKey(t *testing.T) {
	t.Parallel()
	c := Config{Enabled: true, Upstream: "x:1", AuthType: "online"}
	opts, err := c.toOptions(nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if opts.AuthType != geyserlite.Online {
		t.Fatalf("AuthType = %v", opts.AuthType)
	}
	if opts.FloodgateKey != nil {
		t.Fatalf("FloodgateKey should be unset for online auth")
	}
}

func TestConfigToOptions_UnknownAuthType(t *testing.T) {
	t.Parallel()
	c := Config{Enabled: true, AuthType: "made-up"}
	if _, err := c.toOptions(nil); err == nil {
		t.Fatalf("expected error for unknown auth_type")
	}
}

func TestConfigToOptions_UnknownMode(t *testing.T) {
	t.Parallel()
	c := Config{Enabled: true, AuthType: "online", Mode: "magic"}
	if _, err := c.toOptions(nil); err == nil {
		t.Fatalf("expected error for unknown mode")
	}
}

func TestConfigToOptions_BadHexKey(t *testing.T) {
	t.Parallel()
	c := Config{Enabled: true, AuthType: "floodgate", FloodgateKey: "zzz"}
	if _, err := c.toOptions(nil); err == nil {
		t.Fatalf("expected hex decode error")
	}
}

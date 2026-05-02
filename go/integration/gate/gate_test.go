// SPDX-License-Identifier: MIT
package gate

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	geyserlite "go.minekube.com/geyserlite"
)

// fakeServer implements the [server] interface for testing. Tracks
// call counts + lets each method return a configurable error.
type fakeServer struct {
	startCalls atomic.Int32
	stopCalls  atomic.Int32
	healthy    atomic.Bool
	startErr   error
	stopErr    error
	// blockUntil, if non-nil, is closed by the test when it wants Start
	// to return. Lets us simulate a long-running listener.
	blockUntil chan struct{}
}

func (f *fakeServer) Start(ctx context.Context) error {
	f.startCalls.Add(1)
	if f.startErr != nil {
		return f.startErr
	}
	if f.blockUntil != nil {
		select {
		case <-f.blockUntil:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (f *fakeServer) Stop(_ context.Context) error {
	f.stopCalls.Add(1)
	return f.stopErr
}

func (f *fakeServer) Healthy() bool { return f.healthy.Load() }

// withFakeServer swaps newSrv for the duration of the test, restoring
// it on teardown. Returns the fake the caller can mutate.
func withFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	fake := &fakeServer{}
	prev := newSrv
	newSrv = func(_ geyserlite.Options) (server, error) { return fake, nil }
	t.Cleanup(func() { newSrv = prev })
	return fake
}

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

// validCfg returns a Config that passes toOptions validation; used by
// lifecycle tests that don't care about config edge cases.
func validCfg() Config {
	return Config{
		Enabled:      true,
		Listen:       "0.0.0.0:0",
		Upstream:     "127.0.0.1:25567",
		AuthType:     "floodgate",
		FloodgateKey: strings.Repeat("00", 16),
	}
}

func TestStart_DelegatesAndUnwrapsCancel(t *testing.T) {
	t.Parallel()
	fake := withFakeServer(t)
	fake.blockUntil = make(chan struct{}) // Start blocks until ctx canceled
	b, err := New(validCfg(), nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	startDone := make(chan error, 1)
	go func() { startDone <- b.Start(ctx) }()

	// Give Start a moment to enter the fake's block.
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-startDone:
		if err != nil {
			t.Fatalf("Start should swallow ctx.Canceled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("Start did not return after cancel")
	}
	if got := fake.startCalls.Load(); got != 1 {
		t.Fatalf("Start delegate calls = %d, want 1", got)
	}
}

func TestStart_PropagatesNonCancelErrors(t *testing.T) {
	t.Parallel()
	fake := withFakeServer(t)
	fake.startErr = errors.New("boom")
	b, _ := New(validCfg(), nil)

	if err := b.Start(context.Background()); err == nil || err.Error() != "boom" {
		t.Fatalf("Start error = %v, want boom", err)
	}
}

func TestStart_DeadlineExceededIsCleanStop(t *testing.T) {
	t.Parallel()
	fake := withFakeServer(t)
	fake.startErr = context.DeadlineExceeded
	b, _ := New(validCfg(), nil)

	if err := b.Start(context.Background()); err != nil {
		t.Fatalf("Start should swallow DeadlineExceeded, got %v", err)
	}
	_ = fake // satisfy lint
}

func TestStop_DelegatesAndPropagatesError(t *testing.T) {
	t.Parallel()
	fake := withFakeServer(t)
	fake.stopErr = errors.New("stop-boom")
	b, _ := New(validCfg(), nil)

	if err := b.Stop(context.Background()); err == nil || err.Error() != "stop-boom" {
		t.Fatalf("Stop error = %v, want stop-boom", err)
	}
	if got := fake.stopCalls.Load(); got != 1 {
		t.Fatalf("Stop delegate calls = %d, want 1", got)
	}
}

func TestHealthy_RoundTrips(t *testing.T) {
	t.Parallel()
	fake := withFakeServer(t)
	b, _ := New(validCfg(), nil)

	if b.Healthy() {
		t.Fatalf("Healthy should default false")
	}
	fake.healthy.Store(true)
	if !b.Healthy() {
		t.Fatalf("Healthy should reflect underlying server")
	}
}

func TestNew_PropagatesUnderlyingFailure(t *testing.T) {
	t.Parallel()
	prev := newSrv
	t.Cleanup(func() { newSrv = prev })
	newSrv = func(geyserlite.Options) (server, error) { return nil, errors.New("ctor-boom") }

	if _, err := New(validCfg(), nil); err == nil || err.Error() != "ctor-boom" {
		t.Fatalf("New should propagate underlying error, got %v", err)
	}
}

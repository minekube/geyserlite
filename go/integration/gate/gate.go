// SPDX-License-Identifier: MIT
package gate

import (
	"context"
	"errors"
	"log/slog"

	geyserlite "go.minekube.com/geyserlite"
)

// Bedrock is a Gate-shaped wrapper around a [geyserlite.Server]. It
// exposes the lifecycle Gate's runtime expects (Start in a goroutine,
// Stop on shutdown, Healthy for health checks) while keeping all of
// geyserlite's optionality reachable through [Config].
type Bedrock struct {
	server *geyserlite.Server
	logger *slog.Logger
}

// New constructs a [Bedrock] from a [Config]. When cfg.Enabled is
// false, returns (nil, nil) so Gate can no-op the bedrock subsystem
// without checking the disabled case at every callsite.
//
// logger may be nil; nil routes through [slog.Default].
func New(cfg Config, logger *slog.Logger) (*Bedrock, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if logger == nil {
		logger = slog.Default()
	}

	opts, err := cfg.toOptions(logger)
	if err != nil {
		return nil, err
	}

	server, err := geyserlite.New(opts)
	if err != nil {
		return nil, err
	}

	return &Bedrock{server: server, logger: logger}, nil
}

// Start runs the bedrock listener until ctx is canceled or the
// underlying Geyser exits unrecoverably. Blocks; Gate is expected to
// call this from a managed goroutine.
//
// A nil receiver is treated as a no-op (returns nil immediately) so
// callers can hand a zero-value [Bedrock] from a disabled config
// straight into their goroutine pool without nil-checking first.
func (b *Bedrock) Start(ctx context.Context) error {
	if b == nil {
		return nil
	}
	err := b.server.Start(ctx)
	// Gate's lifecycle treats ctx.Cancel as a clean stop, not an error.
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// Stop requests graceful shutdown. Idempotent; safe on a nil receiver.
//
// Returns when the listener has stopped or ctx is canceled. Gate's
// shutdown path typically passes a context.WithTimeout to bound how
// long a stuck Geyser can hold up the proxy's exit.
func (b *Bedrock) Stop(ctx context.Context) error {
	if b == nil {
		return nil
	}
	return b.server.Stop(ctx)
}

// Healthy reports whether the bedrock listener is currently accepting
// connections. False for a nil receiver (disabled). Gate plugs this
// into its health probe.
func (b *Bedrock) Healthy() bool {
	if b == nil {
		return false
	}
	return b.server.Healthy()
}

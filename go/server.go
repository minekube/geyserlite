// SPDX-License-Identifier: MIT
package geyserlite

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
)

// Server is a managed geyserlite instance. A Server is reusable: after
// Start returns, callers may Start again to spawn a fresh run. The
// "started" flag means "currently running," not "has ever run."
type Server struct {
	opts   Options
	logger *slog.Logger

	mu      sync.Mutex
	started atomic.Bool // read by Healthy without the lock; set by Start under mu

	// Set when Start runs; cleared when the run finishes.
	cancel context.CancelFunc
	done   chan struct{}
	runErr error

	runner runner
}

// runner is the mode-specific execution strategy.
type runner interface {
	// run blocks until ctx is canceled or the underlying Geyser exits.
	// It must respect ctx for graceful shutdown.
	run(ctx context.Context, s *Server) error
	// healthy reports whether Geyser is currently serving.
	healthy() bool
}

// New constructs a [Server] from [Options]. It does not start it; call [Server.Start].
func New(opts Options) (*Server, error) {
	validated, err := opts.validate()
	if err != nil {
		return nil, err
	}
	s := &Server{
		opts:   validated,
		logger: validated.Logger.With(slog.String("component", "geyserlite")),
	}
	switch validated.Mode {
	case ModeEmbedded:
		s.runner = &embeddedRunner{}
	case ModeSubprocess:
		s.runner = &subprocessRunner{}
	default:
		s.runner = &embeddedRunner{}
	}
	return s, nil
}

// Start runs the server until ctx is canceled or an unrecoverable error occurs.
// Start blocks until either of these conditions is met. Call from a goroutine
// if your program needs to do other work concurrently.
//
// Returns ctx.Err() on graceful shutdown, or the underlying error otherwise.
// After Start returns, the server may be started again.
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.started.Load() {
		s.mu.Unlock()
		return ErrAlreadyStarted
	}

	runCtx, cancel := context.WithCancel(ctx)

	s.started.Store(true)
	s.cancel = cancel
	s.done = make(chan struct{})
	s.runErr = nil

	done := s.done
	s.mu.Unlock()

	defer func() {
		cancel()

		s.mu.Lock()
		s.started.Store(false)
		s.cancel = nil
		s.mu.Unlock()

		close(done)
	}()

	err := s.runner.run(runCtx, s)
	s.runErr = err
	return err
}

// Err returns the error from the most recent [Start] call, or nil if the
// server has not been started or the last run succeeded.
func (s *Server) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runErr
}

// Stop requests a graceful shutdown. Idempotent. Returns when the server
// has stopped or ctx is canceled.
//
// Stop returns nil if:
//   - the server was already stopped (not running), or
//   - the server was running and stopped before ctx expired.
//
// Stop returns ctx.Err() only if the stop context expires before the
// server finishes shutting down. It never returns the underlying run
// error; use [Err] or the [Start] return for that.
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.started.Load() {
		s.mu.Unlock()
		return nil
	}

	cancel := s.cancel
	done := s.done
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Healthy reports whether Geyser is currently accepting connections.
//
// This is an eventually consistent signal: Healthy() reads the started
// flag atomically and then calls the runner's health check. The started
// flag may flip to false between the check and the runner call during
// teardown, so callers should treat false as authoritative but true as
// a best-effort hint.
func (s *Server) Healthy() bool {
	if !s.started.Load() {
		return false
	}
	return s.runner.healthy()
}

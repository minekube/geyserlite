// SPDX-License-Identifier: MIT
package geyserlite

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
)

// Server is a managed geyserlite instance.
type Server struct {
	opts   Options
	logger *slog.Logger

	mu      sync.Mutex
	started bool

	// Set when Start runs.
	healthy atomic.Bool
	cancel  context.CancelFunc
	done    chan struct{}
	runErr  atomic.Pointer[error]

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
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return ErrAlreadyStarted
	}
	s.started = true
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.done = make(chan struct{})
	s.mu.Unlock()

	defer close(s.done)
	defer cancel()

	err := s.runner.run(runCtx, s)
	s.runErr.Store(&err)
	return err
}

// Stop requests a graceful shutdown. Idempotent. Returns when the server
// has stopped or ctx is canceled.
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return ErrNotStarted
	}
	cancel := s.cancel
	done := s.done
	s.mu.Unlock()

	cancel()
	select {
	case <-done:
		if errp := s.runErr.Load(); errp != nil {
			return *errp
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Healthy reports whether Geyser is currently accepting connections.
func (s *Server) Healthy() bool {
	if !s.started {
		return false
	}
	return s.runner.healthy()
}

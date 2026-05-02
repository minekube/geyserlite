// SPDX-License-Identifier: MIT
package geyserlite

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"
)

// subprocessRunner spawns the geyserlite ELF via os/exec, supervises it,
// restarts on crash with exponential backoff, and forwards signals.
type subprocessRunner struct {
	healthyFlag atomic.Bool
}

func (r *subprocessRunner) healthy() bool { return r.healthyFlag.Load() }

func (r *subprocessRunner) run(ctx context.Context, s *Server) error {
	binary, err := locateBinary(ctx, s.opts)
	if err != nil {
		return err
	}
	s.logger.Info("located geyserlite binary", slog.String("path", binary))

	workdir, err := os.MkdirTemp("", "geyserlite-*")
	if err != nil {
		return fmt.Errorf("geyserlite: create workdir: %w", err)
	}
	defer os.RemoveAll(workdir)

	if err := writeFloodgateKey(workdir, s.opts); err != nil {
		return err
	}
	if err := renderConfig(workdir, s.opts); err != nil {
		return err
	}
	if err := copyPermissionsYML(workdir); err != nil {
		s.logger.Warn("could not stage permissions.yml; Geyser may regenerate it", slog.String("err", err.Error()))
	}

	policy := s.opts.RestartPolicy
	backoff := newBackoff(policy.MinBackoff, policy.MaxBackoff)

	for attempt := 0; ; attempt++ {
		if policy.MaxRetries > 0 && attempt >= policy.MaxRetries {
			return fmt.Errorf("geyserlite: max retries (%d) exceeded", policy.MaxRetries)
		}
		err := r.runOnce(ctx, s, binary, workdir)
		if err == nil || errors.Is(err, context.Canceled) {
			return ctx.Err()
		}
		s.logger.Warn("geyser exited with error; restarting after backoff",
			slog.Any("err", err),
			slog.Duration("backoff", backoff.peek()),
			slog.Int("attempt", attempt+1),
		)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff.next()):
		}
	}
}

func (r *subprocessRunner) runOnce(ctx context.Context, s *Server, binary, workdir string) error {
	args := []string{"--nogui"}
	args = append(args, s.opts.JVMArgs...)

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = workdir
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = s.opts.ShutdownTimeout
	// Place the child in its own process group so we can signal it cleanly.
	cmd.SysProcAttr = sysProcAttrNewGroup()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("geyserlite: stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("geyserlite: stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("geyserlite: start subprocess: %w", err)
	}
	s.logger.Info("started geyserlite subprocess", slog.Int("pid", cmd.Process.Pid))

	go pipeToLogger(stdout, s.logger.With(slog.String("stream", "stdout")), slog.LevelInfo, &r.healthyFlag)
	go pipeToLogger(stderr, s.logger.With(slog.String("stream", "stderr")), slog.LevelWarn, nil)

	defer r.healthyFlag.Store(false)

	err = cmd.Wait()
	if err == nil {
		return nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return fmt.Errorf("geyserlite: subprocess exited %d: %w", exitErr.ExitCode(), err)
	}
	return fmt.Errorf("geyserlite: wait subprocess: %w", err)
}

// pipeToLogger forwards each line from r to logger at the given level.
// If readyFlag is non-nil, sets it to true on first sight of Geyser's
// "Done (...)" boot completion line.
func pipeToLogger(r io.Reader, logger *slog.Logger, level slog.Level, readyFlag *atomic.Bool) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		logger.LogAttrs(context.Background(), level, line)
		if readyFlag != nil && !readyFlag.Load() {
			if isGeyserReady(line) {
				readyFlag.Store(true)
			}
		}
	}
}

// isGeyserReady detects Geyser's "Done (X.XXs)!" boot completion line.
func isGeyserReady(line string) bool {
	// Strip ANSI color codes to make the match resilient.
	for i := 0; i < len(line); i++ {
		if line[i] == '\x1b' {
			// crude ANSI strip — find the first 'm' after ESC[
			j := i + 1
			for j < len(line) && line[j] != 'm' {
				j++
			}
			line = line[:i] + line[min(j+1, len(line)):]
			i--
		}
	}
	// Look for the substring "Done (".
	for i := 0; i+6 <= len(line); i++ {
		if line[i:i+6] == "Done (" {
			return true
		}
	}
	return false
}

// writeFloodgateKey writes the Floodgate AES-128 key to <workdir>/key.bin
// when AuthType == Floodgate. The path is referenced from config.yml as
// floodgate-key-file: key.bin (relative to cwd).
func writeFloodgateKey(workdir string, opts Options) error {
	if opts.AuthType != Floodgate {
		return nil
	}
	if len(opts.FloodgateKey) != 16 {
		return ErrInvalidFloodgateKey
	}
	path := filepath.Join(workdir, "key.bin")
	if err := os.WriteFile(path, opts.FloodgateKey, 0o600); err != nil {
		return fmt.Errorf("geyserlite: write floodgate key: %w", err)
	}
	return nil
}

// copyPermissionsYML places a default permissions.yml in workdir.
// Geyser refuses to start without one. The committed default is empty.
func copyPermissionsYML(workdir string) error {
	const empty = `# Default permissions.yml — see GeyserMC docs.
default-permissions: []
`
	path := filepath.Join(workdir, "permissions.yml")
	return os.WriteFile(path, []byte(empty), 0o644)
}

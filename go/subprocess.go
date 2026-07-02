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
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

// subprocessRunner spawns the geyserlite native binary via os/exec, supervises it,
// restarts on crash with exponential backoff, and forwards signals.
type subprocessRunner struct {
	healthyFlag atomic.Bool
}

// stableRunThreshold is how long a subprocess must run before the
// supervisor considers it stable and resets the crash-restart backoff.
// Matches the Rust subprocess.rs threshold (5 minutes).
const stableRunThreshold = 5 * time.Minute


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
			startedAt := time.Now()
			err := r.runOnce(ctx, s, binary, workdir)
			if err == nil || errors.Is(err, context.Canceled) {
				return ctx.Err()
			}
			// If the subprocess stayed up long enough to look stable,
			// reset backoff so a later crash loop doesn't inherit the
			// max-backoff from a previous incident.
			if time.Since(startedAt) >= stableRunThreshold {
				backoff.reset()
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
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		// Signal the entire process group (not just the direct child) so
		// grandchildren Geyser spawned are terminated too. On Unix the
		// child is its own group leader (Setpgid), so -pid targets it.
		return signalProcess(cmd.Process.Pid)
	}
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
	return fmt.Errorf("%s: %w", formatExitError(err), err)
}

// formatExitError renders an exec.Cmd failure as a human-readable prefix.
// It distinguishes signal death (ExitCode -1) from ordinary nonzero exit
// codes, since the raw -1 reads as a nonsensical "exited -1". On Unix it
// also surfaces the terminating signal name when available.
func formatExitError(err error) string {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return "geyserlite: wait subprocess"
	}
	code := exitErr.ExitCode()
	if code >= 0 {
		return fmt.Sprintf("geyserlite: subprocess exited %d", code)
	}
	// code < 0 means the process was terminated by a signal (or otherwise
	// didn't produce an exit code). Surface the signal if we can.
	if sig := signalFromExitError(exitErr.ProcessState); sig != nil {
		return fmt.Sprintf("geyserlite: subprocess killed by signal %s", sig)
	}
	return "geyserlite: subprocess killed by signal"
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
	return strings.Contains(stripANSI(line), "Done (")
}

// ansiRE matches CSI escape sequences. A CSI sequence starts with ESC [
// (0x1b 0x5b) or the single-byte CSI (0x9b), followed by parameter bytes
// (0x30..0x3f), intermediate bytes (0x20..0x2f), and a single final byte
// in 0x40..0x7e (the command, e.g. 'm' for SGR color, 'K' for erase line,
// 'H' for cursor position). The previous hand-rolled scanner only handled
// 'm', which left sequences ending in K/J/H/etc. in readiness detection.
var ansiRE = regexp.MustCompile(`(?:\x9b|\x1b\[)[0-?]*[ -/]*[@-~]`)

// stripANSI removes ANSI CSI escape sequences from s. Truncated sequences
// (an ESC[ with no final byte) are left intact, matching the Rust parser.
func stripANSI(s string) string {
	return ansiRE.ReplaceAllString(s, "")
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

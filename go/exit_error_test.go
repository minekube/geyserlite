// SPDX-License-Identifier: MIT
package geyserlite

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestFormatExitError_OrdinaryExitCode(t *testing.T) {
	t.Parallel()
	// Run a command that exits with a known nonzero code so we get a
	// real *exec.ExitError with a concrete ProcessState.
	cmd := exec.Command("sh", "-c", "exit 42")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected nonzero exit, got nil")
	}
	got := formatExitError(err)
	if !strings.Contains(got, "42") {
		t.Errorf("formatExitError(%v) = %q, want it to mention code 42", err, got)
	}
	if strings.Contains(got, "signal") {
		t.Errorf("formatExitError(%v) = %q, should not mention signal for ordinary exit", err, got)
	}
}

func TestFormatExitError_NonExitError(t *testing.T) {
	t.Parallel()
	got := formatExitError(errors.New("something else"))
	if got != "geyserlite: wait subprocess" {
		t.Errorf("formatExitError for non-ExitError = %q, want generic prefix", got)
	}
}

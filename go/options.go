// SPDX-License-Identifier: MIT
package geyserlite

import (
	"fmt"
	"log/slog"
	"time"
)

// validate fills in defaults and checks invariants.
// Returns the validated copy; never mutates the input.
func (o Options) validate() (Options, error) {
	if o.Upstream == "" {
		return o, ErrUpstreamRequired
	}
	if o.AuthType == Floodgate && len(o.FloodgateKey) != 16 {
		return o, ErrInvalidFloodgateKey
	}
	if o.Listen == "" {
		o.Listen = ":19132"
	}
	// Validate endpoints strictly: reject malformed host:port strings,
	// non-numeric ports, and out-of-range ports. Empty Listen was
	// defaulted above; Upstream is required and always validated.
	if _, _, err := splitHostPort(o.Listen, "", 0); err != nil {
		return o, fmt.Errorf("geyserlite: invalid Listen %q: %w", o.Listen, err)
	}
	if _, _, err := splitHostPort(o.Upstream, "", 0); err != nil {
		return o, fmt.Errorf("geyserlite: invalid Upstream %q: %w", o.Upstream, err)
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.ShutdownTimeout == 0 {
		o.ShutdownTimeout = 30 * time.Second
	}
	if o.JVMArgs == nil {
		o.JVMArgs = DefaultJVMArgs()
	}
	if o.RestartPolicy == nil {
		o.RestartPolicy = &RestartPolicy{
			MinBackoff: time.Second,
			MaxBackoff: time.Minute,
		}
	}
	return o, nil
}

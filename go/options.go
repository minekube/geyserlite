// SPDX-License-Identifier: MIT
package geyserlite

import (
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

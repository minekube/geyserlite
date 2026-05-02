// SPDX-License-Identifier: MIT
package gate

import (
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"

	geyserlite "go.minekube.com/geyserlite"
)

// Config is the YAML-friendly bedrock config Gate parses out of its
// own configuration file and hands to [New]. Field names mirror Gate's
// snake_case YAML conventions; Go-side mapping uses the standard
// `yaml`/`json` struct tags so either decoder works.
type Config struct {
	// Enabled gates the whole subsystem. When false, [New] returns
	// (nil, nil) and Gate skips wiring up the bedrock listener.
	Enabled bool `yaml:"enabled" json:"enabled"`

	// Listen is the host:port the Bedrock UDP listener binds to.
	// Use "fly-global-services:19132" on Fly.io (see [geyserlite.FlyGlobalServices]).
	Listen string `yaml:"listen" json:"listen"`

	// Upstream is host:port of Gate's own Java listener — the embedded
	// Geyser forwards translated traffic into Gate as if it were any
	// other Java client.
	Upstream string `yaml:"upstream" json:"upstream"`

	// AuthType is one of "floodgate" (default), "online", "offline".
	AuthType string `yaml:"auth_type" json:"auth_type"`

	// FloodgateKey is the 16-byte AES-128 shared key, as hex. Required
	// when AuthType == "floodgate". Must match the Floodgate plugin
	// (or compatible) on the upstream Java side. The bytes — not a
	// PEM/RSA file — are what go on the wire; upstream Geyser docs
	// show an openssl RSA example that does NOT work for Floodgate.
	FloodgateKey string `yaml:"floodgate_key" json:"floodgate_key"`

	// MOTD is the two-line message Bedrock clients see in the server list.
	MOTD MOTDConfig `yaml:"motd" json:"motd"`

	// Mode selects "embedded" (default, in-process) or "subprocess"
	// (out-of-process, crash-isolated).
	Mode string `yaml:"mode" json:"mode"`

	// LibraryPath overrides auto-location of libgeyserlite.so. Empty
	// uses the standard locate strategy (path → env → embed → system →
	// auto-download).
	LibraryPath string `yaml:"library_path" json:"library_path"`

	// Mirror overrides the GitHub Release base URL for the
	// auto-download fallback. Empty uses the GitHub Releases default.
	Mirror string `yaml:"mirror" json:"mirror"`

	// Offline disables the auto-download path entirely. Useful for
	// air-gapped deployments that pre-place the .so via Docker layer.
	Offline bool `yaml:"offline" json:"offline"`
}

// MOTDConfig is the two-line server description shown to Bedrock clients.
type MOTDConfig struct {
	Line1 string `yaml:"line1" json:"line1"`
	Line2 string `yaml:"line2" json:"line2"`
}

// toOptions converts a [Config] into [geyserlite.Options]. The receiver
// is a value, not a pointer, so the caller can pass the parsed config
// from an immutable Gate config struct without aliasing.
func (c Config) toOptions(logger *slog.Logger) (geyserlite.Options, error) {
	opts := geyserlite.Options{
		Listen:      c.Listen,
		Upstream:    c.Upstream,
		LibraryPath: c.LibraryPath,
		Mirror:      c.Mirror,
		Offline:     c.Offline,
		Logger:      logger,
		MOTD: geyserlite.MOTD{
			Line1: c.MOTD.Line1,
			Line2: c.MOTD.Line2,
		},
	}

	switch strings.ToLower(c.AuthType) {
	case "", "floodgate":
		opts.AuthType = geyserlite.Floodgate
		key, err := decodeFloodgateKey(c.FloodgateKey)
		if err != nil {
			return geyserlite.Options{}, err
		}
		opts.FloodgateKey = key
	case "online":
		opts.AuthType = geyserlite.Online
	case "offline":
		opts.AuthType = geyserlite.Offline
	default:
		return geyserlite.Options{}, fmt.Errorf("gate/geyserlite: unknown auth_type %q (want floodgate|online|offline)", c.AuthType)
	}

	switch strings.ToLower(c.Mode) {
	case "", "embedded":
		opts.Mode = geyserlite.ModeEmbedded
	case "subprocess":
		opts.Mode = geyserlite.ModeSubprocess
	default:
		return geyserlite.Options{}, fmt.Errorf("gate/geyserlite: unknown mode %q (want embedded|subprocess)", c.Mode)
	}

	return opts, nil
}

// decodeFloodgateKey accepts hex (with or without 0x prefix). Empty input
// returns nil — validation that the key is *required* for floodgate auth
// is geyserlite.Options.validate's job, not this layer's.
func decodeFloodgateKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	s = strings.TrimPrefix(s, "0x")
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("gate/geyserlite: floodgate_key is not valid hex: %w", err)
	}
	return b, nil
}

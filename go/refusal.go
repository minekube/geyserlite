// SPDX-License-Identifier: MIT
package geyserlite

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
)

// versionRefusalMarker prefixes the one structured line the embedded Geyser
// build emits on its Bedrock version-refusal path (see
// build/patches/0009-bedrock-version-refusal-telemetry.patch). Neither Geyser's
// supported-version list nor its player-visible disconnect text is affected;
// the line exists so refusals can be counted instead of grepped for.
const versionRefusalMarker = "GEYSERLITE_REFUSAL"

// Refusal kinds reported by the native side. "newer_than_edge" is the case that
// matters for a fleet whose Geyser build lags an upstream client release: the
// player's client is ahead of the versions this build can translate.
const (
	VersionRefusalNewerThanEdge = "newer_than_edge"
	VersionRefusalOlderThanEdge = "older_than_edge"
)

// versionRefusalKeys is the exact key set the native line carries. A line with
// unknown, missing, or duplicated keys is not counted — it falls through to the
// ordinary log passthrough instead of being interpreted.
var versionRefusalKeys = [...]string{"refusal", "client_protocol", "max_supported_protocol", "bedrock_ingress"}

// VersionRefusal is one Bedrock client refused at the version handshake.
//
// It carries no player identity: Geyser refuses these clients before login, so
// there is no username, XUID, or account to attach, and BedrockIngress is the
// configured listener the refusal arrived on (server config), never a client
// address.
type VersionRefusal struct {
	// Kind is VersionRefusalNewerThanEdge or VersionRefusalOlderThanEdge.
	Kind string
	// ClientProtocol is the Bedrock protocol version the refused client claimed.
	ClientProtocol int
	// MaxSupportedProtocol is the newest protocol this GeyserLite build
	// translates, i.e. the ceiling the client was refused against.
	MaxSupportedProtocol int
	// BedrockIngress is Geyser's configured bedrock.address:port.
	BedrockIngress string
}

// parseVersionRefusal recognizes the native refusal line and returns its fields.
//
// The line arrives on the child process's stdout, so it may carry Geyser's
// "[HH:mm:ss LEVEL] " prefix and ANSI colour codes. Parsing is strict on
// purpose: only an exact known key set is recognized, so unrelated log text
// (which may legitimately contain player names or addresses elsewhere) can
// never be reinterpreted as a refusal.
func parseVersionRefusal(line string) (VersionRefusal, bool) {
	line = stripANSICodes(line)
	idx := strings.Index(line, versionRefusalMarker)
	if idx < 0 {
		return VersionRefusal{}, false
	}
	// We only accept the marker where a log message starts: at the beginning of
	// the line, or after Geyser's "[time LEVEL] " prefix.
	if prefix := strings.TrimSpace(line[:idx]); prefix != "" && !strings.HasSuffix(prefix, "]") {
		return VersionRefusal{}, false
	}

	fields := strings.Fields(line[idx+len(versionRefusalMarker):])
	if len(fields) != len(versionRefusalKeys) {
		return VersionRefusal{}, false
	}
	values := make(map[string]string, len(fields))
	for _, field := range fields {
		key, value, ok := strings.Cut(field, "=")
		if !ok || value == "" {
			return VersionRefusal{}, false
		}
		if _, dup := values[key]; dup {
			return VersionRefusal{}, false
		}
		values[key] = value
	}
	for _, key := range versionRefusalKeys {
		if _, ok := values[key]; !ok {
			return VersionRefusal{}, false
		}
	}
	if len(values) != len(versionRefusalKeys) {
		return VersionRefusal{}, false
	}

	switch values["refusal"] {
	case VersionRefusalNewerThanEdge, VersionRefusalOlderThanEdge:
	default:
		return VersionRefusal{}, false
	}
	clientProtocol, err := strconv.Atoi(values["client_protocol"])
	if err != nil || clientProtocol <= 0 {
		return VersionRefusal{}, false
	}
	maxSupported, err := strconv.Atoi(values["max_supported_protocol"])
	if err != nil || maxSupported <= 0 {
		return VersionRefusal{}, false
	}
	if !validIngressValue(values["bedrock_ingress"]) {
		return VersionRefusal{}, false
	}

	return VersionRefusal{
		Kind:                 values["refusal"],
		ClientProtocol:       clientProtocol,
		MaxSupportedProtocol: maxSupported,
		BedrockIngress:       values["bedrock_ingress"],
	}, true
}

// validIngressValue keeps the ingress label to what a host:port listener can
// contain, so a malformed or hostile line cannot smuggle arbitrary text (a
// player name, an address list) into the log record or the metric label.
func validIngressValue(value string) bool {
	if value == "" || len(value) > 128 || !strings.Contains(value, ":") {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.', r == '-', r == '_', r == ':', r == '[', r == ']', r == '*':
		default:
			return false
		}
	}
	return true
}

// observeVersionRefusal records one refusal: exactly one counter increment and
// exactly one structured log record, with no raw child line reproduced.
func observeVersionRefusal(refusal VersionRefusal, logger *slog.Logger) {
	bedrockVersionRefusals.WithLabelValues(
		refusal.Kind,
		strconv.Itoa(refusal.ClientProtocol),
		strconv.Itoa(refusal.MaxSupportedProtocol),
		refusal.BedrockIngress,
	).Inc()

	if logger == nil {
		return
	}
	logger.LogAttrs(context.Background(), slog.LevelWarn, "bedrock version refused",
		slog.String("event", "bedrock.version_refused"),
		slog.String("refusal", refusal.Kind),
		slog.Int("client_protocol", refusal.ClientProtocol),
		slog.Int("max_supported_protocol", refusal.MaxSupportedProtocol),
		slog.String("bedrock_ingress", refusal.BedrockIngress),
	)
}

// stripANSICodes removes ANSI colour sequences, matching the tolerance
// isGeyserReady already needs for Geyser's console output.
func stripANSICodes(line string) string {
	if !strings.ContainsRune(line, '\x1b') {
		return line
	}
	var b strings.Builder
	b.Grow(len(line))
	for i := 0; i < len(line); i++ {
		if line[i] != '\x1b' {
			b.WriteByte(line[i])
			continue
		}
		j := i + 1
		for j < len(line) && line[j] != 'm' {
			j++
		}
		i = j
	}
	return b.String()
}

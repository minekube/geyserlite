// SPDX-License-Identifier: MIT
package geyserlite

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// refusalLine is the shape the embedded Geyser build emits on its refusal path,
// including Geyser's own "[HH:mm:ss LEVEL] " prefix.
const refusalLine = `[20:36:05 INFO] GEYSERLITE_REFUSAL refusal=newer_than_edge ` +
	`client_protocol=9999 max_supported_protocol=827 bedrock_ingress=fly-global-services:19132`

// refusalCount reads the counter for one label set without needing a registry.
func refusalCount(t *testing.T, kind, clientProtocol, maxSupported string) float64 {
	t.Helper()
	return testutil.ToFloat64(bedrockVersionRefusals.WithLabelValues(kind, clientProtocol, maxSupported))
}

// captureLogger returns a JSON slog handler writing into buf.
func captureLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestVersionRefusalIncrementsCounterOnce(t *testing.T) {
	bedrockVersionRefusals.Reset()
	var buf bytes.Buffer
	pipeToLogger(strings.NewReader(refusalLine+"\n"), captureLogger(&buf), slog.LevelInfo, nil)

	if got := refusalCount(t, "newer_than_edge", "9999", "827"); got != 1 {
		t.Fatalf("refused client protocol counter = %v, want 1", got)
	}
	// A single refusal is one event: one counter increment, one log record.
	if n := strings.Count(strings.TrimSpace(buf.String()), "\n"); n != 0 {
		t.Fatalf("expected exactly one emitted log record, got %d newlines in %q", n, buf.String())
	}
}

func TestAcceptedHandshakeDoesNotIncrementCounter(t *testing.T) {
	bedrockVersionRefusals.Reset()
	var buf bytes.Buffer
	accepted := strings.Join([]string{
		"[20:36:04 INFO] Started Geyser on UDP port 19132",
		"[20:36:04 INFO] Done (2.382s)! Run /geyser help for help!",
		// A client that negotiated a supported version reaches the login path;
		// Geyser logs its own disconnect line, which must pass through untouched.
		`[20:36:05 INFO] Bedrock user with ip: <IP address withheld> has disconnected for reason Bedrock client timed out`,
	}, "\n")
	pipeToLogger(strings.NewReader(accepted+"\n"), captureLogger(&buf), slog.LevelInfo, nil)

	for _, kind := range []string{VersionRefusalNewerThanEdge, VersionRefusalOlderThanEdge} {
		for _, protocol := range []string{"9999", "0", "827"} {
			if got := refusalCount(t, kind, protocol, "827"); got != 0 {
				t.Fatalf("counter for kind=%s protocol=%s = %v, want 0 for accepted handshake", kind, protocol, got)
			}
		}
	}
	// Ordinary lines keep their byte-for-byte passthrough behavior.
	if !strings.Contains(buf.String(), "Started Geyser on UDP port 19132") ||
		!strings.Contains(buf.String(), "Bedrock client timed out") {
		t.Fatalf("accepted-handshake lines were not passed through: %q", buf.String())
	}
}

func TestVersionRefusalLineCarriesNoPlayerIdentity(t *testing.T) {
	bedrockVersionRefusals.Reset()
	var buf bytes.Buffer
	// The child's prefix can legitimately carry an address; the structured
	// record must not reproduce the raw line, so nothing from the prefix leaks.
	hostile := `[20:36:05 INFO] Bedrock user with ip: 203.0.113.7 (PlayerName42) ` + refusalLine
	pipeToLogger(strings.NewReader(hostile+"\n"), captureLogger(&buf), slog.LevelInfo, nil)

	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &record); err != nil {
		t.Fatalf("emitted line is not a single structured record: %v (%q)", err, buf.String())
	}

	want := map[string]bool{"time": true, "level": true, "msg": true,
		"event": true, "refusal": true, "client_protocol": true,
		"max_supported_protocol": true, "bedrock_ingress": true}
	for key := range record {
		if !want[key] {
			t.Fatalf("unexpected field %q in refusal record: %v", key, record)
		}
	}
	for _, key := range []string{"event", "refusal", "client_protocol", "max_supported_protocol", "bedrock_ingress"} {
		if _, ok := record[key]; !ok {
			t.Fatalf("missing field %q in refusal record: %v", key, record)
		}
	}

	emitted := buf.String()
	ipPattern := regexp.MustCompile(`\b\d{1,3}(\.\d{1,3}){3}\b`)
	if m := ipPattern.FindString(emitted); m != "" {
		t.Fatalf("refusal record contains an IP address (%q): %s", m, emitted)
	}
	for _, leak := range []string{"203.0.113.7", "PlayerName42", "Bedrock user with ip"} {
		if strings.Contains(emitted, leak) {
			t.Fatalf("refusal record reproduced %q from the raw child line: %s", leak, emitted)
		}
	}
	if strings.Contains(emitted, "@") {
		t.Fatalf("refusal record contains an '@' identity fragment: %s", emitted)
	}
}

func TestVersionRefusalMetricHasBoundedLabels(t *testing.T) {
	bedrockVersionRefusals.Reset()
	pipeToLogger(strings.NewReader(refusalLine+"\n"), captureLogger(&bytes.Buffer{}), slog.LevelInfo, nil)

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	var found bool
	for _, family := range families {
		if family.GetName() != "geyserlite_bedrock_version_refused_total" {
			continue
		}
		found = true
		if len(family.GetMetric()) != 1 {
			t.Fatalf("expected one series, got %d", len(family.GetMetric()))
		}
		labels := map[string]string{}
		for _, label := range family.GetMetric()[0].GetLabel() {
			labels[label.GetName()] = label.GetValue()
		}
		want := map[string]bool{"refusal": true, "client_protocol": true, "max_supported_protocol": true}
		for name := range labels {
			if !want[name] {
				// Connect's telemetry federation drops endpoint/address/identity
				// dimensions; the exporter must not publish them at all.
				t.Fatalf("unexpected metric label %q in %v", name, labels)
			}
		}
		for name := range want {
			if _, ok := labels[name]; !ok {
				t.Fatalf("missing metric label %q in %v", name, labels)
			}
		}
	}
	if !found {
		t.Fatal("geyserlite_bedrock_version_refused_total was not gathered from the default registry")
	}
}

func TestVersionRefusalRecordsOlderClientKind(t *testing.T) {
	bedrockVersionRefusals.Reset()
	line := `[09:00:00 INFO] GEYSERLITE_REFUSAL refusal=older_than_edge ` +
		`client_protocol=594 max_supported_protocol=827 bedrock_ingress=[::]:19132`
	pipeToLogger(strings.NewReader(line+"\n"), captureLogger(&bytes.Buffer{}), slog.LevelInfo, nil)

	if got := refusalCount(t, "older_than_edge", "594", "827"); got != 1 {
		t.Fatalf("older_than_edge counter = %v, want 1", got)
	}
}

func TestParseVersionRefusalRejectsUnrecognizedLines(t *testing.T) {
	cases := []struct {
		name string
		line string
	}{
		{"empty", ""},
		{"ordinary log", "[20:36:05 INFO] Started Geyser on UDP port 19132"},
		{"marker not at message start", "prefix GEYSERLITE_REFUSAL refusal=newer_than_edge client_protocol=9999 max_supported_protocol=827 bedrock_ingress=a:19132"},
		{"missing field", "GEYSERLITE_REFUSAL refusal=newer_than_edge client_protocol=9999 max_supported_protocol=827"},
		{"unknown field", "GEYSERLITE_REFUSAL refusal=newer_than_edge client_protocol=9999 max_supported_protocol=827 bedrock_ingress=a:19132 username=Steve"},
		{"unknown kind", "GEYSERLITE_REFUSAL refusal=somewhere client_protocol=9999 max_supported_protocol=827 bedrock_ingress=a:19132"},
		{"non-numeric protocol", "GEYSERLITE_REFUSAL refusal=newer_than_edge client_protocol=lots max_supported_protocol=827 bedrock_ingress=a:19132"},
		{"ingress with user text", "GEYSERLITE_REFUSAL refusal=newer_than_edge client_protocol=9999 max_supported_protocol=827 bedrock_ingress=Steve@example"},
		{"ingress without port", "GEYSERLITE_REFUSAL refusal=newer_than_edge client_protocol=9999 max_supported_protocol=827 bedrock_ingress=localhost"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if refusal, ok := parseVersionRefusal(c.line); ok {
				t.Fatalf("parseVersionRefusal(%q) accepted the line as %+v", c.line, refusal)
			}
		})
	}
}

func TestVersionRefusalLineWithANSIAndMultiplePrefixes(t *testing.T) {
	// Geyser colourises console output; the parser must still find the record.
	line := "\x1b[36;1m[20:36:05 INFO]\x1b[m GEYSERLITE_REFUSAL refusal=newer_than_edge " +
		"client_protocol=9999 max_supported_protocol=827 bedrock_ingress=fly-global-services:19132"
	refusal, ok := parseVersionRefusal(line)
	if !ok {
		t.Fatalf("parseVersionRefusal did not accept the ANSI-prefixed line %q", line)
	}
	if refusal.ClientProtocol != 9999 || refusal.MaxSupportedProtocol != 827 ||
		refusal.Kind != VersionRefusalNewerThanEdge || refusal.BedrockIngress != "fly-global-services:19132" {
		t.Fatalf("unexpected refusal %+v", refusal)
	}
}

func TestVersionRefusalDoesNotDisturbReadyDetection(t *testing.T) {
	bedrockVersionRefusals.Reset()
	var ready atomic.Bool
	var buf bytes.Buffer
	pipeToLogger(strings.NewReader("[20:36:04 INFO] Done (2.4s)!\n"+refusalLine+"\n"), captureLogger(&buf), slog.LevelInfo, &ready)
	if !ready.Load() {
		t.Fatal("readiness detection stopped working after the refusal line")
	}
}

// SPDX-License-Identifier: MIT
package geyserlite

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// bedrockVersionRefusals counts Bedrock clients refused at the version
// handshake, labelled by the refused client protocol and the ceiling it was
// refused against.
//
// It is registered on the default Prometheus registry because that is the shape
// this fleet already scrapes: the embedding process serves promhttp's default
// gatherer on its metrics address (moxy exposes it on its health address, which
// Fly's managed Prometheus federates), so a refusal rate is a query instead of a
// log grep. Labels stay bounded and identity-free on purpose: the Connect
// telemetry federation rejects endpoint/address dimensions, and the refusing
// client has no identity yet. The listener the refusal arrived on is in the log
// record, not in a time series.
//
// A native refusal is a per-connection event, so a counter is the only honest
// instrument — it counts refusal events and can never count distinct players.
var bedrockVersionRefusals = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "geyserlite_bedrock_version_refused_total",
	Help: "Total Bedrock client connections refused at the Bedrock version handshake, " +
		"by refusal kind, refused client protocol, and the newest protocol the running " +
		"GeyserLite build translates.",
}, []string{"refusal", "client_protocol", "max_supported_protocol"})

// SPDX-License-Identifier: MIT
package geyserlite

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Guards over the operator-facing claims in ROADMAP.md and
// docs/troubleshooting.md.
//
// WHAT THESE TESTS PROVE
//
//   - ROADMAP.md is no longer frozen in the v0.5 era: it carries a phase that
//     names the releases which changed operator expectations (keepalive gating,
//     the Geyser 2.x HAProxy path, NetherNet ingress + Bedrock 26.60, the
//     converted-placement hold, transport recovery), each by the version that
//     shipped it.
//   - The roadmap's latest version line matches CHANGELOG.md's newest release,
//     so a future minor series cannot ship while the roadmap still describes
//     the previous one. That is exactly the drift this guard was written for:
//     the roadmap was stamped 2026-05-02 while releases had reached v0.7.6.
//   - troubleshooting.md names the three session symptoms that reached support
//     as "geyserlite is broken" reports — the floating-check kick, the
//     "generating world" hang after a terminated executor, and the keepalive
//     `Timed out` — with the version that fixed each one, so a reader on an
//     older build gets a fix instead of a bisect.
//   - README.md still states the NetherNet runtime requirement (OpenSSL 3,
//     libgcc, the bundled JNI library), because a static-musl build failing to
//     load it is the most common "NetherNet does not work" report.
//
// WHAT THEY DO NOT PROVE
//
//   - That the fixes work. Each behaviour is covered by its own overlay/patch
//     tests and by the release acceptance runs named in the changelog entries.
//   - That the versions quoted are downloadable. The release workflow's own
//     asset verification covers that; a test in this package cannot reach the
//     network by design.

const (
	roadmapDoc      = "ROADMAP.md"
	changelogDoc    = "CHANGELOG.md"
	troubleshooting = "docs/troubleshooting.md"
	readmeDoc       = "README.md"
	docsRepoRootRel = ".."
)

func readDocFile(t *testing.T, rel string) string {
	t.Helper()
	path := filepath.Join(docsRepoRootRel, rel)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	// Windows runners check the docs out with CRLF line endings. Normalising here
	// keeps every assertion below about content, not about how git checked the
	// files out (a multi-line needle otherwise fails only on windows-latest).
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

func requireContains(t *testing.T, file, content string, want []string) {
	t.Helper()
	for _, needle := range want {
		if !strings.Contains(content, needle) {
			t.Fatalf("%s no longer documents %q", file, needle)
		}
	}
}

// newestChangelogRelease returns the version and date of the top entry, e.g.
// ("0.7.6", "2026-11-03").
func newestChangelogRelease(t *testing.T, changelog string) (string, string) {
	t.Helper()
	m := regexp.MustCompile(`(?m)^## \[(\d+\.\d+\.\d+)\][^\n]*\((\d{4}-\d{2}-\d{2})\)`).FindStringSubmatch(changelog)
	if m == nil {
		t.Fatal("CHANGELOG.md has no `## [x.y.z](...) (date)` release heading")
	}
	return m[1], m[2]
}

func TestRoadmapNamesTheShippedBedrockPhases(t *testing.T) {
	roadmap := readDocFile(t, roadmapDoc)
	requireContains(t, roadmapDoc, roadmap, []string{
		"### v0.6.x – v0.7.x — Bedrock currency, placement and transport correctness",
		"(v0.6.1)",
		"(v0.6.4)",
		"(v0.7.0, moved to upstream stable 26.60 in v0.7.1)",
		"(v0.7.2 → v0.7.4)",
		"(v0.7.6)",
		// The Gate adapter phase used to promise a Gate v0.65.0 cut that never
		// happened; the roadmap now states what Gate actually consumes.
		"managed subprocess dependency",
		"go.minekube.com/geyserlite v0.7.6",
	})
}

func TestRoadmapTracksTheNewestReleaseLine(t *testing.T) {
	roadmap := readDocFile(t, roadmapDoc)
	version, _ := newestChangelogRelease(t, readDocFile(t, changelogDoc))
	minor := version[:strings.LastIndex(version, ".")]
	if !strings.Contains(roadmap, "v"+minor+".") {
		t.Fatalf("%s does not mention the current release line v%s.* (newest CHANGELOG entry is v%s); the roadmap has drifted from the shipped releases", roadmapDoc, minor, version)
	}
}

func TestTroubleshootingNamesTheSessionSymptoms(t *testing.T) {
	doc := readDocFile(t, troubleshooting)
	requireContains(t, troubleshooting, doc, []string{
		// The floating-check kick, its mechanism, and the version range that fixed it.
		"`Flying is not enabled on this server` moments after a Bedrock player joins",
		"clientIsFloating",
		"81 ticks",
		"Fixed across v0.7.2 – v0.7.4",
		"no movement sample is forwarded at all",
		// The terminated-executor hang and the recovery.
		`A player hangs on "generating world" and the log claims a login that never happened`,
		"RejectedExecutionException",
		"v0.7.6 replaces a terminated thread group",
		// The keepalive starvation and the setting that caused it.
		"`Timed out` about 30 s after spawn while the session looks healthy",
		"gameplay.forward-player-ping",
		"Before v0.6.1",
	})
}

func TestReadmeKeepsTheNetherNetRuntimeRequirement(t *testing.T) {
	readme := readDocFile(t, readmeDoc)
	requireContains(t, readmeDoc, readme, []string{
		"NetherNet also needs the",
		"libdatachannel",
		"musl executables cannot load it",
	})
}

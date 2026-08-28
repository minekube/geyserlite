// SPDX-License-Identifier: MIT
package geyserlite

import (
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The fresh release path must not go through softprops/action-gh-release:
// its files glob uploads duplicate basenames — libgeyserlite.h (and its
// sigstore bundle) exists under every upload-artifact arch dir — and the
// second same-named upload fails with "Not Found - update-a-release-asset".
// Every geyserlite release from v0.4.11 (2026-07-27) to v0.5.22 died
// exactly there: assets half-uploaded, verification and crates publish
// skipped, and no Gate bump dispatched. The repair path already proves
// that `gh release upload --clobber` only POSTs to /releases/{id}/assets
// and works against the same releases.
//
// These tests pin the whole publish path (fresh AND repair) onto one
// idempotent asset-endpoint step and preserve the two guards that keep a
// re-run from rewriting a release consumers have already checksummed.

const publishStepName = "Publish GitHub Release assets"

type publishWorkflow struct {
	Jobs map[string]struct {
		Steps []publishWorkflowStep `yaml:"steps"`
	} `yaml:"jobs"`
}

type publishWorkflowStep struct {
	Name string `yaml:"name"`
	Uses string `yaml:"uses"`
	Run  string `yaml:"run"`
}

func releaseJobSteps(t *testing.T) []publishWorkflowStep {
	t.Helper()

	const workflowPath = "../.github/workflows/release.yml"

	workflowBytes, err := os.ReadFile(workflowPath)
	if errors.Is(err, os.ErrNotExist) {
		if _, checkoutErr := os.Stat("../.git"); errors.Is(checkoutErr, os.ErrNotExist) {
			t.Skipf("%s is unavailable outside a repository checkout", workflowPath)
		}
	}
	if err != nil {
		t.Fatal(err)
	}

	var workflow publishWorkflow
	if err := yaml.Unmarshal(workflowBytes, &workflow); err != nil {
		t.Fatal(err)
	}

	return workflow.Jobs["release"].Steps
}

func publishStep(t *testing.T) publishWorkflowStep {
	t.Helper()

	for _, step := range releaseJobSteps(t) {
		if step.Name == publishStepName {
			return step
		}
	}

	t.Fatalf("release job is missing the %q step", publishStepName)
	return publishWorkflowStep{}
}

// TestPublishPathNeverUsesSoftprops is the fix itself.
func TestPublishPathNeverUsesSoftprops(t *testing.T) {
	for _, step := range releaseJobSteps(t) {
		if strings.Contains(step.Uses, "action-gh-release") {
			t.Errorf("release job step %q uses action-gh-release, whose files glob "+
				"cannot upload duplicate basenames and dies on the second same-named asset",
				step.Name)
		}
	}
}

// TestPublishPathUsesAssetEndpoint pins the upload mechanism: the single
// publish step must upload through the asset endpoint only.
func TestPublishPathUsesAssetEndpoint(t *testing.T) {
	step := publishStep(t)

	if step.Run == "" {
		t.Fatalf("%q must be a run step", publishStepName)
	}
	if !strings.Contains(step.Run, "gh release upload") {
		t.Errorf("%q does not use `gh release upload`; only the asset endpoint "+
			"avoids the 403-ing release-metadata PATCH", publishStepName)
	}
	if !strings.Contains(step.Run, "--clobber") {
		t.Errorf("%q does not pass --clobber; a re-run would fail on existing assets",
			publishStepName)
	}
	if strings.Contains(step.Uses, "action-gh-release") {
		t.Errorf("%q uses action-gh-release, which PATCHes release metadata "+
			"before uploading and is exactly what fails", publishStepName)
	}
}

// TestPublishPathCreatesMissingRelease pins idempotency across the fresh
// and repair paths: a missing release is created, an existing one is only
// filled.
func TestPublishPathCreatesMissingRelease(t *testing.T) {
	script := publishStep(t).Run

	if !strings.Contains(script, "gh release view") {
		t.Error("publish step does not check for an existing release before deciding to create one")
	}
	if !strings.Contains(script, "gh release create") {
		t.Error("publish step does not create a missing release with `gh release create`")
	}
}

// TestPublishPathFlattensByBasename pins the root cause: upload-artifact
// nests files per architecture, and libgeyserlite.h exists under every
// arch dir. Uploading the raw tree would duplicate asset names; the
// publish step must flatten by basename first.
func TestPublishPathFlattensByBasename(t *testing.T) {
	script := publishStep(t).Run

	if !strings.Contains(script, `cp -f "$1" "upload/$(basename "$1")"`) {
		t.Error("publish step does not flatten per-arch artifacts by basename; " +
			"duplicate asset names will fail the upload")
	}
}

// TestPublishPathOnlyTouchesIncompleteReleases pins guard 1. A release
// that already carries a real build, a non-empty checksums.txt, AND every
// asset the manifest promises is complete; re-uploading over it would
// replace artifacts consumers have already checksummed.
func TestPublishPathOnlyTouchesIncompleteReleases(t *testing.T) {
	script := publishStep(t).Run

	if !strings.Contains(script, "/releases/tags/") {
		t.Error("publish step does not read the published release before deciding to modify it")
	}
	if !regexp.MustCompile(`EXISTING_BUILDS"?\s*-gt\s*0`).MatchString(script) {
		t.Error("publish step does not check whether the release already has a real build artifact")
	}
	if !regexp.MustCompile(`HAS_CHECKSUMS"?\s*-gt\s*0`).MatchString(script) {
		t.Error("publish step does not check whether the release already has checksums.txt")
	}
	if !strings.Contains(script, `.name == "checksums.txt" and .state == "uploaded" and .size > 0`) {
		t.Error("publish step treats a zero-byte checksums.txt as a complete manifest")
	}
	// v0.5.22 looked complete by build+checksum count while checksums.txt
	// still referenced an asset that had never uploaded. Completeness must
	// also require every manifest entry to be present.
	if !strings.Contains(script, "while read -r _ name") {
		t.Error("publish step does not enumerate checksums.txt entries to detect missing assets")
	}
	if !strings.Contains(script, "MISSING") {
		t.Error("publish step does not detect checksummed assets that are absent from the release")
	}
}

// TestPublishPathNeverClobbersGoodAssets pins guard 2. --clobber is only
// acceptable against a hole: an asset that is absent, or present in a
// non-uploaded / zero-byte state from a failed upload.
func TestPublishPathNeverClobbersGoodAssets(t *testing.T) {
	script := publishStep(t).Run

	if !strings.Contains(script, `.state == "uploaded" and .size > 0`) {
		t.Error("publish step does not classify an existing asset as good by upload state and size; " +
			"--clobber could overwrite a healthy artifact")
	}
	if !strings.Contains(script, "sha256sum") {
		t.Error("publish step does not reconcile existing good assets against the local build")
	}
	if !strings.Contains(script, "SKIPPED") || !strings.Contains(script, "not clobbered") {
		t.Error("publish step does not skip and report existing good assets")
	}
}

// TestPublishStepRunsBeforeVerify pins the fail-closed ordering: the
// verify step re-reads the PUBLISHED release and must run after the
// upload, and before the crates publish and the Gate dispatch.
func TestPublishStepRunsBeforeVerify(t *testing.T) {
	steps := releaseJobSteps(t)
	publishIdx, verifyIdx := -1, -1
	for i, step := range steps {
		switch step.Name {
		case publishStepName:
			publishIdx = i
		case "Verify published release assets":
			verifyIdx = i
		}
	}
	if publishIdx < 0 {
		t.Fatalf("release job is missing the %q step", publishStepName)
	}
	if verifyIdx < 0 {
		t.Fatalf("release job is missing the %q step", "Verify published release assets")
	}
	if publishIdx > verifyIdx {
		t.Fatalf("publish step (index %d) must run before verify (index %d)",
			publishIdx, verifyIdx)
	}
}

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

// Repairing an existing release cannot go through
// softprops/action-gh-release: it PATCHes /releases/{id} to sync release
// metadata before uploading anything, and that PATCH returns
// "403 Resource not accessible by integration" for these releases, so the
// upload is never attempted. Four backfill runs for v0.3.6-v0.3.9 died
// exactly there with every artifact already built and signed.
//
// These tests pin the repair path onto the asset endpoint and pin the two
// guards that keep a backfill from turning into a rewrite.
//
// The YAML scaffolding below is deliberately self-contained so this change
// stays reviewable as its own pull request; it can be folded together with
// the asset-verification tests once both have landed.

const repairStepName = "Repair release assets"

type repairWorkflow struct {
	Jobs map[string]struct {
		Steps []repairWorkflowStep `yaml:"steps"`
	} `yaml:"jobs"`
}

type repairWorkflowStep struct {
	Name string `yaml:"name"`
	Uses string `yaml:"uses"`
	If   string `yaml:"if"`
	Run  string `yaml:"run"`
}

func repairStep(t *testing.T) repairWorkflowStep {
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

	var workflow repairWorkflow
	if err := yaml.Unmarshal(workflowBytes, &workflow); err != nil {
		t.Fatal(err)
	}

	for _, step := range workflow.Jobs["release"].Steps {
		if step.Name == repairStepName {
			return step
		}
	}

	t.Fatalf("release job is missing the %q step; a repair would fall back to "+
		"the metadata PATCH that 403s", repairStepName)
	return repairWorkflowStep{}
}

// TestRepairPathUsesAssetEndpoint is the fix itself.
func TestRepairPathUsesAssetEndpoint(t *testing.T) {
	step := repairStep(t)

	if step.Run == "" {
		t.Fatalf("%q must be a run step", repairStepName)
	}
	if !strings.Contains(step.Run, "gh release upload") {
		t.Errorf("%q does not use `gh release upload`; only the asset endpoint "+
			"avoids the 403-ing release-metadata PATCH", repairStepName)
	}
	if strings.Contains(step.Uses, "action-gh-release") {
		t.Errorf("%q uses action-gh-release, which PATCHes release metadata "+
			"before uploading and is exactly what fails", repairStepName)
	}

	// It must be reached only for an explicit repair dispatch, so a normal
	// release keeps its existing create-and-upload behaviour.
	if !strings.Contains(step.If, "inputs.release_tag") {
		t.Errorf("%q is not gated on inputs.release_tag (if: %q)", repairStepName, step.If)
	}
}

// TestRepairPathOnlyTouchesIncompleteReleases pins guard 1. Repair fills
// holes; a release that already carries a real build and its manifest is
// complete, and re-uploading over it would replace artifacts consumers have
// already checksummed.
func TestRepairPathOnlyTouchesIncompleteReleases(t *testing.T) {
	script := repairStep(t).Run

	if !strings.Contains(script, "/releases/tags/") {
		t.Error("repair does not read the published release before deciding to modify it")
	}
	if !regexp.MustCompile(`EXISTING_BUILDS"?\s*-gt\s*0`).MatchString(script) {
		t.Error("repair does not check whether the release already has a real build artifact")
	}
	if !regexp.MustCompile(`HAS_CHECKSUMS"?\s*-gt\s*0`).MatchString(script) {
		t.Error("repair does not check whether the release already has checksums.txt")
	}
	if !strings.Contains(script, "Refusing to modify a complete release") {
		t.Error("repair does not refuse a release that is already complete")
	}
}

// TestRepairPathNeverClobbersGoodAssets pins guard 2. --clobber is only
// acceptable against a hole: an asset that is absent, or present in a
// non-uploaded / zero-byte state from a failed upload.
func TestRepairPathNeverClobbersGoodAssets(t *testing.T) {
	script := repairStep(t).Run

	if !strings.Contains(script, "--clobber") {
		t.Fatalf("%q no longer passes --clobber", repairStepName)
	}

	// The eligibility test must require BOTH a completed upload and a
	// non-empty asset before treating an existing name as good.
	if !strings.Contains(script, `.state == "uploaded" and .size > 0`) {
		t.Error("repair does not classify an existing asset as good by upload state and size; " +
			"--clobber could overwrite a healthy artifact")
	}
	if !strings.Contains(script, "SKIPPED") || !strings.Contains(script, "not clobbered") {
		t.Error("repair does not skip and report existing good assets")
	}

	// And it must upload a filtered list, never the whole artifact set.
	if regexp.MustCompile(`gh release upload "\$RELEASE_TAG" upload/\*`).MatchString(script) {
		t.Error("repair uploads every built file with --clobber, overwriting good assets")
	}
	if !regexp.MustCompile(`gh release upload "\$RELEASE_TAG" \$UPLOAD_LIST`).MatchString(script) {
		t.Error("repair does not upload the filtered missing-asset list")
	}
}

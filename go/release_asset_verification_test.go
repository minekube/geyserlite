// SPDX-License-Identifier: MIT
package geyserlite

import (
	"errors"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The release workflow used to end at "upload the artifacts and hope".
// v0.3.3, v0.3.6-v0.3.9 and v0.3.20-v0.4.8 all published with zero assets
// while their runs looked normal, because nothing ever re-read the release
// that actually landed. These tests pin the guard that closes that gap.

const (
	verifyAssetsStepName  = "Verify published release assets"
	createReleaseStepName = "Create GitHub Release"
)

type assetWorkflow struct {
	Jobs map[string]assetWorkflowJob `yaml:"jobs"`
}

type assetWorkflowJob struct {
	Steps []assetWorkflowStep `yaml:"steps"`
}

type assetWorkflowStep struct {
	Name string `yaml:"name"`
	Uses string `yaml:"uses"`
	If   string `yaml:"if"`
	Run  string `yaml:"run"`
}

func readReleaseJobSteps(t *testing.T) []assetWorkflowStep {
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

	var workflow assetWorkflow
	if err := yaml.Unmarshal(workflowBytes, &workflow); err != nil {
		t.Fatal(err)
	}

	release, ok := workflow.Jobs["release"]
	if !ok {
		t.Fatal("release job is missing")
	}
	if len(release.Steps) == 0 {
		t.Fatal("release job has no steps")
	}

	return release.Steps
}

func stepIndex(steps []assetWorkflowStep, name string) int {
	for i, step := range steps {
		if step.Name == name {
			return i
		}
	}
	return -1
}

// TestReleaseVerifiesPublishedAssets is the core regression guard: the
// release job must fail when the published release carries no downloadable
// artifact.
func TestReleaseVerifiesPublishedAssets(t *testing.T) {
	steps := readReleaseJobSteps(t)

	verifyAt := stepIndex(steps, verifyAssetsStepName)
	if verifyAt < 0 {
		t.Fatalf("release job is missing the %q step; a release that publishes "+
			"no asset would ship green again", verifyAssetsStepName)
	}

	script := steps[verifyAt].Run
	if script == "" {
		t.Fatalf("%q must be a run step that asserts on the published release", verifyAssetsStepName)
	}
	if steps[verifyAt].If != "" {
		t.Errorf("%q is conditional (if: %q); the guard must always run",
			verifyAssetsStepName, steps[verifyAt].If)
	}
}

// TestReleaseVerificationReadsPublishedRelease is the point of the guard.
// Asserting against the upload step's own output would rebuild the exact
// "trust the run, not the artifact" defect one layer up, so the check has to
// go back to the GitHub API and read what actually landed.
func TestReleaseVerificationReadsPublishedRelease(t *testing.T) {
	steps := readReleaseJobSteps(t)

	verifyAt := stepIndex(steps, verifyAssetsStepName)
	if verifyAt < 0 {
		t.Fatalf("release job is missing the %q step", verifyAssetsStepName)
	}
	script := steps[verifyAt].Run

	for _, want := range []string{
		"/releases/tags/",    // re-reads the published release by tag
		"checksums.txt",      // the manifest every auto-download path needs
		`"uploaded"`,         // only fully-uploaded assets count
		"releases/download/", // proves a build is actually served, not just listed
	} {
		if !strings.Contains(script, want) {
			t.Errorf("%q script does not reference %q; it must assert on the "+
				"published release, not on local build output", verifyAssetsStepName, want)
		}
	}

	// The guard must not be satisfied by inspecting the local artifacts/
	// directory: those files exist even when the upload never happened.
	localOnly := !strings.Contains(script, "gh api")
	if localOnly {
		t.Error("verification must call the GitHub API to read the published release")
	}
}

// TestReleaseVerificationRequiresRealBuildArtifact pins the second failure
// condition. A non-empty asset list is not proof of a usable release: a
// release carrying only checksums.txt, signature bundles, SBOMs or the
// libgeyserlite.h C header has a positive asset count and still offers
// nothing anyone can run. v0.3.6 was briefly in exactly that state - one
// asset, no build - so the guard must classify by name/type rather than
// count.
func TestReleaseVerificationRequiresRealBuildArtifact(t *testing.T) {
	steps := readReleaseJobSteps(t)

	verifyAt := stepIndex(steps, verifyAssetsStepName)
	if verifyAt < 0 {
		t.Fatalf("release job is missing the %q step", verifyAssetsStepName)
	}
	script := steps[verifyAt].Run

	// The metadata types that must NOT satisfy the guard on their own.
	for _, excluded := range []string{
		`\\.sigstore\\.json$`,      // signature bundles
		`\\.attest\\.spdx\\.json$`, // SBOM attestations
		`\\.h$`,                    // the C header that faked a repair
		`^checksums\\.txt$`,        // the manifest itself
	} {
		if !strings.Contains(script, excluded) {
			t.Errorf("build-artifact classifier does not exclude %s; a release of pure "+
				"metadata would pass the guard", excluded)
		}
	}

	// And it must actually gate on the classified count, not just compute it.
	for _, want := range []string{"BUILD_COUNT", "-eq 0"} {
		if !strings.Contains(script, want) {
			t.Errorf("guard does not fail on a zero build-artifact count (missing %q)", want)
		}
	}
}

// TestReleaseVerificationRunsAfterUpload pins the ordering. The guard is
// meaningless before the upload, and crates.io is irreversible: a version
// whose binaries are missing must not reach the registry, and the Gate bump
// cascade must not advertise a release nobody can download.
func TestReleaseVerificationRunsAfterUpload(t *testing.T) {
	steps := readReleaseJobSteps(t)

	verifyAt := stepIndex(steps, verifyAssetsStepName)
	createAt := stepIndex(steps, createReleaseStepName)
	if verifyAt < 0 || createAt < 0 {
		t.Fatalf("expected both %q (%d) and %q (%d) in the release job",
			createReleaseStepName, createAt, verifyAssetsStepName, verifyAt)
	}
	if verifyAt < createAt {
		t.Errorf("%q runs before %q; it would always see an empty release",
			verifyAssetsStepName, createReleaseStepName)
	}

	for i, step := range steps {
		if !strings.Contains(step.Uses, "crates-io-auth-action") &&
			!strings.Contains(step.Run, "cargo publish") {
			continue
		}
		if i < verifyAt {
			t.Errorf("crates.io step %q runs before %q; an irreversible publish "+
				"must not precede the asset guard", step.Name, verifyAssetsStepName)
		}
	}
}

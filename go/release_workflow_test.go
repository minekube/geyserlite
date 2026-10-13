// SPDX-License-Identifier: MIT
package geyserlite

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const gateDispatchWorkflowPath = "minekube/actions/.github/workflows/dispatch-workflow.yml"

const gateDispatchWorkflowMajorTag = "v1"

var immutableWorkflowRef = regexp.MustCompile(`^[0-9a-f]{40}$`)

type releaseWorkflow struct {
	Jobs map[string]releaseWorkflowJob `yaml:"jobs"`
}

type releaseWorkflowJob struct {
	Needs       string                `yaml:"needs"`
	If          string                `yaml:"if"`
	RunsOn      string                `yaml:"runs-on"`
	Uses        string                `yaml:"uses"`
	Permissions map[string]string     `yaml:"permissions"`
	With        map[string]string     `yaml:"with"`
	Secrets     string                `yaml:"secrets"`
	Steps       []releaseWorkflowStep `yaml:"steps"`
	Concurrency struct {
		Group            string `yaml:"group"`
		Queue            string `yaml:"queue"`
		CancelInProgress bool   `yaml:"cancel-in-progress"`
	} `yaml:"concurrency"`
}

type releaseWorkflowStep struct {
	Name             string            `yaml:"name"`
	Uses             string            `yaml:"uses"`
	Run              string            `yaml:"run"`
	WorkingDirectory string            `yaml:"working-directory"`
	With             map[string]string `yaml:"with"`
	Env              map[string]string `yaml:"env"`
}

func TestReleaseGateDispatchWorkflowContract(t *testing.T) {
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

	var workflow releaseWorkflow
	if err := yaml.Unmarshal(workflowBytes, &workflow); err != nil {
		t.Fatal(err)
	}

	dispatch, ok := workflow.Jobs["dispatch-gate-bump"]
	if !ok {
		t.Fatal("dispatch-gate-bump job is missing")
	}
	if err := validateGateDispatchWorkflowContract(dispatch); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseMoxyDispatchWorkflowContract(t *testing.T) {
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

	var workflow releaseWorkflow
	if err := yaml.Unmarshal(workflowBytes, &workflow); err != nil {
		t.Fatal(err)
	}

	dispatch, ok := workflow.Jobs["dispatch-moxy-geyserlite-bump"]
	if !ok {
		t.Fatal("dispatch-moxy-geyserlite-bump job is missing")
	}
	if err := validateMoxyDispatchWorkflowContract(dispatch); err != nil {
		t.Fatal(err)
	}
}

func validMoxyDispatchWorkflowJob() releaseWorkflowJob {
	job := releaseWorkflowJob{
		Needs:       "release",
		If:          "startsWith(inputs.release_tag || github.ref_name, 'v')",
		RunsOn:      "ubuntu-24.04",
		Permissions: map[string]string{"contents": "read"},
		Steps: []releaseWorkflowStep{
			{
				Name: "Create Moxy release-cascade token",
				Uses: "actions/create-github-app-token@bcd2ba49218906704ab6c1aa796996da409d3eb1",
				With: map[string]string{
					"client-id": "${{ vars.RELEASE_CASCADE_APP_CLIENT_ID }}", "private-key": "${{ secrets.RELEASE_CASCADE_APP_PRIVATE_KEY }}",
					"owner": "minekube", "repositories": "moxy", "permission-contents": "write", "permission-pull-requests": "write",
				},
			},
			{
				Name: "Checkout Moxy main",
				Uses: "actions/checkout@v6",
				With: map[string]string{
					"repository": "minekube/moxy", "ref": "main", "path": "moxy", "token": "${{ steps.moxy-token.outputs.token }}",
				},
			},
			{
				Name: "Install Go",
				Uses: "actions/setup-go@40f1582b2485089dde7abd97c1529aa768e1baff",
				With: map[string]string{"go-version-file": "moxy/go.mod", "cache-dependency-path": "moxy/go.sum", "cache": "true"},
			},
			{
				Name: "Install Bun",
				Uses: "oven-sh/setup-bun@0c5077e51419868618aeaa5fe8019c62421857d6",
				With: map[string]string{"bun-version": "1.3.7"},
			},
			{
				Name: "Install Cosign",
				Uses: "sigstore/cosign-installer@6f9f17788090df1f26f669e9d70d6ae9567deba6",
				With: map[string]string{"cosign-release": "v3.1.3"},
			},
			{
				Name:             "Install Moxy dependencies",
				WorkingDirectory: "moxy",
				Run:              "bun install --frozen-lockfile",
			},
			{
				Name:             "Update Moxy and open dependency PR",
				WorkingDirectory: "moxy",
				Run: `bun scripts/update-geyserlite-release.ts \
  --version "$GEYSERLITE_VERSION" \
  --source-release-url "$GEYSERLITE_RELEASE_URL" \
  --publish`,
				Env: map[string]string{
					"GH_TOKEN": "${{ steps.moxy-token.outputs.token }}", "GH_APP_SLUG": "${{ steps.moxy-token.outputs.app-slug }}",
					"GEYSERLITE_VERSION":     "${{ inputs.release_tag || github.ref_name }}",
					"GEYSERLITE_RELEASE_URL": "${{ github.server_url }}/${{ github.repository }}/releases/tag/${{ inputs.release_tag || github.ref_name }}",
				},
			},
		},
	}
	job.Concurrency.Group = "moxy-geyserlite-release-update"
	job.Concurrency.Queue = "max"
	return job
}

func validateMoxyDispatchWorkflowContract(dispatch releaseWorkflowJob) error {
	if dispatch.Concurrency.Group != "moxy-geyserlite-release-update" || dispatch.Concurrency.Queue != "max" || dispatch.Concurrency.CancelInProgress {
		return fmt.Errorf("Moxy updates must serialize without canceling a publisher")
	}
	if dispatch.Needs != "release" || dispatch.If != "startsWith(inputs.release_tag || github.ref_name, 'v')" || dispatch.RunsOn != "ubuntu-24.04" {
		return fmt.Errorf("Moxy dispatch = needs %q, if %q, runs-on %q", dispatch.Needs, dispatch.If, dispatch.RunsOn)
	}
	if !reflect.DeepEqual(dispatch.Permissions, map[string]string{"contents": "read"}) {
		return fmt.Errorf("Moxy dispatch permissions = %#v", dispatch.Permissions)
	}
	actualSteps := append([]releaseWorkflowStep(nil), dispatch.Steps...)
	expectedSteps := validMoxyDispatchWorkflowJob().Steps
	if len(actualSteps) > 0 {
		actualSteps[len(actualSteps)-1].Run = strings.TrimSpace(actualSteps[len(actualSteps)-1].Run)
		expectedSteps[len(expectedSteps)-1].Run = strings.TrimSpace(expectedSteps[len(expectedSteps)-1].Run)
	}
	if !reflect.DeepEqual(actualSteps, expectedSteps) {
		return fmt.Errorf("Moxy dispatch steps = %#v", dispatch.Steps)
	}
	return nil
}

func TestReleaseMoxyDispatchWorkflowContractRejectsMutations(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*releaseWorkflowJob)
	}{
		{"early dispatch", func(job *releaseWorkflowJob) { job.Needs = "native-image" }},
		{"parallel publishers", func(job *releaseWorkflowJob) { job.Concurrency.Group = "" }},
		{"replaced pending publisher", func(job *releaseWorkflowJob) { job.Concurrency.Queue = "single" }},
		{"interrupted publisher", func(job *releaseWorkflowJob) { job.Concurrency.CancelInProgress = true }},
		{"wrong target repository", func(job *releaseWorkflowJob) { job.Steps[0].With["repositories"] = "gate" }},
		{"missing Cosign", func(job *releaseWorkflowJob) { job.Steps[4].Uses = "" }},
		{"wrong Moxy checkout", func(job *releaseWorkflowJob) { job.Steps[1].With["repository"] = "minekube/gate" }},
		{"missing source URL", func(job *releaseWorkflowJob) { job.Steps[6].Env["GEYSERLITE_RELEASE_URL"] = "" }},
		{"missing signed-update PR step", func(job *releaseWorkflowJob) {
			job.Steps[6].Run = strings.ReplaceAll(job.Steps[6].Run, "--publish", "")
		}},
		{"excess permission", func(job *releaseWorkflowJob) { job.Permissions["contents"] = "write" }},
		{"missing dependency installation", func(job *releaseWorkflowJob) { job.Steps[5].Run = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := validMoxyDispatchWorkflowJob()
			tt.mutate(&job)
			if err := validateMoxyDispatchWorkflowContract(job); err == nil {
				t.Fatal("mutation unexpectedly satisfied the dispatch contract")
			}
		})
	}
}

func validGateDispatchWorkflowJob() releaseWorkflowJob {
	return releaseWorkflowJob{
		Needs:       "release",
		If:          "startsWith(inputs.release_tag || github.ref_name, 'v')",
		Uses:        gateDispatchWorkflowPath + "@" + gateDispatchWorkflowMajorTag,
		Permissions: map[string]string{"contents": "read", "id-token": "write"},
		With: map[string]string{
			"target-repository": "gate",
			"target-workflow":   "bump-managed-dependency.yml",
			"target-ref":        "master",
			"inputs-json": `{
  "dependency": "geyserlite",
  "version": "${{ inputs.release_tag || github.ref_name }}",
  "source_repository": "${{ github.repository }}",
  "source_release_url": "${{ github.server_url }}/${{ github.repository }}/releases/tag/${{ inputs.release_tag || github.ref_name }}"
}
`,
		},
		Secrets: "inherit",
	}
}

func validateGateDispatchWorkflowContract(dispatch releaseWorkflowJob) error {
	if dispatch.Needs != "release" || dispatch.If != "startsWith(inputs.release_tag || github.ref_name, 'v')" {
		return fmt.Errorf("dispatch release gate = needs %q, if %q", dispatch.Needs, dispatch.If)
	}
	if !reflect.DeepEqual(dispatch.Permissions, map[string]string{"contents": "read", "id-token": "write"}) {
		return fmt.Errorf("dispatch permissions = %#v", dispatch.Permissions)
	}
	if dispatch.Secrets != "inherit" {
		return fmt.Errorf("dispatch secrets = %q, want inherit", dispatch.Secrets)
	}

	workflowRefPath, ref, ok := strings.Cut(dispatch.Uses, "@")
	if !ok || workflowRefPath != gateDispatchWorkflowPath {
		return fmt.Errorf("dispatch workflow path = %q, want %q", workflowRefPath, gateDispatchWorkflowPath)
	}
	if immutableWorkflowRef.MatchString(ref) {
		return fmt.Errorf("dispatch workflow ref = %q, want intentional major tag %q instead of an immutable commit pin", ref, gateDispatchWorkflowMajorTag)
	}
	if ref != gateDispatchWorkflowMajorTag {
		return fmt.Errorf("dispatch workflow ref = %q, want intentional major tag %q", ref, gateDispatchWorkflowMajorTag)
	}
	if !reflect.DeepEqual(dispatch.With, validGateDispatchWorkflowJob().With) {
		return fmt.Errorf("dispatch inputs = %#v", dispatch.With)
	}

	return nil
}

func TestReleaseGateDispatchWorkflowContractRejectsMutations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*releaseWorkflowJob)
	}{
		{"commit pin", func(dispatch *releaseWorkflowJob) {
			dispatch.Uses = gateDispatchWorkflowPath + "@1965ed6ae602a602f9f98edcb31fe177403e8d77"
		}},
		{"branch", func(dispatch *releaseWorkflowJob) { dispatch.Uses = gateDispatchWorkflowPath + "@main" }},
		{"other major tag", func(dispatch *releaseWorkflowJob) { dispatch.Uses = gateDispatchWorkflowPath + "@v2" }},
		{"versioned tag", func(dispatch *releaseWorkflowJob) { dispatch.Uses = gateDispatchWorkflowPath + "@v1.2.3" }},
		{"empty ref", func(dispatch *releaseWorkflowJob) { dispatch.Uses = gateDispatchWorkflowPath + "@" }},
		{"alternate owner", func(dispatch *releaseWorkflowJob) {
			dispatch.Uses = "other/actions/.github/workflows/dispatch-workflow.yml@v1"
		}},
		{"release gate", func(dispatch *releaseWorkflowJob) { dispatch.Needs = "other" }},
		{"permissions", func(dispatch *releaseWorkflowJob) { dispatch.Permissions["contents"] = "write" }},
		{"secrets", func(dispatch *releaseWorkflowJob) { dispatch.Secrets = "explicit" }},
		{"target repository", func(dispatch *releaseWorkflowJob) { dispatch.With["target-repository"] = "other" }},
		{"target ref", func(dispatch *releaseWorkflowJob) { dispatch.With["target-ref"] = "main" }},
		{"payload", func(dispatch *releaseWorkflowJob) { dispatch.With["inputs-json"] = "{}\n" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dispatch := validGateDispatchWorkflowJob()
			tt.mutate(&dispatch)
			if err := validateGateDispatchWorkflowContract(dispatch); err == nil {
				t.Fatal("mutation unexpectedly satisfied the dispatch contract")
			}
		})
	}
}

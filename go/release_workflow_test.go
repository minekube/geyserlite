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
	Needs       string            `yaml:"needs"`
	If          string            `yaml:"if"`
	Uses        string            `yaml:"uses"`
	Permissions map[string]string `yaml:"permissions"`
	With        map[string]string `yaml:"with"`
	Secrets     string            `yaml:"secrets"`
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

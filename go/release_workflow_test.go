// SPDX-License-Identifier: MIT
package geyserlite

import (
	"errors"
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

	var workflow struct {
		Jobs map[string]struct {
			Needs       string            `yaml:"needs"`
			If          string            `yaml:"if"`
			Uses        string            `yaml:"uses"`
			Permissions map[string]string `yaml:"permissions"`
			With        map[string]string `yaml:"with"`
			Secrets     string            `yaml:"secrets"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(workflowBytes, &workflow); err != nil {
		t.Fatal(err)
	}

	dispatch, ok := workflow.Jobs["dispatch-gate-bump"]
	if !ok {
		t.Fatal("dispatch-gate-bump job is missing")
	}
	if dispatch.Needs != "release" || dispatch.If != "startsWith(inputs.release_tag || github.ref_name, 'v')" {
		t.Fatalf("dispatch release gate = needs %q, if %q", dispatch.Needs, dispatch.If)
	}
	if !reflect.DeepEqual(dispatch.Permissions, map[string]string{"contents": "read", "id-token": "write"}) {
		t.Fatalf("dispatch permissions = %#v", dispatch.Permissions)
	}
	if dispatch.Secrets != "inherit" {
		t.Fatalf("dispatch secrets = %q, want inherit", dispatch.Secrets)
	}

	workflowRefPath, ref, ok := strings.Cut(dispatch.Uses, "@")
	if !ok || workflowRefPath != gateDispatchWorkflowPath {
		t.Fatalf("dispatch workflow path = %q, want %q", workflowRefPath, gateDispatchWorkflowPath)
	}
	if immutableWorkflowRef.MatchString(ref) {
		t.Fatalf("dispatch workflow ref = %q, want intentional major tag %q instead of an immutable commit pin", ref, gateDispatchWorkflowMajorTag)
	}
	if ref != gateDispatchWorkflowMajorTag {
		t.Fatalf("dispatch workflow ref = %q, want intentional major tag %q", ref, gateDispatchWorkflowMajorTag)
	}

	wantInputs := map[string]string{
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
	}
	if !reflect.DeepEqual(dispatch.With, wantInputs) {
		t.Fatalf("dispatch inputs = %#v, want %#v", dispatch.With, wantInputs)
	}
}

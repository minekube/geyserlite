// SPDX-License-Identifier: MIT
package geyserlite

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// A release publishes the artifacts that native-image.yml built for the last
// first-parent commit that touched the native build inputs — not for the tag
// commit. Those uploads carry `retention-days`, while a release can be cut
// arbitrarily later: a go/**-only fix does not move NATIVE_SHA, so a delayed
// release reaches for artifacts that no longer exist.
//
// Observed on geyserlite v0.6.1 (2026-10-17, Release run 39064010350): the
// native run 38049688205 was `completed/success` for d7d8bd5, but its
// geyserlite-* uploads had expired (only the 90-day `*.dockerbuild` blobs were
// left), so `gh run download --pattern 'geyserlite-*'` matched nothing. The
// release published 0 assets, skipped crates.io and BOTH cascade dispatches,
// and looked published because the tag and a release object existed. A human
// had to re-run the native run and then re-run Release.
//
// .github/scripts/fetch-native-artifacts.sh is that repair, automated and
// fail-closed: it re-runs THAT run (same run id, same head sha) to republish
// the artifacts and never substitutes another run, another SHA or an older
// artifact set. The tests below drive the real script with a stub `gh` so every
// branch — including "red native build still fails the release" — is proven
// without a 40-minute CI round trip.

const (
	nativeArtifactsScriptPath  = "../.github/scripts/fetch-native-artifacts.sh"
	nativeArtifactsWorkflow    = "../.github/workflows/native-image.yml"
	nativeArtifactsReleasePath = "../.github/workflows/release.yml"

	nativeArtifactsRunID = "4242"
	nativeArtifactsSHA   = "d7d8bd5785bbe8f4bd3d0f5a45747aff27a83350"

	nativeArtifactsStepName = "Pull native artifacts for this release"
)

// nativeArtifactsStubGH is a `gh` stand-in implementing exactly the endpoints
// the script uses. Its state lives in a scenario directory: runs.json plus
// per-phase state sequences and artifact lists, so a "re-run republished the
// expired uploads" transition is expressed as data, not as timing.
const nativeArtifactsStubGH = `#!/usr/bin/env bash
set -euo pipefail
S="${STUB_STATE:?STUB_STATE must point at the scenario directory}"
printf '%s\n' "$*" >> "${S}/calls.log"

phase() {
  if [ "$(cat "${S}/rerun_count" 2>/dev/null || echo 0)" -gt 0 ] &&
     [ -f "${S}/artifacts_after.json" ]; then
    echo after
  else
    echo before
  fi
}

next_state() {
  local p seq idxf idx total line status conclusion
  p=$(phase)
  seq="${S}/state_seq_${p}.txt"
  [ -f "${seq}" ] || seq="${S}/state_seq_before.txt"
  idxf="${S}/idx_${p}"
  idx=$(( $(cat "${idxf}" 2>/dev/null || echo 0) + 1 ))
  echo "${idx}" > "${idxf}"
  total=$(wc -l < "${seq}" | tr -d ' ')
  [ "${idx}" -gt "${total}" ] && idx=${total}
  line=$(sed -n "${idx}p" "${seq}")
  status=${line%% *}
  conclusion=
  if [ "${status}" = "completed" ]; then
    conclusion=$(echo "${line}" | awk '{print $2}')
  fi
  printf '{"id":%s,"status":"%s","conclusion":"%s","head_sha":"%s","html_url":"https://github.test/runs/%s"}\n' \
    "${2:-4242}" "${status}" "${conclusion}" "$(cat "${S}/sha")" "${2:-4242}"
}

cmd=${1:?}; shift
case "${cmd}" in
  api)
    method=GET
    path=
    while [ $# -gt 0 ]; do
      case "$1" in
        --method|-X) method=${2:?}; shift 2 ;;
        --paginate|--silent) shift ;;
        /repos/*) path=$1; shift ;;
        *) shift ;;
      esac
    done
    queryless=${path%%\?*}
    case "${queryless}" in
      */actions/workflows/*/runs) cat "${S}/runs.json" ;;
      */actions/runs/*/artifacts) cat "${S}/artifacts_$(phase).json" ;;
      */actions/runs/*/rerun)
        [ "${method}" = "POST" ] || { echo "stub gh: rerun requires POST" >&2; exit 2; }
        if [ "$(cat "${S}/rerun_allowed" 2>/dev/null || echo 1)" = "0" ]; then
          echo "gh: HTTP 403: Resource not accessible by integration (createWorkflowRunRerun)" >&2
          exit 1
        fi
        n=$(( $(cat "${S}/rerun_count" 2>/dev/null || echo 0) + 1 ))
        echo "${n}" > "${S}/rerun_count"
        ;;
      */actions/runs/*)
        id=${queryless##*/}
        next_state "${method}" "${id}"
        ;;
      *) echo "stub gh: unhandled api path ${path}" >&2; exit 2 ;;
    esac
    ;;
  run)
    sub=${1:?}; shift
    [ "${sub}" = "download" ] || { echo "stub gh: unhandled run subcommand ${sub}" >&2; exit 2; }
    dir=.
    pattern=
    while [ $# -gt 0 ]; do
      case "$1" in
        --dir) dir=${2:?}; shift 2 ;;
        --pattern) pattern=${2:?}; shift 2 ;;
        *) shift ;;
      esac
    done
    matched=0
    while IFS= read -r name; do
      [ -n "${name}" ] || continue
      # shellcheck disable=SC2254
      case "${name}" in ${pattern}) ;; *) continue ;; esac
      matched=$((matched + 1))
      mkdir -p "${dir}/${name}"
      case "${name}" in
        *windows*) printf 'exe\n' > "${dir}/${name}/geyserlite-windows-amd64.exe" ;;
        *amd64*) printf 'elf\n' > "${dir}/${name}/geyserlite-linux-amd64" ;;
        *arm64*) printf 'elf\n' > "${dir}/${name}/geyserlite-linux-arm64" ;;
        *) printf 'bin\n' > "${dir}/${name}/payload" ;;
      esac
      printf 'header\n' > "${dir}/${name}/libgeyserlite.h"
    done < <(jq -r '.artifacts[] | select(.expired | not) | .name | select(startswith("geyserlite-"))' \
      "${S}/artifacts_$(phase).json")
    if [ "${matched}" -eq 0 ]; then
      echo "no artifact matches any of the names or patterns provided" >&2
      exit 1
    fi
    ;;
  *) echo "stub gh: unhandled command ${cmd}" >&2; exit 2 ;;
esac
`

type nativeArtifactsScenario struct {
	name string
	// stateBefore is the run-state sequence before any re-run; stateAfter
	// applies once a re-run has been requested. A sequence is consumed one
	// entry per state read and clamps on its last entry, so the last entry is
	// the resting state.
	stateBefore []string
	stateAfter  []string
	// artifactsBefore/After are artifact names the run publishes (before/after
	// a re-run). Expired uploads are absent from this list — that is exactly
	// what the GitHub API reports once retention elapses.
	artifactsBefore []string
	artifactsAfter  []string
	noRun           bool
	rerunAllowed    bool

	rerunAttempts string

	wantErr         bool
	wantErrContains []string
	wantReruns      int
	wantDownloaded  bool
	wantNoRerunCall bool
}

func nativeArtifactsRunsJSON(scenario nativeArtifactsScenario) string {
	if scenario.noRun {
		return `{"total_count":0,"workflow_runs":[]}`
	}
	// The discovery query filters by head sha on the client; both the matching
	// run and an unrelated one are present so a wrong-SHA pick cannot pass.
	return fmt.Sprintf(`{"total_count":2,"workflow_runs":[
  {"id":9001,"head_sha":"1111111111111111111111111111111111111111","status":"completed","conclusion":"success","created_at":"2026-10-11T00:00:00Z","html_url":"https://github.test/runs/9001"},
  {"id":%s,"head_sha":"%s","status":"completed","conclusion":"success","created_at":"2026-10-10T11:48:00Z","html_url":"https://github.test/runs/%s"}
]}`, nativeArtifactsRunID, nativeArtifactsSHA, nativeArtifactsRunID)
}

func nativeArtifactsJSON(names []string) string {
	entries := make([]string, 0, len(names)+1)
	// The docker build-summary blobs are always present and 90-day lived; they
	// must never be counted as release artifacts.
	entries = append(entries, `{"name":"minekube~geyserlite~WUHLKO.dockerbuild","expired":false}`)
	for _, name := range names {
		entries = append(entries, fmt.Sprintf(`{"name":%q,"expired":false}`, name))
	}
	for _, name := range names {
		entries = append(entries, fmt.Sprintf(`{"name":%q,"expired":true}`, name+"-expired-copy"))
	}
	return fmt.Sprintf(`{"total_count":%d,"artifacts":[%s]}`, len(entries), strings.Join(entries, ","))
}

// nativeArtifactsRunScript executes the real acquisition script against the
// stub `gh` and returns its combined output and error.
func nativeArtifactsRunScript(t *testing.T, scenario nativeArtifactsScenario) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("the acquisition script is bash; CI covers it on ubuntu-latest")
	}
	for _, tool := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is required to drive the acquisition script: %v", tool, err)
		}
	}
	scriptAbs, err := filepath.Abs(nativeArtifactsScriptPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(scriptAbs); err != nil {
		if _, checkoutErr := os.Stat("../.git"); os.IsNotExist(checkoutErr) {
			t.Skipf("%s is unavailable outside a repository checkout", nativeArtifactsScriptPath)
		}
		t.Fatal(err)
	}

	root := t.TempDir()
	stubDir := filepath.Join(root, "bin")
	scenarioDir := filepath.Join(root, "scenario")
	artifactsDir := filepath.Join(root, "artifacts")
	for _, dir := range []string{stubDir, scenarioDir, artifactsDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	writeNativeArtifactsFile(t, filepath.Join(stubDir, "gh"), nativeArtifactsStubGH, 0o755)
	writeNativeArtifactsFile(t, filepath.Join(scenarioDir, "runs.json"), nativeArtifactsRunsJSON(scenario), 0o644)
	writeNativeArtifactsFile(t, filepath.Join(scenarioDir, "sha"), nativeArtifactsSHA, 0o644)
	writeNativeArtifactsFile(t, filepath.Join(scenarioDir, "artifacts_before.json"),
		nativeArtifactsJSON(scenario.artifactsBefore), 0o644)
	writeNativeArtifactsFile(t, filepath.Join(scenarioDir, "artifacts_after.json"),
		nativeArtifactsJSON(scenario.artifactsAfter), 0o644)
	threshold := "impossible"
	writeNativeArtifactsFile(t, filepath.Join(scenarioDir, "state_seq_before.txt"),
		nativeArtifactsSequence(scenario.stateBefore, threshold), 0o644)
	if scenario.stateAfter != nil {
		writeNativeArtifactsFile(t, filepath.Join(scenarioDir, "state_seq_after.txt"),
			nativeArtifactsSequence(scenario.stateAfter, threshold), 0o644)
	}
	if scenario.rerunAllowed {
		writeNativeArtifactsFile(t, filepath.Join(scenarioDir, "rerun_allowed"), "1", 0o644)
	} else {
		writeNativeArtifactsFile(t, filepath.Join(scenarioDir, "rerun_allowed"), "0", 0o644)
	}

	rerunAttempts := scenario.rerunAttempts
	if rerunAttempts == "" {
		rerunAttempts = "1"
	}

	cmd := exec.Command("bash", scriptAbs)
	cmd.Dir = root
	// Override (not append to) the inherited environment: duplicate names make
	// the child's getenv implementation-defined.
	overrides := map[string]string{
		"PATH":                      stubDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"STUB_STATE":                scenarioDir,
		"GITHUB_REPOSITORY":         "minekube/geyserlite",
		"NATIVE_SHA":                nativeArtifactsSHA,
		"ARTIFACT_DIR":              artifactsDir,
		"NATIVE_POLL_SECONDS":       "0",
		"NATIVE_DISCOVERY_ATTEMPTS": "2",
		"NATIVE_INFLIGHT_ATTEMPTS":  "3",
		"NATIVE_RERUN_ATTEMPTS":     rerunAttempts,
		"GH_TOKEN":                  "stub-token",
	}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, replaced := overrides[key]; replaced {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	for key, value := range overrides {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	output, err := cmd.CombinedOutput()

	calls, _ := os.ReadFile(filepath.Join(scenarioDir, "calls.log"))
	reruns, _ := os.ReadFile(filepath.Join(scenarioDir, "rerun_count"))

	if scenario.wantErr {
		if err == nil {
			t.Fatalf("acquisition script succeeded, want failure; output:\n%s", output)
		}
		for _, want := range scenario.wantErrContains {
			if !strings.Contains(string(output), want) {
				t.Errorf("failure output does not mention %q; output:\n%s", want, output)
			}
		}
	} else if err != nil {
		t.Fatalf("acquisition script failed: %v; output:\n%s", err, output)
	}

	gotReruns := strings.TrimSpace(string(reruns))
	if gotReruns == "" {
		gotReruns = "0"
	}
	if gotReruns != fmt.Sprint(scenario.wantReruns) {
		t.Errorf("re-run count = %q, want %d; calls:\n%s", gotReruns, scenario.wantReruns, calls)
	}

	if strings.Contains(string(calls), "run download") != scenario.wantDownloaded {
		t.Errorf("download attempted = %v, want %v; calls:\n%s",
			strings.Contains(string(calls), "run download"), scenario.wantDownloaded, calls)
	}

	if scenario.wantNoRerunCall && strings.Contains(string(calls), "rerun") {
		t.Errorf("acquisition script re-ran the native run; calls:\n%s", calls)
	}

	if scenario.wantDownloaded {
		for _, name := range []string{"geyserlite-amd64", "geyserlite-arm64", "geyserlite-windows-amd64"} {
			entries, err := os.ReadDir(filepath.Join(artifactsDir, name))
			if err != nil || len(entries) == 0 {
				t.Errorf("artifact %s was not downloaded into %s: %v", name, artifactsDir, err)
			}
		}
	}

	// No path may ever pull from a run whose head sha is not NATIVE_SHA.
	if strings.Contains(string(calls), "/runs/9001") {
		t.Errorf("acquisition script addressed the run for a different head sha; calls:\n%s", calls)
	}

}

func nativeArtifactsSequence(states []string, fallback string) string {
	if len(states) == 0 {
		states = []string{fallback}
	}
	return strings.Join(states, "\n") + "\n"
}

func writeNativeArtifactsFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func TestNativeArtifactsAcquisition(t *testing.T) {
	t.Parallel()

	allArtifacts := []string{"geyserlite-amd64", "geyserlite-arm64", "geyserlite-windows-amd64"}

	scenarios := []nativeArtifactsScenario{
		{
			// The normal path: fresh artifacts, no re-run, download only.
			name:            "fresh artifacts download without a re-run",
			stateBefore:     []string{"completed success"},
			artifactsBefore: allArtifacts,
			rerunAllowed:    true,
			wantReruns:      0,
			wantDownloaded:  true,
			wantNoRerunCall: true,
		},
		{
			// The v0.6.1 defect: run green, uploads expired, download empty.
			name:            "expired artifacts are republished by re-running that exact run",
			stateBefore:     []string{"completed success"},
			stateAfter:      []string{"in_progress", "completed success"},
			artifactsBefore: nil,
			artifactsAfter:  allArtifacts,
			rerunAllowed:    true,
			wantReruns:      1,
			wantDownloaded:  true,
		},
		{
			// A partially expired upload set is the same defect with a subtler
			// symptom: an incomplete release that passes an asset-count check.
			name:            "a partially expired upload set is repaired too",
			stateBefore:     []string{"completed success"},
			stateAfter:      []string{"in_progress", "completed success"},
			artifactsBefore: []string{"geyserlite-arm64"},
			artifactsAfter:  allArtifacts,
			rerunAllowed:    true,
			wantReruns:      1,
			wantDownloaded:  true,
		},
		{
			// A run that is still building must be waited for, not re-run.
			name:            "an in-flight native run is awaited",
			stateBefore:     []string{"queued", "in_progress", "completed success"},
			artifactsBefore: allArtifacts,
			rerunAllowed:    true,
			wantReruns:      0,
			wantDownloaded:  true,
			wantNoRerunCall: true,
		},
		{
			// Fail closed: a red native tree must never publish, not even when
			// the artifacts happen to be present.
			name:            "a red native run fails the release closed",
			stateBefore:     []string{"completed failure"},
			artifactsBefore: allArtifacts,
			rerunAllowed:    true,
			wantErr:         true,
			wantErrContains: []string{"concluded failure", "refusing to publish"},
			wantReruns:      0,
			wantDownloaded:  false,
			wantNoRerunCall: true,
		},
		{
			// The self-heal re-run lands red: still fail closed, never fall
			// back to the (expired) artifacts or another run.
			name:            "a re-run that lands red fails the release closed",
			stateBefore:     []string{"completed success"},
			stateAfter:      []string{"in_progress", "completed failure"},
			artifactsBefore: nil,
			artifactsAfter:  allArtifacts,
			rerunAllowed:    true,
			wantErr:         true,
			wantErrContains: []string{"concluded failure"},
			wantReruns:      1,
			wantDownloaded:  false,
		},
		{
			// A re-run GitHub refuses to start (e.g. the run is too old to be
			// re-run) must be an actionable failure, not a silent timeout.
			name:            "a re-run that never starts fails closed",
			stateBefore:     []string{"completed success"},
			stateAfter:      []string{"completed success"},
			artifactsBefore: nil,
			artifactsAfter:  nil,
			rerunAllowed:    true,
			wantErr:         true,
			wantErrContains: []string{"never started", "re-run it manually"},
			wantReruns:      1,
			wantDownloaded:  false,
		},
		{
			// Re-run succeeded but the uploads are still not there: fail with
			// the run URL instead of publishing a 0-asset release.
			name:            "artifacts still missing after the re-run fails closed",
			stateBefore:     []string{"completed success"},
			stateAfter:      []string{"in_progress", "completed success"},
			artifactsBefore: nil,
			artifactsAfter:  nil,
			rerunAllowed:    true,
			wantErr:         true,
			wantErrContains: []string{"retention window expired", "re-run"},
			wantReruns:      1,
			wantDownloaded:  false,
		},
		{
			// The release token must be allowed to re-run; a 403 has to say so.
			name:            "a forbidden re-run names the missing permission",
			stateBefore:     []string{"completed success"},
			artifactsBefore: nil,
			rerunAllowed:    false,
			wantErr:         true,
			wantErrContains: []string{"actions: write"},
			wantReruns:      0,
			wantDownloaded:  false,
		},
		{
			// NATIVE_SHA resolution pointing at a commit that never ran the
			// native build: actionable error, not 40 minutes of waiting.
			name:            "no native run for the resolved sha fails fast",
			noRun:           true,
			stateBefore:     []string{"completed success"},
			artifactsBefore: allArtifacts,
			rerunAllowed:    true,
			wantErr:         true,
			wantErrContains: []string{"has no run for", nativeArtifactsSHA},
			wantReruns:      0,
			wantDownloaded:  false,
		},
	}

	for _, scenario := range scenarios {
		scenario := scenario
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			nativeArtifactsRunScript(t, scenario)
		})
	}
}

// ── Wiring contract ────────────────────────────────────────────────────────

type nativeArtifactsStep struct {
	Name string            `yaml:"name"`
	Uses string            `yaml:"uses"`
	Run  string            `yaml:"run"`
	Env  map[string]string `yaml:"env"`
	With map[string]any    `yaml:"with"`
}

type nativeArtifactsJob struct {
	Strategy struct {
		Matrix struct {
			Include []struct {
				Arch string `yaml:"arch"`
			} `yaml:"include"`
		} `yaml:"matrix"`
	} `yaml:"strategy"`
	Steps []nativeArtifactsStep `yaml:"steps"`
}

type nativeArtifactsWorkflowFile struct {
	Permissions map[string]string `yaml:"permissions"`
	On          struct {
		Push struct {
			Paths []string `yaml:"paths"`
		} `yaml:"push"`
		PullRequest struct {
			Paths []string `yaml:"paths"`
		} `yaml:"pull_request"`
	} `yaml:"on"`
	Jobs map[string]nativeArtifactsJob `yaml:"jobs"`
}

func nativeArtifactsReadWorkflow(t *testing.T, path string) nativeArtifactsWorkflowFile {
	t.Helper()

	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if _, checkoutErr := os.Stat("../.git"); os.IsNotExist(checkoutErr) {
			t.Skipf("%s is unavailable outside a repository checkout", path)
		}
	}
	if err != nil {
		t.Fatal(err)
	}

	var workflow nativeArtifactsWorkflowFile
	if err := yaml.Unmarshal(content, &workflow); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return workflow
}

func nativeArtifactsReadScript(t *testing.T) string {
	t.Helper()

	content, err := os.ReadFile(nativeArtifactsScriptPath)
	if os.IsNotExist(err) {
		if _, checkoutErr := os.Stat("../.git"); os.IsNotExist(checkoutErr) {
			t.Skipf("%s is unavailable outside a repository checkout", nativeArtifactsScriptPath)
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

// TestReleasePullsNativeArtifactsThroughTheSelfHealingScript pins the wiring:
// the release job delegates to the script (single source of truth, testable)
// and holds the permission the self-heal needs.
func TestReleasePullsNativeArtifactsThroughTheSelfHealingScript(t *testing.T) {
	release := nativeArtifactsReadWorkflow(t, nativeArtifactsReleasePath)

	if got := release.Permissions["actions"]; got != "write" {
		t.Errorf("release.yml permissions.actions = %q, want \"write\"; the self-heal "+
			"re-runs the expired native worklow run and a read-only token fails with 403", got)
	}

	var step *nativeArtifactsStep
	for _, job := range release.Jobs {
		for i := range job.Steps {
			if job.Steps[i].Name == nativeArtifactsStepName {
				step = &job.Steps[i]
			}
		}
	}
	if step == nil {
		t.Fatalf("release.yml has no %q step", nativeArtifactsStepName)
	}
	if !strings.Contains(step.Run, "fetch-native-artifacts.sh") {
		t.Errorf("%q does not invoke the acquisition script; run:\n%s", nativeArtifactsStepName, step.Run)
	}
	// A repair dispatch checks the TAG out, so a tag cut before this script
	// existed has no in-tree copy. The step must fall back to the default
	// branch's version instead of failing on a missing file.
	if !strings.Contains(step.Run, `[ ! -f "$script" ]`) {
		t.Errorf("%q does not fall back when the checked-out ref has no copy of the script; "+
			"a repair dispatch of an older tag would fail with \"No such file\"; run:\n%s",
			nativeArtifactsStepName, step.Run)
	}
	if !strings.Contains(step.Run, "DEFAULT_BRANCH") || !strings.Contains(step.Env["DEFAULT_BRANCH"], "default_branch") {
		t.Errorf("%q does not take the fallback copy from the repository's default branch; env:\n%v",
			nativeArtifactsStepName, step.Env)
	}
	if strings.Contains(step.Run, "gh run download") {
		t.Errorf("%q still downloads artifacts inline; the download must go through the "+
			"self-healing script, otherwise an expired artifact set fails opaquely again", nativeArtifactsStepName)
	}
}

// TestNativeImageArtifactsOutliveAReleaseCut pins the retention floor. Seven
// days was the whole bug: release-please can cut a tag long after the native
// build that owns its artifacts, so the uploads must outlive a realistic
// release delay (the self-heal repairs the rest).
func TestNativeImageArtifactsOutliveAReleaseCut(t *testing.T) {
	const minimumRetentionDays = 30

	native := nativeArtifactsReadWorkflow(t, nativeArtifactsWorkflow)

	uploaded := 0
	for jobName, job := range native.Jobs {
		for _, step := range job.Steps {
			if step.Uses != "actions/upload-artifact@v7" && !strings.HasPrefix(step.Uses, "actions/upload-artifact@") {
				continue
			}
			uploaded++
			days, ok := step.With["retention-days"]
			if !ok {
				t.Errorf("job %s uploads %v without retention-days; the release path depends on "+
					"how long these live", jobName, step.With["name"])
				continue
			}
			value := fmt.Sprint(days)
			if value != fmt.Sprint(minimumRetentionDays) && !nativeArtifactsAtLeastDays(value, minimumRetentionDays) {
				t.Errorf("job %s uploads with retention-days %s, want at least %d: a release cut "+
					"later than that finds no artifacts and publishes an empty release",
					jobName, value, minimumRetentionDays)
			}
		}
	}
	if uploaded == 0 {
		t.Fatal("native-image.yml uploads no artifacts")
	}
}

func nativeArtifactsAtLeastDays(value string, minimum int) bool {
	var days int
	if _, err := fmt.Sscanf(value, "%d", &days); err != nil {
		return false
	}
	return days >= minimum
}

// TestNativeArtifactExpectationMatchesUploads pins the expected artifact set to
// the workflow's own upload names, so adding or renaming an arch cannot leave
// the release silently waiting for (or accepting a partial set of) the wrong
// names.
func TestNativeArtifactExpectationMatchesUploads(t *testing.T) {
	native := nativeArtifactsReadWorkflow(t, nativeArtifactsWorkflow)
	script := nativeArtifactsReadScript(t)

	expected := nativeArtifactsExpectedNames(t, native)
	if len(expected) == 0 {
		t.Fatal("native-image.yml declares no artifact uploads")
	}
	for _, name := range expected {
		if !strings.Contains(script, name) {
			t.Errorf("acquired artifact %q is not required by %s; the release could republish "+
				"a partial artifact set without noticing", name, nativeArtifactsScriptPath)
		}
	}
}

func nativeArtifactsExpectedNames(t *testing.T, native nativeArtifactsWorkflowFile) []string {
	t.Helper()

	var names []string
	for _, job := range native.Jobs {
		arches := make([]string, 0, len(job.Strategy.Matrix.Include))
		for _, include := range job.Strategy.Matrix.Include {
			if include.Arch != "" {
				arches = append(arches, include.Arch)
			}
		}
		for _, step := range job.Steps {
			if !strings.HasPrefix(step.Uses, "actions/upload-artifact@") {
				continue
			}
			name := fmt.Sprint(step.With["name"])
			switch {
			case strings.Contains(name, "${{ matrix.arch }}"):
				if len(arches) == 0 {
					t.Fatalf("upload %q expands matrix.arch but the job has no matrix.include arches", name)
				}
				for _, arch := range arches {
					names = append(names, strings.ReplaceAll(name, "${{ matrix.arch }}", arch))
				}
			case name == "" || name == "<nil>":
				t.Fatalf("upload step in native-image.yml has no name")
			default:
				names = append(names, name)
			}
		}
	}
	return names
}

// TestNativeShaResolutionPathsAreNativeTriggers pins the invariant that makes
// the discovery step terminate: every path release.yml resolves NATIVE_SHA
// from must also trigger native-image.yml, otherwise the release can resolve to
// a commit that never ran the native build.
func TestNativeShaResolutionPathsAreNativeTriggers(t *testing.T) {
	native := nativeArtifactsReadWorkflow(t, nativeArtifactsWorkflow)
	script := nativeArtifactsReadScript(t)

	const marker = "NATIVE_SOURCE_PATHS=("
	start := strings.Index(script, marker)
	if start < 0 {
		t.Fatalf("%s does not declare NATIVE_SOURCE_PATHS", nativeArtifactsScriptPath)
	}
	rest := script[start+len(marker):]
	end := strings.Index(rest, ")")
	if end < 0 {
		t.Fatalf("NATIVE_SOURCE_PATHS in %s is unterminated", nativeArtifactsScriptPath)
	}
	resolutionPaths := strings.Fields(rest[:end])
	if len(resolutionPaths) == 0 {
		t.Fatal("NATIVE_SOURCE_PATHS is empty")
	}

	triggers := native.On.Push.Paths
	if len(triggers) == 0 {
		t.Fatal("native-image.yml has no push paths")
	}
	for _, path := range resolutionPaths {
		if !nativeArtifactsPathCovered(path, triggers) {
			t.Errorf("NATIVE_SHA resolves from %q but native-image.yml does not run on pushes to it; "+
				"the release would wait for a run that can never exist", path)
		}
	}
}

// TestCIDoesNotSkipTheSelfHealingScript is the "workflow-only PR can never
// merge" trap: `lint-test` is the only required context on main, so a change
// that ci.yml's paths filter does not match produces no CI run, no required
// check, and a permanently blocked PR. The acquisition script is maintained as
// a standalone file, so its path must be in the filter.
func TestCIDoesNotSkipTheSelfHealingScript(t *testing.T) {
	ci := nativeArtifactsReadWorkflow(t, "../.github/workflows/ci.yml")

	scriptPath := strings.TrimPrefix(nativeArtifactsScriptPath, "../")
	for event, paths := range map[string][]string{
		"push":         ci.On.Push.Paths,
		"pull_request": ci.On.PullRequest.Paths,
	} {
		if len(paths) == 0 {
			t.Fatalf("ci.yml has no %s paths", event)
		}
		if !nativeArtifactsPathCovered(scriptPath, paths) {
			t.Errorf("ci.yml %s filter does not cover %s; a script-only PR would never get the "+
				"required lint-test context and could never merge", event, scriptPath)
		}
	}
}

// nativeArtifactsPathCovered reports whether any of the GitHub Actions `paths`
// patterns covers the given path.
func nativeArtifactsPathCovered(path string, patterns []string) bool {
	for _, pattern := range patterns {
		if pattern == path {
			return true
		}
		if prefix, ok := strings.CutSuffix(pattern, "/**"); ok {
			if path == prefix || strings.HasPrefix(path, prefix+"/") {
				return true
			}
		}
	}
	return false
}

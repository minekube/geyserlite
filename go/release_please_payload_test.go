// SPDX-License-Identifier: MIT
package geyserlite

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The "Validate release PR" step used to build its first command out of the
// release PR payload with a single-quoted shell literal:
//
//	PR_NUMBER=$(echo '${{ steps.rp.outputs.pr }}' | jq -r '.number')
//
// `steps.rp.outputs.pr` is GitHub's JSON for the generated release PR *including
// its body*, and that body repeats the merged commit subjects verbatim. One
// apostrophe in a subject (`don't`, `Geyser's`, `Operator's`) closes the quoted
// literal, the rest of the payload is parsed as shell code, and the step dies
// with
//
//	/home/runner/work/_temp/<id>.sh: line 1: syntax error near unexpected token `('
//	##[error]Process completed with exit code 2
//
// The release PR then never gets merged: the release-please job goes red while
// ci.yml is green on the same merge sha, so nothing tells the author, and the
// whole chain (tag -> release.yml -> crates.io -> Gate dependency bump) silently
// skips the release. This repository is the worst place for that failure: its
// release.yml resolves NATIVE_SHA at the tag from the first-parent commit that
// touched build/**, so a release whose chain never ran leaves a tag that cannot
// be repaired afterwards (v0.5.28 case) — the next tag is the only recovery.
//
// The payload must therefore always reach the shell as DATA (an environment
// variable), never as script text. The tests below pin that boundary, run the
// step for real against a payload whose body carries an apostrophe, and prove
// the detection rejects the historical shape and the weakenings of the policy.
const (
	// releasePleaseWorkflowPath is relative to the repository root; the tests
	// resolve it against the package directory ("..").
	releasePleaseWorkflowPath  = ".github/workflows/release-please.yml"
	releasePleaseMergeStepName = "Validate release PR"

	// releasePleasePayloadEnvVar carries `steps.rp.outputs.pr` into the shell as
	// data instead of into the script as code.
	releasePleasePayloadEnvVar = "RP_PR"

	// releasePleasePolicyPrefix marks every assertion below so the mutation
	// harness can tell a policy rejection from an unrelated failure.
	releasePleasePolicyPrefix = "release-please workflow policy:"

	releasePleaseMutationChildEnv = "GEYSERLITE_RELEASE_PLEASE_PAYLOAD_MUTATION_CHILD"

	// releasePleaseStubHeadSHA is what the stub `gh` reports as the release
	// PR's head commit; the step only needs it to be a stable non-empty value.
	releasePleaseStubHeadSHA = "3f7c1d4e8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d"
)

// releasePleasePRBodyApostrophe is a release-notes body of the exact shape the
// real payload carries, with a merged subject that contains an apostrophe — the
// input that killed gate's v0.74.21 chain and would kill this one too.
const releasePleasePRBodyApostrophe = `:robot: I have created a release *beep* *boop*
---


## [0.5.31](https://github.com/minekube/geyserlite/compare/v0.5.30...v0.5.31) (2026-09-27)


### Bug Fixes

* **go:** don't let the embedded config failure swallow Geyser's startup error ([#212](https://github.com/minekube/geyserlite/issues/212)) ([b41c9de](https://github.com/minekube/geyserlite/commit/b41c9de))


### Dependencies

* Operator's bind address is no longer rewritten before it reaches Gate ([#211](https://github.com/minekube/geyserlite/issues/211)) ([0f7a12c](https://github.com/minekube/geyserlite/commit/0f7a12c))

---
This PR was generated with [Release Please](https://github.com/googleapis/release-please). See [documentation](https://github.com/googleapis/release-please#release-please).`

// releasePleasePRBodyQuoteFree has the same shape with a merged subject that
// contains no apostrophe: the release notes that always worked, which is why the
// defect stayed invisible until a subject like `Geyser's` shipped.
const releasePleasePRBodyQuoteFree = `:robot: I have created a release *beep* *boop*
---


## [0.5.32](https://github.com/minekube/geyserlite/compare/v0.5.31...v0.5.32) (2026-09-28)


### Bug Fixes

* **deps:** update module github.com/ebitengine/purego to v0.11.2 ([#214](https://github.com/minekube/geyserlite/issues/214)) ([7c2b8ff](https://github.com/minekube/geyserlite/commit/7c2b8ff))

---
This PR was generated with [Release Please](https://github.com/googleapis/release-please). See [documentation](https://github.com/googleapis/release-please#release-please).`

// releasePleasePayload renders the JSON GitHub passes to the step, with the same
// keys and key order as the payload release-please-action emits.
func releasePleasePayload(t *testing.T, number int, title, body string) string {
	t.Helper()
	payload := struct {
		HeadBranchName string   `json:"headBranchName"`
		BaseBranchName string   `json:"baseBranchName"`
		Number         int      `json:"number"`
		Title          string   `json:"title"`
		Body           string   `json:"body"`
		Files          []string `json:"files"`
		Labels         []string `json:"labels"`
	}{
		HeadBranchName: "release-please--branches--main--components--geyserlite",
		BaseBranchName: "main",
		Number:         number,
		Title:          title,
		Body:           body,
		Files:          []string{},
		Labels:         []string{"autorelease: pending"},
	}
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	// The payload arrives as raw JSON; do not HTML-escape it.
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(payload); err != nil {
		t.Fatal(err)
	}
	return strings.TrimRight(encoded.String(), "\n")
}

type releasePleaseStep struct {
	Name string            `yaml:"name"`
	If   string            `yaml:"if"`
	Env  map[string]string `yaml:"env"`
	Run  string            `yaml:"run"`
}

type releasePleaseWorkflow struct {
	Jobs map[string]struct {
		Steps []releasePleaseStep `yaml:"steps"`
	} `yaml:"jobs"`
}

// releasePleaseWorkflowSource reads release-please.yml and normalises line
// endings, so the mutation harness can anchor on bytes regardless of how the
// checkout spelled newlines (Windows checkouts may carry CRLF).
func releasePleaseWorkflowSource(t *testing.T, dir string) []byte {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(dir, releasePleaseWorkflowPath))
	if err != nil {
		t.Fatal(err)
	}
	return []byte(strings.ReplaceAll(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\r", "\n"))
}

func readReleasePleaseWorkflow(t *testing.T, dir string) releasePleaseWorkflow {
	t.Helper()
	var workflow releasePleaseWorkflow
	if err := yaml.Unmarshal(releasePleaseWorkflowSource(t, dir), &workflow); err != nil {
		t.Fatal(err)
	}
	return workflow
}

func readReleasePleaseMergeStep(t *testing.T, dir string) releasePleaseStep {
	t.Helper()
	for _, step := range readReleasePleaseWorkflow(t, dir).Jobs["release-please"].Steps {
		if step.Name == releasePleaseMergeStepName {
			return step
		}
	}
	t.Fatalf("%s the release-please workflow must keep the %q step", releasePleasePolicyPrefix, releasePleaseMergeStepName)
	return releasePleaseStep{}
}

// releasePleaseWantCommands is the command plan the auto-merge step must execute,
// in order, once comments and line continuations are resolved. Pinning the whole
// plan (rather than grepping for a few commands) is what makes the mutation
// harness below meaningful: a merge that is only mentioned in a comment, wrapped
// in `echo`, made asynchronous, or stripped of the required-check mirroring
// changes this plan and is rejected.
func releasePleaseWantCommands() []string {
	return []string{
		`set -euo pipefail`,
		`PR_NUMBER=$(printf '%s' "$RP_PR" | jq -r '.number')`,
		`PR_JSON=$(gh pr view "$PR_NUMBER" --repo "$GITHUB_REPOSITORY" --json headRefName,headRefOid)`,
		`HEAD_REF=$(echo "$PR_JSON" | jq -r '.headRefName')`,
		`HEAD_SHA=$(echo "$PR_JSON" | jq -r '.headRefOid')`,
		`echo "Dispatching CI for release PR #$PR_NUMBER at $HEAD_REF ($HEAD_SHA)"`,
		`gh workflow run ci.yml --repo "$GITHUB_REPOSITORY" --ref "$HEAD_REF"`,
		`RUN_ID=""`,
		`for _ in $(seq 1 60); do`,
		`RUN_ID=$(gh run list --repo "$GITHUB_REPOSITORY" --workflow ci.yml --branch "$HEAD_REF" --event workflow_dispatch --json databaseId,headSha --jq "map(select(.headSha == \"$HEAD_SHA\")) | .[0].databaseId // \"\"")`,
		`if [ -n "$RUN_ID" ]; then`,
		`break`,
		`fi`,
		`sleep 5`,
		`done`,
		`if [ -z "$RUN_ID" ]; then`,
		`echo "::error::Timed out waiting for dispatched CI run to appear"`,
		`exit 1`,
		`fi`,
		`echo "Waiting for CI run $RUN_ID"`,
		`for _ in $(seq 1 180); do`,
		`RUN_JSON=$(gh run view "$RUN_ID" --repo "$GITHUB_REPOSITORY" --json status,conclusion,url)`,
		`STATUS=$(echo "$RUN_JSON" | jq -r '.status')`,
		`CONCLUSION=$(echo "$RUN_JSON" | jq -r '.conclusion // ""')`,
		`RUN_URL=$(echo "$RUN_JSON" | jq -r '.url')`,
		`if [ "$STATUS" = "completed" ]; then`,
		`break`,
		`fi`,
		`sleep 10`,
		`done`,
		`if [ "$STATUS" != "completed" ]; then`,
		`echo "::error::Timed out waiting for CI run $RUN_ID"`,
		`exit 1`,
		`fi`,
		`CHECK_CONCLUSION="$CONCLUSION"`,
		`if [ "$CHECK_CONCLUSION" = "cancelled" ]; then`,
		`CHECK_CONCLUSION="neutral"`,
		`fi`,
		`gh api "repos/$GITHUB_REPOSITORY/check-runs" -f name='lint-test' -f head_sha="$HEAD_SHA" -f status='completed' -f conclusion="$CHECK_CONCLUSION" -f details_url="$RUN_URL" -f output[title]="Release PR CI $CONCLUSION" -f output[summary]="Mirrors workflow_dispatch CI run $RUN_ID for release PR #$PR_NUMBER: $RUN_URL"`,
		`STATUS_STATE="failure"`,
		`if [ "$CONCLUSION" = "success" ]; then`,
		`STATUS_STATE="success"`,
		`fi`,
		`gh api "repos/$GITHUB_REPOSITORY/statuses/$HEAD_SHA" -f state="$STATUS_STATE" -f context='lint-test' -f target_url="$RUN_URL" -f description="Release PR CI $CONCLUSION via workflow_dispatch"`,
		`if [ "$CONCLUSION" != "success" ]; then`,
		`echo "::error::Release PR CI concluded $CONCLUSION"`,
		`exit 1`,
		`fi`,
		`echo "Merging release PR #$PR_NUMBER"`,
		`for _ in $(seq 1 30); do`,
		`if gh pr merge "$PR_NUMBER" --repo "$GITHUB_REPOSITORY" --merge; then`,
		`break`,
		`fi`,
		`STATE=$(gh pr view "$PR_NUMBER" --repo "$GITHUB_REPOSITORY" --json state --jq '.state')`,
		`if [ "$STATE" = "MERGED" ]; then`,
		`break`,
		`fi`,
		`sleep 10`,
		`done`,
		`STATE=$(gh pr view "$PR_NUMBER" --repo "$GITHUB_REPOSITORY" --json state --jq '.state')`,
		`if [ "$STATE" != "MERGED" ]; then`,
		`echo "::error::Release PR #$PR_NUMBER did not merge"`,
		`exit 1`,
		`fi`,
		`gh workflow run release-please.yml --repo "$GITHUB_REPOSITORY" --ref main`,
	}
}

// releasePleasePayloadParseCommand is the single command that turns the payload
// into the PR number; it must read from the environment.
const releasePleasePayloadParseCommand = `PR_NUMBER=$(printf '%s' "$RP_PR" | jq -r '.number')`

// TestReleasePleaseMergeStepNeverInterpolatesPayloadIntoShell pins the boundary
// that stalled v0.74.21 on the sibling repository: the release PR payload reaches
// the shell as an environment variable, never as script text. The assertions run
// against the commands the shell would actually execute (comments and line
// continuations resolved), so a decoy that only mentions a command in a comment
// cannot pass.
func TestReleasePleaseMergeStepNeverInterpolatesPayloadIntoShell(t *testing.T) {
	step := readReleasePleaseMergeStep(t, "..")

	// The step only has work to do once release-please generated a release PR;
	// that guard is what keeps a green run from merging anything else.
	if got := strings.TrimSpace(step.If); got != "${{ steps.rp.outputs.pr }}" {
		t.Fatalf("%s the auto-merge step must run only for a generated release PR (if: ${{ steps.rp.outputs.pr }}), got %q", releasePleasePolicyPrefix, got)
	}

	if got := step.Env[releasePleasePayloadEnvVar]; got != "${{ steps.rp.outputs.pr }}" {
		t.Fatalf("%s the auto-merge step must receive the release PR payload as %s, got %q",
			releasePleasePolicyPrefix, releasePleasePayloadEnvVar, got)
	}

	commands := releasePleaseShellCommands(step.Run)
	// Any expression rendered into `run` becomes script text before the shell
	// parses it, so any expression here can turn payload data into shell code.
	for _, command := range commands {
		if strings.Contains(command, "${") {
			t.Fatalf("%s the auto-merge step must not interpolate expressions into its shell script, found %q", releasePleasePolicyPrefix, command)
		}
	}

	parsed := false
	for _, command := range commands {
		if command == releasePleasePayloadParseCommand {
			parsed = true
		}
	}
	if !parsed {
		t.Fatalf("%s the auto-merge step must parse the payload as data with %q", releasePleasePolicyPrefix, releasePleasePayloadParseCommand)
	}

	if want := releasePleaseWantCommands(); !reflect.DeepEqual(commands, want) {
		t.Fatalf("%s the auto-merge step must run exactly these commands:\nwant:\n%q\ngot:\n%q", releasePleasePolicyPrefix, want, commands)
	}
}

// TestReleasePleaseRunBlocksCarryNoInterpolatedExpressions extends the boundary
// to every step of this workflow: no `run` block may render a GitHub expression
// into its script text, and the repository name must come from the runner's own
// $GITHUB_REPOSITORY. The dispatch job is included, so the tag name it hands to
// gh travels through the environment too.
func TestReleasePleaseRunBlocksCarryNoInterpolatedExpressions(t *testing.T) {
	for job, definition := range readReleasePleaseWorkflow(t, "..").Jobs {
		for _, step := range definition.Steps {
			if strings.TrimSpace(step.Run) == "" {
				continue
			}
			for _, command := range releasePleaseShellCommands(step.Run) {
				if strings.Contains(command, "${") {
					t.Fatalf("%s job %q step %q must not interpolate expressions into its shell script, found %q",
						releasePleasePolicyPrefix, job, step.Name, command)
				}
			}
		}
	}
}

// TestWorkflowRunBlocksNeverSingleQuoteExpressions is the repository-wide sweep
// for the defect shape that stalled the release chain: an expression wrapped in a
// single-quoted shell literal inside a `run` block, where one apostrophe in the
// rendered value closes the literal and the rest is parsed as shell code. Values
// belong in `env:` (or, for the payload, in an environment variable) and are read
// with `"$VAR"` from the script.
func TestWorkflowRunBlocksNeverSingleQuoteExpressions(t *testing.T) {
	paths, err := filepath.Glob("../.github/workflows/*.y*ml")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Skip("no workflow files available outside a repository checkout")
	}
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var workflow releasePleaseWorkflow
		if err := yaml.Unmarshal(contents, &workflow); err != nil {
			t.Fatalf("%s is not parseable: %v", path, err)
		}
		for job, definition := range workflow.Jobs {
			for _, step := range definition.Steps {
				if strings.Contains(step.Run, "'${{") {
					t.Fatalf("%s job %q step %q wraps an expression in a single-quoted shell literal; carry it through `env:` and read it as \"$VAR\"",
						path, job, step.Name)
				}
			}
		}
	}
}

// releasePleaseShellCommands returns the commands the shell would execute for a
// step `run` block: whole-line and trailing comments dropped, line continuations
// joined, whitespace collapsed.
func releasePleaseShellCommands(run string) []string {
	var commands []string
	joined := ""
	for _, line := range strings.Split(run, "\n") {
		trimmed := strings.TrimSpace(releasePleaseUncommentedShell(line))
		if trimmed == "" {
			continue
		}
		continued := strings.HasSuffix(trimmed, "\\")
		trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, "\\"))
		if joined != "" {
			joined += " "
		}
		joined += trimmed
		if !continued {
			commands = append(commands, strings.Join(strings.Fields(joined), " "))
			joined = ""
		}
	}
	if joined != "" {
		commands = append(commands, strings.Join(strings.Fields(joined), " "))
	}
	return commands
}

// releasePleaseUncommentedShell drops a shell comment from a line, honouring
// single and double quotes so a `#` inside a quoted argument survives.
func releasePleaseUncommentedShell(line string) string {
	var quote rune
	for index, char := range line {
		switch {
		case quote != 0:
			if char == quote {
				quote = 0
			}
		case char == '\'' || char == '"':
			quote = char
		case char == '#':
			return line[:index]
		}
	}
	return line
}

var releasePleaseExpression = regexp.MustCompile(`\$\{\{[^}]*\}\}`)

// renderReleasePleaseStepText resolves the GitHub expressions the runner resolves
// before the shell ever sees the script. It deliberately substitutes the release
// PR payload as *text* for `${{ steps.rp.outputs.pr }}`: a workflow that
// interpolates the payload into `run` must reproduce the production failure.
func renderReleasePleaseStepText(text, payload string) string {
	return releasePleaseExpression.ReplaceAllStringFunc(text, func(expression string) string {
		switch strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(expression, "${{"), "}}")) {
		case "steps.rp.outputs.pr":
			return payload
		case "github.repository":
			return "minekube/geyserlite"
		default:
			// The stub `gh` never reads GH_TOKEN, so any remaining expression
			// (e.g. `secrets.GITHUB_TOKEN`) only needs a non-empty value.
			return "stub"
		}
	})
}

// releasePleaseStubGH answers the queries the auto-merge step makes — it records
// every invocation, then returns the JSON (or the already-filtered `--jq` value)
// the real `gh` would. It never talks to GitHub, so the harness is offline.
const releasePleaseStubGH = `#!/bin/sh
printf '%s\n' "$*" >> "$GH_LOG"
case "$*" in
  *"--json headRefName,headRefOid"*)
    printf '{"headRefName":"release-please--branches--main--components--geyserlite","headRefOid":"%s"}\n' "$GH_STUB_HEAD_SHA"
    ;;
  *"run list"*)
    printf '4242\n'
    ;;
  *"run view"*)
    printf '{"status":"completed","conclusion":"success","url":"https://github.example/minekube/geyserlite/actions/runs/4242"}\n'
    ;;
  *"--json state"*)
    printf 'MERGED\n'
    ;;
  *"pr merge"*)
    printf 'Merged pull request\n'
    ;;
esac
exit 0
`

// runReleasePleaseMergeStep executes the step's own shell script the way the
// runner does (bash -e) with a stub `gh` on PATH, and returns the combined script
// output plus the recorded `gh` invocations.
func runReleasePleaseMergeStep(t *testing.T, run string, stepEnv map[string]string, payload string) (string, error) {
	t.Helper()

	if runtime.GOOS == "windows" {
		// The auto-merge step only ever runs on ubuntu-latest, and Git Bash cannot
		// execute the Windows temp path this harness would hand it. The payload
		// policy assertions above (and the mutation harness) cover every platform.
		t.Skip("the auto-merge step is a POSIX-shell step; nothing to prove on Windows")
	}
	shell, err := exec.LookPath("bash")
	if err != nil {
		shell, err = exec.LookPath("sh")
	}
	if err != nil {
		t.Skipf("no bash/sh on PATH; the auto-merge step is a bash step (the runner executes it with /usr/bin/bash -e): %v", err)
	}
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skipf("the auto-merge step parses its payload with jq and it is not on PATH: %v", err)
	}

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ghLog := filepath.Join(dir, "gh.log")
	// The stub records every gh invocation so the assertions can tell a real
	// `gh pr merge <number>` / rerun dispatch from a shell that never got there.
	if err := os.WriteFile(filepath.Join(binDir, "gh"), []byte(releasePleaseStubGH), 0o755); err != nil {
		t.Fatal(err)
	}

	script := filepath.Join(dir, "release-please-auto-merge.sh")
	if err := os.WriteFile(script, []byte(renderReleasePleaseStepText(run, payload)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	env := []string{
		"PATH=" + binDir + string(filepath.ListSeparator) + os.Getenv("PATH"),
		"GH_LOG=" + ghLog,
		"GH_STUB_HEAD_SHA=" + releasePleaseStubHeadSHA,
		"HOME=" + dir,
		"GITHUB_REPOSITORY=minekube/geyserlite",
	}
	for name, value := range stepEnv {
		env = append(env, name+"="+renderReleasePleaseStepText(value, payload))
	}

	cmd := exec.Command(shell, "-e", script)
	cmd.Dir = dir
	cmd.Env = env
	output, runErr := cmd.CombinedOutput()
	logged, err := os.ReadFile(ghLog)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(output) + "recorded gh calls:\n" + string(logged), runErr
}

// TestReleasePleaseMergeStepHandlesApostrophesInReleaseNotes runs the workflow's
// own auto-merge step against a payload whose body repeats a merged subject with
// an apostrophe. That is normal input — a merged `fix:` subject can contain one —
// so the step must mirror the required check, merge the release PR and dispatch
// the rerun for it, exactly as it does for quote-free release notes.
func TestReleasePleaseMergeStepHandlesApostrophesInReleaseNotes(t *testing.T) {
	step := readReleasePleaseMergeStep(t, "..")

	cases := []struct {
		name      string
		payload   string
		wantMerge string
	}{
		{
			name:      "apostrophe-in-release-notes",
			payload:   releasePleasePayload(t, 216, "chore(main): release 0.5.31", releasePleasePRBodyApostrophe),
			wantMerge: "pr merge 216 --repo minekube/geyserlite --merge",
		},
		{
			name:      "quote-free-release-notes",
			payload:   releasePleasePayload(t, 217, "chore(main): release 0.5.32", releasePleasePRBodyQuoteFree),
			wantMerge: "pr merge 217 --repo minekube/geyserlite --merge",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			output, runErr := runReleasePleaseMergeStep(t, step.Run, step.Env, testCase.payload)
			if runErr != nil {
				t.Fatalf("the auto-merge step failed for a release PR payload with %s:\n%v\n%s", testCase.name, runErr, output)
			}
			for _, want := range []string{
				testCase.wantMerge,
				"api repos/minekube/geyserlite/check-runs -f name=lint-test",
				"api repos/minekube/geyserlite/statuses/" + releasePleaseStubHeadSHA + " -f state=success",
				"workflow run release-please.yml --repo minekube/geyserlite --ref main",
			} {
				if !strings.Contains(output, want) {
					t.Fatalf("the auto-merge step did not run `gh %s`:\n%s", want, output)
				}
			}
		})
	}
}

// TestReleasePleaseDetectionCatchesShellInterpolatedPayload rebuilds the
// pre-fix step body (the payload interpolated into a single-quoted shell literal)
// and shows the harness above catches it — the RED this change started from —
// while the same shape still works for quote-free release notes, which is why the
// defect was silent until an apostrophe shipped.
func TestReleasePleaseDetectionCatchesShellInterpolatedPayload(t *testing.T) {
	step := readReleasePleaseMergeStep(t, "..")

	historicalRun := step.Run
	if strings.Contains(historicalRun, releasePleasePayloadParseCommand) {
		historicalRun = strings.Replace(historicalRun, `printf '%s' "$RP_PR"`, `echo '${{ steps.rp.outputs.pr }}'`, 1)
	} else if !strings.Contains(historicalRun, "${{ steps.rp.outputs.pr }}") {
		t.Fatalf("%s the auto-merge step no longer parses the payload nor interpolates it; update this mutation", releasePleasePolicyPrefix)
	}

	apostrophePayload := releasePleasePayload(t, 216, "chore(main): release 0.5.31", releasePleasePRBodyApostrophe)
	output, runErr := runReleasePleaseMergeStep(t, historicalRun, step.Env, apostrophePayload)
	if runErr == nil {
		t.Fatalf("a shell-interpolated payload must fail; the harness reported success:\n%s", output)
	}
	// The shell never got as far as merging anything, so assert on that as well
	// as on the parse error: Linux bash reports
	// `syntax error near unexpected token `('` while older bash 3.2 reports
	// `unexpected EOF while looking for matching `"`.
	if loweredOutput := strings.ToLower(output); !strings.Contains(loweredOutput, "syntax error") && !strings.Contains(loweredOutput, "unexpected eof") {
		t.Fatalf("the shell-interpolated payload must fail while the shell parses the script, got %v:\n%s", runErr, output)
	}
	if !strings.HasSuffix(output, "recorded gh calls:\n") {
		t.Fatalf("the historical shape must not reach gh at all:\n%s", output)
	}

	quoteFreePayload := releasePleasePayload(t, 217, "chore(main): release 0.5.32", releasePleasePRBodyQuoteFree)
	if output, runErr := runReleasePleaseMergeStep(t, historicalRun, step.Env, quoteFreePayload); runErr != nil {
		t.Fatalf("the historical shape must still work for quote-free release notes (the defect was silent); got %v:\n%s", runErr, output)
	}
}

type releasePleaseWorkflowMutation struct {
	id    string
	apply func([]byte) ([]byte, error)
}

func replaceReleasePleaseWorkflow(id string, anchor, replacement string) releasePleaseWorkflowMutation {
	return releasePleaseWorkflowMutation{id: id, apply: func(source []byte) ([]byte, error) {
		if count := bytes.Count(source, []byte(anchor)); count != 1 {
			return nil, fmt.Errorf("mutation %s: anchor %q matched %d times, want exactly once", id, anchor, count)
		}
		return bytes.Replace(source, []byte(anchor), []byte(replacement), 1), nil
	}}
}

// TestReleasePleasePayloadPolicyRejectsWeakeningMutations proves the payload
// policy tests are load-bearing: every candidate below keeps the workflow
// YAML-parseable and still must be rejected by the real policy assertions.
func TestReleasePleasePayloadPolicyRejectsWeakeningMutations(t *testing.T) {
	if os.Getenv(releasePleaseMutationChildEnv) == "1" {
		t.Skip("mutation child runs only the payload policy tests")
	}

	baseline := releasePleaseWorkflowSource(t, "..")

	mutations := []releasePleaseWorkflowMutation{
		// The historical, defect-carrying form: the payload interpolated into a
		// single-quoted shell literal.
		replaceReleasePleaseWorkflow("shell-interpolated-payload", `printf '%s' "$RP_PR"`, `echo '${{ steps.rp.outputs.pr }}'`),
		replaceReleasePleaseWorkflow("single-quoted-payload-expression", `printf '%s' "$RP_PR"`, `printf '%s' '${{ steps.rp.outputs.pr }}'`),
		// Interpolation and semantics weakenings that keep the shape readable.
		replaceReleasePleaseWorkflow("unquoted-payload", `printf '%s' "$RP_PR"`, `printf '%s' $RP_PR`),
		replaceReleasePleaseWorkflow("payload-not-parsed-as-json", `printf '%s' "$RP_PR" | jq -r '.number'`, `printf '%s' "$RP_PR"`),
		replaceReleasePleaseWorkflow("payload-echo-inert", `printf '%s' "$RP_PR" | jq -r '.number'`, `echo "$RP_PR" | jq -r '.number'`),
		replaceReleasePleaseWorkflow("payload-env-removed", "          RP_PR: ${{ steps.rp.outputs.pr }}\n", ""),
		replaceReleasePleaseWorkflow("repository-expression-interpolated", `gh workflow run ci.yml --repo "$GITHUB_REPOSITORY" --ref "$HEAD_REF"`, `gh workflow run ci.yml --repo "${{ github.repository }}" --ref "$HEAD_REF"`),
		replaceReleasePleaseWorkflow("release-tag-expression-interpolated", `--ref "$RELEASE_TAG"`, `--ref "${{ needs.release-please.outputs.tag_name }}"`),
		replaceReleasePleaseWorkflow("async-merge", `--merge; then`, `--auto; then`),
		// The trust boundary of this step: the required-check mirroring, the
		// commit status, and the post-merge state verification.
		replaceReleasePleaseWorkflow("required-check-mirror-removed", `gh api "repos/$GITHUB_REPOSITORY/check-runs"`, `echo "check-runs mirror removed"`),
		replaceReleasePleaseWorkflow("commit-status-mirror-removed", `gh api "repos/$GITHUB_REPOSITORY/statuses/$HEAD_SHA"`, `echo "status mirror removed"`),
		replaceReleasePleaseWorkflow("merge-state-verification-removed", `if [ "$STATE" != "MERGED" ]; then`, `if false; then`),
		// Decoys: the merge only mentioned in a comment, or wrapped in `echo`.
		replaceReleasePleaseWorkflow("merge-in-comment-decoy", "            if gh pr merge \"$PR_NUMBER\" \\\n", "            # gh pr merge \"$PR_NUMBER\" \\\n"),
		replaceReleasePleaseWorkflow("merge-echo-inert", `if gh pr merge "$PR_NUMBER" \`, `if echo gh pr merge "$PR_NUMBER" \`),
		replaceReleasePleaseWorkflow("rerun-dispatch-in-comment-decoy", "          gh workflow run release-please.yml \\\n", "          # gh workflow run release-please.yml \\\n"),
		replaceReleasePleaseWorkflow("rerun-dispatch-echo-inert", `gh workflow run release-please.yml \`, `echo gh workflow run release-please.yml \`),
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	for _, mutation := range mutations {
		mutation := mutation
		t.Run(mutation.id, func(t *testing.T) {
			candidate, err := mutation.apply(baseline)
			if err != nil {
				t.Fatal(err)
			}
			var parsed yaml.Node
			if err := yaml.Unmarshal(candidate, &parsed); err != nil {
				t.Fatalf("mutation is not YAML-parseable: %v", err)
			}

			// Mirror the repository layout, because the policy tests resolve the
			// workflow relative to the package directory.
			tmp := t.TempDir()
			if err := os.MkdirAll(filepath.Join(tmp, "go"), 0o755); err != nil {
				t.Fatal(err)
			}
			workflowDir := filepath.Join(tmp, filepath.Dir(releasePleaseWorkflowPath))
			if err := os.MkdirAll(workflowDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(tmp, releasePleaseWorkflowPath), candidate, 0o644); err != nil {
				t.Fatal(err)
			}

			cmd := exec.Command(executable,
				"-test.run=^TestReleasePlease(MergeStepNeverInterpolatesPayloadIntoShell|RunBlocksCarryNoInterpolatedExpressions)$",
				"-test.count=1")
			cmd.Dir = filepath.Join(tmp, "go")
			cmd.Env = append(os.Environ(), releasePleaseMutationChildEnv+"=1")
			output, runErr := cmd.CombinedOutput()
			if runErr == nil {
				t.Fatalf("weakening survived the payload policy tests\n%s", output)
			}
			if !bytes.Contains(output, []byte(releasePleasePolicyPrefix)) {
				t.Fatalf("weakening failed for the wrong reason; want a policy assertion\n%s", output)
			}
			t.Logf("MUTATION_REJECTED|%s|reason=release-please-payload-policy", mutation.id)
		})
	}
}

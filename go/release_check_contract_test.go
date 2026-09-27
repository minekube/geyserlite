// SPDX-License-Identifier: MIT
package geyserlite

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Every release PR shows a red `CI` check and it can never go green. That is
// not a broken job and it is not the `on.pull_request.paths` filter: it is
// GitHub's documented approval gate for pull requests that a workflow created
// with `GITHUB_TOKEN`.
//
//	"When a pull request is created or updated by a workflow using
//	 `GITHUB_TOKEN`, `pull_request` events with the `opened`, `synchronize`, or
//	 `reopened` activity types create workflow runs that require approval. A
//	 user with write access to the repository can approve these runs from the
//	 pull request page. With the exception of `workflow_dispatch` and
//	 `repository_dispatch`, other `GITHUB_TOKEN`-triggered events do not create
//	 workflow runs at all."
//	— GitHub docs, "Events that trigger workflows" -> `pull_request`
//
// Observed on this repository (2026-09-27, v0.5.31): release PR #219 head
// 27e30bfe produced run 36324253811 — `event=pull_request`, `CI`,
// `.github/workflows/ci.yml`, 0 jobs, conclusion `failure`, annotation "This
// workflow run required approval but was not approved before it expired". The
// same head sha carries the *dispatched* ci.yml run that release-please.yml
// mirrors onto the required `lint-test` context. The sibling repository gate
// reports the same runs with the raw `action_required` conclusion, and every
// release PR there, in connect-java and in vialite, shows the same shape.
//
// The release branch therefore behaves like this, and both halves are pinned
// below because the disposition in ci.yml depends on them:
//
//   - ci.yml *is* triggered on a release PR (release-please rewrites
//     go/version.go, rust/**, CHANGELOG.md and the manifest, which match this
//     workflow's `paths` filter), so the approval-gated run exists and reports
//     a failed check named `CI`.
//   - native-image.yml is *not* triggered on a release PR (its filter is
//     build/**, mise.toml, its own file), which is the control: a workflow
//     whose `paths` filter does not match produces no run at all, not a failed
//     one.
//
// Consequence, and the reason this test exists: `CI` (`CI / lint-test`, ...)
// must never be added to the required status checks of `main`. No release PR
// can ever satisfy that context — the run is created only to sit in
// `action_required` until the PR is merged, at which point GitHub records it
// as `failure`. The release chain would then stall on every release instead of
// merging itself, and the mirrored `lint-test` dance in release-please.yml
// would be dead code. The only way to make the check real is to have
// release-please open its PR with a GitHub App/PAT token instead of
// `GITHUB_TOKEN` (GitHub docs, "Triggering a workflow from a workflow": an App
// installation token or PAT "also lets `pull_request` workflows run
// automatically (without the approval prompt described above) when the pull
// request is created or updated by automation"). That is a credential change,
// not a workflow edit, so it is deliberately not done here.
const releaseCheckContractPrefix = "release-check contract:"

const (
	releaseCheckCIWorkflowPath     = ".github/workflows/ci.yml"
	releaseCheckNativeWorkflowPath = ".github/workflows/native-image.yml"
)

// releaseCheckReleasePRFiles is the file set release-please writes into its own
// release PR. Observed on #219 (v0.5.31) and unchanged across earlier releases;
// it is the input the approval-gate analysis depends on.
var releaseCheckReleasePRFiles = []string{
	".release-please-manifest.json",
	"CHANGELOG.md",
	"go/version.go",
	"rust/Cargo.toml",
	"rust/src/version.rs",
}

// releaseCheckWantMirroredContexts are the branch-protection contexts the
// release-please auto-merge step satisfies for a release PR: a `lint-test`
// check-run plus a `lint-test` commit status. `lint-test` is the only context
// required on `main`.
var releaseCheckWantMirroredContexts = []string{"lint-test"}

// releaseCheckContract is every workflow fact the acceptance decision in ci.yml
// rests on. It is a plain value so the mutation table below can rebuild it.
type releaseCheckContract struct {
	ciWorkflowName              string
	ciPullRequestPaths          []string
	nativeImagePullRequestPaths []string
	// releasePleaseTokenInput is the `token:` the release-please action step
	// receives. Empty means it uses `GITHUB_TOKEN`, i.e. the approval gate
	// applies.
	releasePleaseTokenInput string
	mirroredContexts        []string
}

// releaseCheckLiveContract reads the three workflows and assembles the contract.
func releaseCheckLiveContract(t *testing.T, dir string) releaseCheckContract {
	t.Helper()
	ci := releaseCheckReadWorkflow(t, filepath.Join(dir, releaseCheckCIWorkflowPath))
	native := releaseCheckReadWorkflow(t, filepath.Join(dir, releaseCheckNativeWorkflowPath))
	return releaseCheckContract{
		ciWorkflowName:              releaseCheckScalar(ci, "name"),
		ciPullRequestPaths:          releaseCheckTriggerPaths(t, releaseCheckCIWorkflowPath, ci, "pull_request"),
		nativeImagePullRequestPaths: releaseCheckTriggerPaths(t, releaseCheckNativeWorkflowPath, native, "pull_request"),
		releasePleaseTokenInput:     releaseCheckReleasePleaseToken(t, dir),
		mirroredContexts:            releaseCheckMirroredContexts(t, dir),
	}
}

// validateReleaseCheckContract fails when any fact the documented disposition
// in ci.yml rests on has changed. The messages name the disposition so a future
// reader re-decides it instead of removing the guard.
func validateReleaseCheckContract(c releaseCheckContract, releaseFiles []string) error {
	if c.ciWorkflowName != "CI" {
		return fmt.Errorf("the workflow the release PR sees as a red check is no longer named %q (got %q); "+
			"the acceptance note in %s and the trap description name it explicitly", "CI", c.ciWorkflowName, releaseCheckCIWorkflowPath)
	}

	var matched []string
	for _, file := range releaseFiles {
		if releaseCheckMatchesAny(c.ciPullRequestPaths, file) {
			matched = append(matched, file)
		}
	}
	if len(matched) == 0 {
		return fmt.Errorf("%s no longer triggers on a release PR: none of %v matches %v. "+
			"The approval-gated `CI` run cannot exist then, so the disposition in %s (accepted red check, "+
			"`lint-test` mirrored by release-please.yml) has to be re-decided before this test is updated",
			releaseCheckCIWorkflowPath, releaseFiles, c.ciPullRequestPaths, releaseCheckCIWorkflowPath)
	}

	for _, file := range releaseFiles {
		if releaseCheckMatchesAny(c.nativeImagePullRequestPaths, file) {
			return fmt.Errorf("%s now triggers on release PRs as well (%q matches %v); the control that shows a "+
				"non-matching `paths` filter creates no run at all — and therefore that the red check is the "+
				"approval gate, not the filter — no longer holds",
				releaseCheckNativeWorkflowPath, file, c.nativeImagePullRequestPaths)
		}
	}

	if c.releasePleaseTokenInput != "" {
		return fmt.Errorf("the release-please action now receives a token (%q), so its pull request is no longer "+
			"created with GITHUB_TOKEN; the approval gate described in %s does not apply anymore. Remove the "+
			"acceptance note, re-check whether the run is now green, and reconsider both the mirroring step and "+
			"whether `CI` may become a required check", c.releasePleaseTokenInput, releaseCheckCIWorkflowPath)
	}

	mirrored := append([]string(nil), c.mirroredContexts...)
	sort.Strings(mirrored)
	want := append([]string(nil), releaseCheckWantMirroredContexts...)
	sort.Strings(want)
	if strings.Join(mirrored, ",") != strings.Join(want, ",") {
		return fmt.Errorf("release-please.yml now mirrors %v for branch protection, want exactly %v. %s is the only "+
			"required context on main: mirroring %q would make `CI` satisfiable and invalidate the rule in %s; "+
			"mirroring anything else leaves the required context unsatisfied",
			c.mirroredContexts, releaseCheckWantMirroredContexts, "lint-test", "CI", releaseCheckCIWorkflowPath)
	}
	return nil
}

// TestReleaseBranchCheckContractMatchesLiveWorkflows is the guard: the live
// workflows must still satisfy every fact the accepted-noise disposition in
// ci.yml depends on.
func TestReleaseBranchCheckContractMatchesLiveWorkflows(t *testing.T) {
	contract := releaseCheckLiveContract(t, "..")
	if err := validateReleaseCheckContract(contract, releaseCheckReleasePRFiles); err != nil {
		t.Fatalf("%s %v", releaseCheckContractPrefix, err)
	}
	if contract.releasePleaseTokenInput != "" {
		t.Fatalf("%s the release-please step must not pass a token, got %q", releaseCheckContractPrefix, contract.releasePleaseTokenInput)
	}
}

// TestReleaseBranchCheckContractRejectsMutations proves the guard has teeth:
// each mutation below invalidates the mechanism the disposition rests on and
// must be rejected with a contract-prefixed error.
func TestReleaseBranchCheckContractRejectsMutations(t *testing.T) {
	live := releaseCheckLiveContract(t, "..")
	mutations := map[string]func(c releaseCheckContract) releaseCheckContract{
		"ci-renamed": func(c releaseCheckContract) releaseCheckContract {
			c.ciWorkflowName = "ci"
			return c
		},
		"ci-paths-filter-dropped": func(c releaseCheckContract) releaseCheckContract {
			c.ciPullRequestPaths = nil
			return c
		},
		"ci-release-files-no-longer-match": func(c releaseCheckContract) releaseCheckContract {
			c.ciPullRequestPaths = []string{"examples/**", "docs/**"}
			return c
		},
		"native-image-now-matches": func(c releaseCheckContract) releaseCheckContract {
			c.nativeImagePullRequestPaths = append(append([]string(nil), c.nativeImagePullRequestPaths...), "go/**")
			return c
		},
		"release-please-token-added": func(c releaseCheckContract) releaseCheckContract {
			c.releasePleaseTokenInput = "${{ secrets.RELEASE_PLEASE_TOKEN }}"
			return c
		},
		"ci-context-mirrored": func(c releaseCheckContract) releaseCheckContract {
			c.mirroredContexts = append(append([]string(nil), c.mirroredContexts...), "CI")
			return c
		},
		"mirror-removed": func(c releaseCheckContract) releaseCheckContract {
			c.mirroredContexts = nil
			return c
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			err := validateReleaseCheckContract(mutate(live), releaseCheckReleasePRFiles)
			if err == nil {
				t.Fatalf("%s mutation %q was accepted", releaseCheckContractPrefix, name)
			}
			if !strings.HasPrefix(err.Error(), releaseCheckContractPrefix) &&
				!strings.Contains(err.Error(), releaseCheckCIWorkflowPath) &&
				!strings.Contains(err.Error(), releaseCheckNativeWorkflowPath) {
				t.Fatalf("%s mutation %q failed without naming the disposition: %v", releaseCheckContractPrefix, name, err)
			}
		})
	}
}

// TestReleaseCheckPathMatcher pins the matcher used above: GitHub's `paths`
// patterns with `**` spanning separators and `*` staying inside one segment.
func TestReleaseCheckPathMatcher(t *testing.T) {
	cases := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"go/**", "go/version.go", true},
		{"go/**", "gofmt/x.go", false},
		{"go/**", "go/a/b/c.go", true},
		{"rust/**", "rust/src/version.rs", true},
		{"rust/**", "rust/Cargo.toml", true},
		{"build/**", "go/version.go", false},
		{"CHANGELOG.md", "CHANGELOG.md", true},
		{".markdownlint*", ".markdownlint.json", true},
		{".markdownlint*", ".markdownlintrc", true},
		{".golangci.yml", ".golangci.yml", true},
		{".github/workflows/ci.yml", ".github/workflows/ci.yml", true},
		{".github/actions/integration-setup/**", ".github/actions/integration-setup/action.yml", true},
		{"examples/**", "examples/basic/main.go", true},
	}
	for _, tc := range cases {
		if got := releaseCheckMatchPath(tc.pattern, tc.path); got != tc.want {
			t.Errorf("%s path %q against pattern %q = %v, want %v", releaseCheckContractPrefix, tc.path, tc.pattern, got, tc.want)
		}
	}
}

// releaseCheckMatchesAny reports whether the file matches at least one pattern.
func releaseCheckMatchesAny(patterns []string, file string) bool {
	for _, pattern := range patterns {
		if releaseCheckMatchPath(pattern, file) {
			return true
		}
	}
	return false
}

// releaseCheckMatchPath implements the subset of GitHub's `paths` globbing this
// repository uses: `**` matches across path separators, `*` and `?` match
// within a single segment.
func releaseCheckMatchPath(pattern, path string) bool {
	return releaseCheckMatchSegments(strings.Split(pattern, "/"), strings.Split(path, "/"))
}

func releaseCheckMatchSegments(pattern, path []string) bool {
	if len(pattern) == 0 {
		return len(path) == 0
	}
	if pattern[0] == "**" {
		for i := 0; i <= len(path); i++ {
			if releaseCheckMatchSegments(pattern[1:], path[i:]) {
				return true
			}
		}
		return false
	}
	if len(path) == 0 {
		return false
	}
	if !releaseCheckMatchSegment(pattern[0], path[0]) {
		return false
	}
	return releaseCheckMatchSegments(pattern[1:], path[1:])
}

func releaseCheckMatchSegment(pattern, segment string) bool {
	if pattern == "" {
		return segment == ""
	}
	switch pattern[0] {
	case '*':
		// `*` never crosses a separator, so it can only consume the rest of
		// this segment.
		for i := 0; i <= len(segment); i++ {
			if releaseCheckMatchSegment(pattern[1:], segment[i:]) {
				return true
			}
		}
		return false
	case '?':
		if segment == "" {
			return false
		}
		return releaseCheckMatchSegment(pattern[1:], segment[1:])
	default:
		if segment == "" || segment[0] != pattern[0] {
			return false
		}
		return releaseCheckMatchSegment(pattern[1:], segment[1:])
	}
}

// releaseCheckReadWorkflow parses a workflow file as a YAML node tree, so the
// `on:` key is read as the literal scalar GitHub writes rather than as the
// boolean a YAML 1.1 resolver would make of it.
func releaseCheckReadWorkflow(t *testing.T, path string) *yaml.Node {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s %v", releaseCheckContractPrefix, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(contents, &doc); err != nil {
		t.Fatalf("%s %s is not parseable: %v", releaseCheckContractPrefix, path, err)
	}
	return &doc
}

func releaseCheckMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func releaseCheckScalar(doc *yaml.Node, key string) string {
	node := releaseCheckMappingValue(doc.Content[0], key)
	if node == nil {
		return ""
	}
	return node.Value
}

// releaseCheckTriggerPaths returns the `paths` a workflow declares for an event
// trigger.
func releaseCheckTriggerPaths(t *testing.T, path string, doc *yaml.Node, event string) []string {
	t.Helper()
	on := releaseCheckMappingValue(doc.Content[0], "on")
	if on == nil {
		t.Fatalf("%s %s declares no `on:` block", releaseCheckContractPrefix, path)
	}
	eventNode := releaseCheckMappingValue(on, event)
	if eventNode == nil {
		t.Fatalf("%s %s does not trigger on %s", releaseCheckContractPrefix, path, event)
	}
	pathsNode := releaseCheckMappingValue(eventNode, "paths")
	if pathsNode == nil {
		t.Fatalf("%s %s no longer filters %s by `paths`; the release PR analysis and the acceptance note in %s assume it does",
			releaseCheckContractPrefix, path, event, releaseCheckCIWorkflowPath)
	}
	var paths []string
	for _, item := range pathsNode.Content {
		paths = append(paths, item.Value)
	}
	return paths
}

// releaseCheckReleasePleaseToken returns the `token:` the release-please action
// step receives (empty means it uses the runner's GITHUB_TOKEN).
func releaseCheckReleasePleaseToken(t *testing.T, dir string) string {
	t.Helper()
	workflow := releaseCheckReadWorkflow(t, filepath.Join(dir, releasePleaseWorkflowPath))
	jobs := releaseCheckMappingValue(workflow.Content[0], "jobs")
	job := releaseCheckMappingValue(jobs, "release-please")
	if job == nil {
		t.Fatalf("%s %s has no release-please job", releaseCheckContractPrefix, releasePleaseWorkflowPath)
	}
	for _, step := range releaseCheckMappingValue(job, "steps").Content {
		uses := releaseCheckMappingValue(step, "uses")
		if uses == nil || !strings.Contains(uses.Value, "release-please-action") {
			continue
		}
		with := releaseCheckMappingValue(step, "with")
		if with == nil {
			return ""
		}
		if token := releaseCheckMappingValue(with, "token"); token != nil {
			return token.Value
		}
		return ""
	}
	t.Fatalf("%s %s no longer uses release-please-action; the release PR (and therefore the approval-gated `CI` run) no longer exists",
		releaseCheckContractPrefix, releasePleaseWorkflowPath)
	return ""
}

// releaseCheckMirroredContexts extracts the branch-protection contexts the
// auto-merge step satisfies: a `lint-test` check-run (`-f name=`) plus a
// `lint-test` commit status (`-f context=`). The step itself is pinned
// command-by-command by TestReleasePleaseMergeStepNeverInterpolatesPayloadIntoShell;
// this only reads the *names* back out so the required-check rule can be
// asserted here.
func releaseCheckMirroredContexts(t *testing.T, dir string) []string {
	t.Helper()
	step := readReleasePleaseMergeStep(t, dir)
	checkRun := regexp.MustCompile(`-f name='([^']*)'`).FindAllStringSubmatch(step.Run, -1)
	commitStatus := regexp.MustCompile(`-f context='([^']*)'`).FindAllStringSubmatch(step.Run, -1)
	if len(checkRun) == 0 || len(commitStatus) == 0 {
		t.Fatalf("%s the auto-merge step must mirror %s as both a check-run and a commit status (got %d check-run and %d commit status mirrors); "+
			"otherwise a release PR can never satisfy the required check", releaseCheckContractPrefix, "lint-test", len(checkRun), len(commitStatus))
	}
	seen := map[string]bool{}
	var contexts []string
	for _, match := range append(checkRun, commitStatus...) {
		if !seen[match[1]] {
			seen[match[1]] = true
			contexts = append(contexts, match[1])
		}
	}
	return contexts
}

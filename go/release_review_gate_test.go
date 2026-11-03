// SPDX-License-Identifier: MIT
package geyserlite

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// Release PRs must not merge before CodeRabbit has actually reviewed them.
//
// The "Validate release PR" step used to go straight from the mirrored required
// `lint-test` context to `gh pr merge`. Nothing waited for CodeRabbit, and it is
// not a required branch-protection context, so the merge closed the PR while the
// review was still running and GitHub recorded the review run as `cancelled`:
//
//	#256 merged 2026-11-03T06:37:31Z — CodeRabbit check started 06:33:23Z,
//	     recorded `cancelled` at 06:38:29Z (the mirrored lint-test check that
//	     satisfied branch protection was created at 06:37:27Z)
//	#251 merged 2026-10-31T20:29:46Z — CodeRabbit check started 20:17:06Z,
//	     recorded `cancelled` at 20:33:28Z
//
// The version bump therefore shipped unreviewed. CodeRabbit must reach a
// terminal state for the release PR head before the merge, and anything but an
// approving conclusion must leave the PR open instead of merging it anyway.
//
// Making CodeRabbit a required branch-protection check is deliberately not the
// fix (that is a settings change nobody can review in this repository, and a
// required context an external reviewer app can leave `cancelled` blocks the
// pull request UI too). The gate lives in .github/workflows/release-please.yml,
// so these tests read that file and run its own shell, exactly like
// release_please_payload_test.go does for the payload boundary.
const (
	// releaseReviewGatePrefix marks every assertion below so the mutation
	// harness can tell a policy rejection from an unrelated failure.
	releaseReviewGatePrefix = "release-please review gate:"

	// releaseReviewMovedHeadSHA is the head commit the stub reports right
	// before the merge when the "the branch moved while it was reviewed" path
	// is exercised.
	releaseReviewMovedHeadSHA = "9f8e7d6c5b4a39281706f5e4d3c2b1a09f8e7d6c"

	// releaseReviewPollArgs identifies the review gate's check-runs query in
	// the recorded `gh` calls (the required-check mirror queries
	// repos/<owner>/<repo>/check-runs and has no /commits/ segment).
	releaseReviewPollArgs = "/commits/" + releasePleaseStubHeadSHA + "/check-runs"

	// releaseReviewDefaultWaitSeconds / releaseReviewDefaultPollSeconds are the
	// cap and cadence the workflow itself uses. 900s is 15 minutes: measured
	// from the CodeRabbit check-runs of release PRs #247..#257
	// (2026-10-30..2026-11-03), the nine successful reviews took 274s at the
	// median (4m34s) and 735s at the slowest (12m15s — this change's own
	// re-review of a76ec0e), so the cap is ~1.2x the worst review actually
	// recorded and ~3.3x the median, leaving ~2m45s of headroom over the
	// slowest real review while keeping the whole release job well inside the
	// runner's 6h limit (the other waits in the step already allow 40 minutes).
	// A review slower than that leaves the PR open rather than merging it
	// unreviewed — the direction the report asked for. (#251 is not in the
	// successful set: its review was cancelled after 16m22s when the PR was
	// merged underneath it.)
	releaseReviewDefaultWaitSeconds = "900"
	releaseReviewDefaultPollSeconds = "15"

	// releaseReviewDefaultRequestSeconds bounds one check-runs request, so a
	// stalled HTTP call cannot outlive the wall-clock deadline.
	releaseReviewDefaultRequestSeconds = "60"
)

// releaseReviewCheckRun is one entry of the scripted check-runs response.
type releaseReviewCheckRun struct {
	Name       string
	Status     string
	Conclusion string
	Detail     string
}

// releaseReviewCheckRunsJSON renders a commit's check-runs response. A run whose
// Conclusion is empty is still running, which is the state #256 merged in.
func releaseReviewCheckRunsJSON(t *testing.T, runs ...releaseReviewCheckRun) string {
	t.Helper()
	type output struct {
		Title string `json:"title"`
	}
	type checkRun struct {
		Name       string  `json:"name"`
		Status     string  `json:"status"`
		Conclusion *string `json:"conclusion"`
		Output     output  `json:"output"`
	}
	document := struct {
		TotalCount int        `json:"total_count"`
		CheckRuns  []checkRun `json:"check_runs"`
	}{TotalCount: len(runs)}
	for _, run := range runs {
		entry := checkRun{Name: run.Name, Status: run.Status, Output: output{Title: run.Detail}}
		if run.Conclusion != "" {
			conclusion := run.Conclusion
			entry.Conclusion = &conclusion
		}
		document.CheckRuns = append(document.CheckRuns, entry)
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded) + "\n"
}

// releaseReviewScenario scripts the check-runs responses the stub `gh` returns,
// one per poll, reusing the last response once the script runs out. It returns
// the environment the step needs to see them.
func releaseReviewScenario(t *testing.T, responses ...string) map[string]string {
	t.Helper()
	if len(responses) == 0 {
		t.Fatal("releaseReviewScenario needs at least one response")
	}
	dir := t.TempDir()
	for index, response := range responses {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d.json", index+1)), []byte(response), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "last.json"), []byte(responses[len(responses)-1]), 0o644); err != nil {
		t.Fatal(err)
	}
	return map[string]string{
		"GH_REVIEW_DIR":   dir,
		"GH_REVIEW_COUNT": filepath.Join(dir, "polls"),
	}
}

// releaseReviewRunStep executes the workflow's own auto-merge step against the
// scripted CodeRabbit responses, with a short cap so a refusal costs seconds
// instead of fifteen minutes.
func releaseReviewRunStep(t *testing.T, env map[string]string, waitSeconds, pollSeconds string) (string, string, error) {
	t.Helper()
	step := readReleasePleaseMergeStep(t, "..")
	merged := map[string]string{}
	for name, value := range step.Env {
		merged[name] = value
	}
	for name, value := range env {
		merged[name] = value
	}
	run := releaseReviewShortenStep(t, step.Run, waitSeconds, pollSeconds)
	payload := releasePleasePayload(t, 256, "chore(main): release 0.7.6", releasePleasePRBodyQuoteFree)
	return runReleasePleaseMergeStep(t, run, merged, payload)
}

// releaseReviewShortenStep rewrites the shipped cap/cadence literals so a
// refusal costs seconds instead of the real 900s. It refuses to guess: the
// shipped values are pinned by the structural test below, and a renamed
// constant makes this fail loudly instead of silently testing the real 900s.
// The single-request timeout is shortened to the cap as well, so the tests
// never rely on a request budget larger than the window they are proving.
func releaseReviewShortenStep(t *testing.T, run, waitSeconds, pollSeconds string) string {
	t.Helper()
	rewrites := []struct{ anchor, replacement string }{
		{"REVIEW_WAIT_SECONDS=" + releaseReviewDefaultWaitSeconds, "REVIEW_WAIT_SECONDS=" + waitSeconds},
		{"REVIEW_POLL_SECONDS=" + releaseReviewDefaultPollSeconds, "REVIEW_POLL_SECONDS=" + pollSeconds},
		{"REVIEW_REQUEST_SECONDS=" + releaseReviewDefaultRequestSeconds, "REVIEW_REQUEST_SECONDS=" + waitSeconds},
	}
	for _, rewrite := range rewrites {
		if count := strings.Count(run, rewrite.anchor); count != 1 {
			t.Fatalf("%s want exactly one %q in the auto-merge step, found %d", releaseReviewGatePrefix, rewrite.anchor, count)
		}
		run = strings.Replace(run, rewrite.anchor, rewrite.replacement, 1)
	}
	return run
}

// releaseReviewPollCount counts the review gate's check-runs queries in the
// recorded `gh` calls — i.e. how many times the step actually looked at the
// review before deciding.
func releaseReviewPollCount(output string) int {
	return strings.Count(output, releaseReviewPollArgs)
}

func releaseReviewRequireSummaryRow(t *testing.T, summary, row, want string) {
	t.Helper()
	if !strings.Contains(summary, row) {
		t.Fatalf("%s the job summary must record %q:\n%s", releaseReviewGatePrefix, row, summary)
	}
	if want != "" && !strings.Contains(summary, want) {
		t.Fatalf("%s the job summary must record %q:\n%s", releaseReviewGatePrefix, want, summary)
	}
}

// TestReleasePleaseReviewGateWaitsForTerminalResultBeforeMerging is the GREEN
// for the report: a review that is still running on the first look and approves
// on the second must be waited for, and only then merged.
func TestReleasePleaseReviewGateWaitsForTerminalResultBeforeMerging(t *testing.T) {
	env := releaseReviewScenario(t,
		// The state #256 merged in: CodeRabbit running, the required check green.
		releaseReviewCheckRunsJSON(t,
			releaseReviewCheckRun{Name: "lint-test", Status: "completed", Conclusion: "success"},
			releaseReviewCheckRun{Name: "CodeRabbit", Status: "in_progress"},
		),
		releaseReviewCheckRunsJSON(t,
			releaseReviewCheckRun{Name: "lint-test", Status: "completed", Conclusion: "success"},
			releaseReviewCheckRun{Name: "CodeRabbit", Status: "completed", Conclusion: "success", Detail: "Review completed"},
		),
	)
	output, summary, runErr := releaseReviewRunStep(t, env, "30", "1")
	if runErr != nil {
		t.Fatalf("%s the step must merge once CodeRabbit approves:\n%v\n%s", releaseReviewGatePrefix, runErr, output)
	}
	if polls := releaseReviewPollCount(output); polls < 2 {
		t.Fatalf("%s the step merged after %d review poll(s); it must not merge while CodeRabbit is still running:\n%s",
			releaseReviewGatePrefix, polls, output)
	}
	mergeIndex := strings.Index(output, "pr merge 256 --repo minekube/geyserlite --merge")
	if mergeIndex < 0 {
		t.Fatalf("%s the approved release PR must still be merged:\n%s", releaseReviewGatePrefix, output)
	}
	if firstPoll := strings.Index(output, releaseReviewPollArgs); firstPoll < 0 || firstPoll > mergeIndex {
		t.Fatalf("%s the review poll must happen before the merge:\n%s", releaseReviewGatePrefix, output)
	}
	releaseReviewRequireSummaryRow(t, summary, "| conclusion | success |", "| merge | true |")
}

// TestReleasePleaseReviewGateRefusesNonApprovingConclusions is the fail-closed
// half: a terminal review that does not approve must leave the PR open, say so
// in the job summary, and never reach `gh pr merge`.
func TestReleasePleaseReviewGateRefusesNonApprovingConclusions(t *testing.T) {
	cases := []string{"failure", "cancelled", "timed_out", "action_required", "stale"}
	for _, conclusion := range cases {
		conclusion := conclusion
		t.Run(conclusion, func(t *testing.T) {
			env := releaseReviewScenario(t, releaseReviewCheckRunsJSON(t,
				releaseReviewCheckRun{Name: "CodeRabbit", Status: "completed", Conclusion: conclusion, Detail: "Review did not approve"},
			))
			output, summary, runErr := releaseReviewRunStep(t, env, "30", "1")
			if runErr == nil {
				t.Fatalf("%s a CodeRabbit conclusion of %q must not merge:\n%s", releaseReviewGatePrefix, conclusion, output)
			}
			if strings.Contains(output, "pr merge 256") {
				t.Fatalf("%s a CodeRabbit conclusion of %q reached the merge:\n%s", releaseReviewGatePrefix, conclusion, output)
			}
			if !strings.Contains(output, "::error::") || !strings.Contains(output, "CodeRabbit") {
				t.Fatalf("%s the refusal must surface the conclusion as an error annotation:\n%s", releaseReviewGatePrefix, output)
			}
			releaseReviewRequireSummaryRow(t, summary, "| conclusion | "+conclusion+" |", "| merge | false |")
			if !strings.Contains(summary, "NOT merged") {
				t.Fatalf("%s the job summary must state that nothing was merged:\n%s", releaseReviewGatePrefix, summary)
			}
			if strings.Contains(output, "workflow run release-please.yml") {
				t.Fatalf("%s a refused review must not re-dispatch the release workflow:\n%s", releaseReviewGatePrefix, output)
			}
		})
	}
}

// TestReleasePleaseReviewGateRefusesWhenTheCheckNeverAppears covers the
// reviewer being uninstalled, down, or slow to start: no terminal CodeRabbit
// result inside the cap is a refusal, not an implicit approval. This is the
// case a reader will ask about, so the summary has to say what it saw.
func TestReleasePleaseReviewGateRefusesWhenTheCheckNeverAppears(t *testing.T) {
	env := releaseReviewScenario(t, releaseReviewCheckRunsJSON(t,
		releaseReviewCheckRun{Name: "lint-test", Status: "completed", Conclusion: "success"},
		releaseReviewCheckRun{Name: "go-windows-test", Status: "completed", Conclusion: "success"},
	))
	output, summary, runErr := releaseReviewRunStep(t, env, "2", "1")
	if runErr == nil {
		t.Fatalf("%s a missing CodeRabbit check must not merge:\n%s", releaseReviewGatePrefix, output)
	}
	if strings.Contains(output, "pr merge 256") {
		t.Fatalf("%s a missing CodeRabbit check reached the merge:\n%s", releaseReviewGatePrefix, output)
	}
	releaseReviewRequireSummaryRow(t, summary, "| check status | missing |", "| conclusion | no terminal result |")
	if !strings.Contains(summary, "| merge | false |") {
		t.Fatalf("%s the job summary must record the refusal:\n%s", releaseReviewGatePrefix, summary)
	}
}

// TestReleasePleaseReviewGateAcceptsNeutralAndSkippedWithRecordedDetail keeps the
// gate from blocking releases for review outcomes that are not approvals but are
// also not defects — CodeRabbit reports "No files to review" as `skipped`
// (#216, check-run 2026-11-01T03:31:21Z) — while recording the detail so the
// waiver is auditable.
func TestReleasePleaseReviewGateAcceptsNeutralAndSkippedWithRecordedDetail(t *testing.T) {
	cases := []releaseReviewCheckRun{
		{Name: "CodeRabbit", Status: "completed", Conclusion: "skipped", Detail: "No files to review"},
		{Name: "CodeRabbit", Status: "completed", Conclusion: "neutral", Detail: "Review rate-limited; retry later"},
	}
	for _, run := range cases {
		run := run
		t.Run(run.Conclusion, func(t *testing.T) {
			env := releaseReviewScenario(t, releaseReviewCheckRunsJSON(t, run))
			output, summary, runErr := releaseReviewRunStep(t, env, "30", "1")
			if runErr != nil {
				t.Fatalf("%s a %q review must still release:\n%v\n%s", releaseReviewGatePrefix, run.Conclusion, runErr, output)
			}
			if !strings.Contains(output, "pr merge 256 --repo minekube/geyserlite --merge") {
				t.Fatalf("%s a %q review must merge the release PR:\n%s", releaseReviewGatePrefix, run.Conclusion, output)
			}
			releaseReviewRequireSummaryRow(t, summary, "| conclusion | "+run.Conclusion+" |", "| detail | "+run.Detail+" |")
			if !strings.Contains(summary, "| merge | true |") {
				t.Fatalf("%s the job summary must record the merge decision:\n%s", releaseReviewGatePrefix, summary)
			}
		})
	}
}

// TestReleasePleaseReviewGateIsBoundedByItsCap proves a hung reviewer cannot
// stall the release job indefinitely: the wait stops at the cap and refuses
// instead of merging. The shipped literals are rewritten to 2s / 1s here so the
// test costs seconds; the structural test below pins 900s / 15s.
func TestReleasePleaseReviewGateIsBoundedByItsCap(t *testing.T) {
	env := releaseReviewScenario(t, releaseReviewCheckRunsJSON(t,
		releaseReviewCheckRun{Name: "CodeRabbit", Status: "in_progress"},
	))
	output, summary, runErr := releaseReviewRunStep(t, env, "2", "1")
	if runErr == nil {
		t.Fatalf("%s a review that never finishes must not merge:\n%s", releaseReviewGatePrefix, output)
	}
	if strings.Contains(output, "pr merge 256") {
		t.Fatalf("%s a review that never finishes reached the merge:\n%s", releaseReviewGatePrefix, output)
	}
	if polls := releaseReviewPollCount(output); polls < 1 || polls > 3 {
		t.Fatalf("%s the wait must be bounded by the cap (2s / 1s => at most 3 polls), saw %d:\n%s",
			releaseReviewGatePrefix, polls, output)
	}
	releaseReviewRequireSummaryRow(t, summary, "| wait limit | 2s |", "| conclusion | no terminal result |")
	if !strings.Contains(summary, "| merge | false |") {
		t.Fatalf("%s the job summary must record the refusal:\\n%s", releaseReviewGatePrefix, summary)
	}
}

// TestReleasePleaseReviewGateBoundsAStalledRequest proves the cap is a
// wall-clock deadline and not a count of sleeps: a check-runs request that
// never answers must be cut off inside the budget instead of running past it
// (with the old shape the request had no timeout at all, so one hung call
// outlived the 900s cap entirely and the step still had no other ceiling).
func TestReleasePleaseReviewGateBoundsAStalledRequest(t *testing.T) {
	env := releaseReviewScenario(t, releaseReviewCheckRunsJSON(t,
		releaseReviewCheckRun{Name: "CodeRabbit", Status: "in_progress"},
	))
	// The stub hangs for 30s; the budget is 2s, so `timeout` must end the
	// request and the gate must finish seconds later, without merging.
	env["GH_REVIEW_STALL_SECONDS"] = "30"

	started := time.Now()
	output, summary, runErr := releaseReviewRunStep(t, env, "2", "1")
	elapsed := time.Since(started)

	if runErr == nil {
		t.Fatalf("%s a stalled review request must not merge:\\n%s", releaseReviewGatePrefix, output)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("%s a stalled request outlived the 2s budget by %.1fs; the request must be bounded by the deadline:\\n%s",
			releaseReviewGatePrefix, elapsed.Seconds(), output)
	}
	if strings.Contains(output, "pr merge 256") {
		t.Fatalf("%s a stalled review request reached the merge:\\n%s", releaseReviewGatePrefix, output)
	}
	if polls := releaseReviewPollCount(output); polls < 1 || polls > 2 {
		t.Fatalf("%s the stalled request must be abandoned inside the budget (2s / 1s => at most 2 attempts), saw %d:\\n%s",
			releaseReviewGatePrefix, polls, output)
	}
	// The refusal still has to be written: a killed request is a non-terminal
	// poll, not an abort.
	releaseReviewRequireSummaryRow(t, summary, "| field | value |", "| merge | false |")
	releaseReviewRequireSummaryRow(t, summary, "| detail | request failed |", "")
	if !strings.Contains(summary, "NOT merged") {
		t.Fatalf("%s the job summary must state that nothing was merged:\\n%s", releaseReviewGatePrefix, summary)
	}
}

// TestReleasePleaseReviewGateSurvivesAPersistentRequestFailure covers the other
// way the lookup fails on a runner: the API answers with an error every time.
// Under `set -e` the pre-fix assignment died on the first non-zero exit and the
// job went red with an empty summary, so the refusal — the only thing an
// operator can act on — was never written. The gate must treat the failure as a
// non-terminal poll, keep trying inside the budget, and then refuse through the
// normal path: summary rows, error annotation, no merge.
func TestReleasePleaseReviewGateSurvivesAPersistentRequestFailure(t *testing.T) {
	env := releaseReviewScenario(t, releaseReviewCheckRunsJSON(t,
		releaseReviewCheckRun{Name: "CodeRabbit", Status: "in_progress"},
	))
	env["GH_REVIEW_FAIL"] = "1"

	output, summary, runErr := releaseReviewRunStep(t, env, "3", "1")
	if runErr == nil {
		t.Fatalf("%s a review lookup that keeps failing must not merge:\\n%s", releaseReviewGatePrefix, output)
	}
	if strings.Contains(output, "pr merge 256") {
		t.Fatalf("%s a failing review lookup reached the merge:\\n%s", releaseReviewGatePrefix, output)
	}
	if polls := releaseReviewPollCount(output); polls < 2 || polls > 5 {
		t.Fatalf("%s a failing lookup must be retried inside the budget (3s / 1s => 2..5 attempts), saw %d:\\n%s",
			releaseReviewGatePrefix, polls, output)
	}
	if !strings.Contains(output, "::error::") || !strings.Contains(output, "CodeRabbit") {
		t.Fatalf("%s the refusal must surface as an error annotation even when every request failed:\\n%s",
			releaseReviewGatePrefix, output)
	}
	// Every row criterion 2 asks for, on the path where the step used to die.
	releaseReviewRequireSummaryRow(t, summary, "| check status |", "")
	releaseReviewRequireSummaryRow(t, summary, "| conclusion | no terminal result |", "| merge | false |")
	releaseReviewRequireSummaryRow(t, summary, "| detail | request failed |", "")
	if !strings.Contains(summary, "NOT merged") {
		t.Fatalf("%s the job summary must state that nothing was merged:\\n%s", releaseReviewGatePrefix, summary)
	}
}

// TestReleasePleaseReviewGateRefusesAMovedHead covers the review being for a
// different commit than the one that would merge: the release branch can be
// force-pushed while CodeRabbit runs, and merging then lands an unreviewed head.
func TestReleasePleaseReviewGateRefusesAMovedHead(t *testing.T) {
	env := releaseReviewScenario(t, releaseReviewCheckRunsJSON(t,
		releaseReviewCheckRun{Name: "CodeRabbit", Status: "completed", Conclusion: "success"},
	))
	env["GH_STUB_MERGED_HEAD_SHA"] = releaseReviewMovedHeadSHA
	output, summary, runErr := releaseReviewRunStep(t, env, "30", "1")
	if runErr == nil {
		t.Fatalf("%s a head that moved after the review must not merge:\n%s", releaseReviewGatePrefix, output)
	}
	if strings.Contains(output, "pr merge 256") {
		t.Fatalf("%s a moved head reached the merge:\n%s", releaseReviewGatePrefix, output)
	}
	if !strings.Contains(output, releaseReviewMovedHeadSHA) || !strings.Contains(output, releasePleaseStubHeadSHA) {
		t.Fatalf("%s the refusal must name both the reviewed and the new head:\n%s", releaseReviewGatePrefix, output)
	}
	if !strings.Contains(summary, "not merging an unreviewed commit") {
		t.Fatalf("%s the job summary must record the moved head:\n%s", releaseReviewGatePrefix, summary)
	}
}

// TestReleasePleaseReviewGatePinsTheFailClosedDisposition is the structural pin.
// It reads the commands the step's shell would actually execute (comments and
// line continuations resolved), so the gate cannot be reduced to a comment, an
// `echo`, or a check that accepts more than the three conclusions discussed
// above.
func TestReleasePleaseReviewGatePinsTheFailClosedDisposition(t *testing.T) {
	step := readReleasePleaseMergeStep(t, "..")
	commands := releasePleaseShellCommands(step.Run)

	indexOf := func(want string) int {
		for index, command := range commands {
			if command == want {
				return index
			}
		}
		t.Fatalf("%s the auto-merge step must run %q; it has:\n%s", releaseReviewGatePrefix, want, strings.Join(commands, "\n"))
		return -1
	}
	// indexOfContaining locates a command by a fragment, for the long
	// single-line requests that are not worth spelling out in full here (the
	// payload policy test pins those command-for-command).
	indexOfContaining := func(fragment string) int {
		for index, command := range commands {
			if strings.Contains(command, fragment) {
				return index
			}
		}
		t.Fatalf("%s the auto-merge step must run a command containing %q; it has:\n%s", releaseReviewGatePrefix, fragment, strings.Join(commands, "\n"))
		return -1
	}
	countOf := func(want string) int {
		count := 0
		for _, command := range commands {
			if command == want {
				count++
			}
		}
		return count
	}

	// The chosen cap and cadence, so "bounded" stays a number and not a hope,
	// plus the per-request bound that keeps one stalled call inside the cap.
	indexOf("REVIEW_WAIT_SECONDS=" + releaseReviewDefaultWaitSeconds)
	indexOf("REVIEW_POLL_SECONDS=" + releaseReviewDefaultPollSeconds)
	indexOf("REVIEW_REQUEST_SECONDS=" + releaseReviewDefaultRequestSeconds)

	// Fail-closed defaults, pinned by name so a Windows run (where the shell
	// harness skips) still catches them: nothing approves by default, the wait
	// is a wall-clock deadline that is re-checked before the request and again
	// before the trailing sleep, the sleep is clipped to what is left, the wait
	// only stops on a terminal review, and a missing conclusion is never
	// silently treated as an approval.
	indexOf(`REVIEW_APPROVED="false"`)
	indexOf(`REVIEW_DEADLINE=$(( $(date -u +%s) + REVIEW_WAIT_SECONDS ))`)
	indexOf(`REVIEW_TIMEOUT="$REVIEW_REQUEST_SECONDS"`)
	indexOf(`REVIEW_SLEEP="$REVIEW_POLL_SECONDS"`)
	indexOf(`if [ "$REVIEW_TIMEOUT" -gt "$REVIEW_REMAINING" ]; then`)
	indexOf(`if [ "$REVIEW_SLEEP" -gt "$REVIEW_REMAINING" ]; then`)
	indexOf(`if [ "$REVIEW_STATUS" = "completed" ]; then`)
	indexOf(`REVIEW_OUTCOME="no terminal result"`)
	if count := countOf(`REVIEW_REMAINING=$((REVIEW_DEADLINE - $(date -u +%s)))`); count != 2 {
		t.Fatalf("%s the deadline must be re-checked before the request and before the sleep, found %d re-checks", releaseReviewGatePrefix, count)
	}

	// A failed or timed-out request is a non-terminal poll with its own detail,
	// not an abort: that is what keeps the refusal summary unconditional.
	indexOf(`REVIEW_REASON="request failed"`)

	poll := indexOfContaining(`timeout "$REVIEW_TIMEOUT" gh api "repos/$GITHUB_REPOSITORY/commits/$HEAD_SHA/check-runs?per_page=100"`)
	refusal := indexOf(`if [ "$REVIEW_APPROVED" != "true" ]; then`)
	headRecheck := indexOf(`if [ "$MERGED_HEAD_SHA" != "$HEAD_SHA" ]; then`)
	merge := indexOf(`echo "Merging release PR #$PR_NUMBER"`)

	// The deadline is computed before the first request, so the bound exists
	// before there is anything to bound.
	deadline := indexOf(`REVIEW_DEADLINE=$(( $(date -u +%s) + REVIEW_WAIT_SECONDS ))`)
	if deadline >= poll {
		t.Fatalf("%s the wall-clock deadline must be computed before the first request (deadline=%d poll=%d)", releaseReviewGatePrefix, deadline, poll)
	}

	// The merge itself is bound to the reviewed commit. A re-check followed by an
	// unbound merge still lets the head move in the window between the two
	// calls; `--match-head-commit` makes GitHub reject that merge server-side.
	indexOf(`if gh pr merge "$PR_NUMBER" --repo "$GITHUB_REPOSITORY" --merge --match-head-commit "$HEAD_SHA"; then`)

	// Fail closed, and in this order: look at the review, refuse on anything
	// that is not an approval, re-check the reviewed commit, then merge.
	if poll >= refusal || refusal >= headRecheck || headRecheck >= merge {
		t.Fatalf("%s the gate must run before the merge (poll=%d refusal=%d head-recheck=%d merge=%d)",
			releaseReviewGatePrefix, poll, refusal, headRecheck, merge)
	}

	// The gate must sit after the required-check mirroring, so a refused review
	// still leaves a manually mergeable PR (branch protection satisfied).
	mirror := indexOf(`gh api "repos/$GITHUB_REPOSITORY/check-runs" -f name='lint-test' -f head_sha="$HEAD_SHA" -f status='completed' -f conclusion="$CHECK_CONCLUSION" -f details_url="$RUN_URL" -f output[title]="Release PR CI $CONCLUSION" -f output[summary]="Mirrors workflow_dispatch CI run $RUN_ID for release PR #$PR_NUMBER: $RUN_URL"`)
	if mirror > poll {
		t.Fatalf("%s the required-check mirror must stay before the review gate (mirror=%d poll=%d)",
			releaseReviewGatePrefix, mirror, poll)
	}

	// Exactly the three accepted conclusions, and nothing else.
	accepted := indexOf(`success | neutral | skipped)`)
	for _, command := range commands {
		for _, unsupported := range []string{"cancelled", "failure", "timed_out"} {
			if !strings.HasPrefix(command, "REVIEW_") && strings.Contains(command, unsupported) &&
				strings.Contains(command, "REVIEW_APPROVED") {
				t.Fatalf("%s %q must not widen the accepted conclusions", releaseReviewGatePrefix, command)
			}
		}
	}
	if accepted < 0 {
		t.Fatalf("%s the gate must accept exactly `success | neutral | skipped`", releaseReviewGatePrefix)
	}

	// The refusal must be a hard failure that stops the step, and the reason
	// must reach the job summary.
	indexOf(`exit 1`)
	if !strings.Contains(strings.Join(commands, "\n"), `printf '| detail | %s |\n' "$REVIEW_REASON" >> "$GITHUB_STEP_SUMMARY"`) {
		t.Fatalf("%s the job summary must record the review detail", releaseReviewGatePrefix)
	}

	// The payload boundary still holds: no expression may be rendered into the
	// script text (the auto-merge step is where the apostrophe defect lived).
	for _, command := range commands {
		if strings.Contains(command, "${") {
			t.Fatalf("%s the auto-merge step must not interpolate expressions into its shell script, found %q",
				releaseReviewGatePrefix, command)
		}
	}
}

// TestReleasePleaseReviewGatePolicyRejectsWeakeningMutations proves the gate
// tests are load-bearing: every candidate below keeps the workflow
// YAML-parseable, keeps the test suite compiling, and still must be rejected.
func TestReleasePleaseReviewGatePolicyRejectsWeakeningMutations(t *testing.T) {
	if os.Getenv(releasePleaseMutationChildEnv) == "1" {
		t.Skip("mutation child runs only the review gate tests")
	}
	// Half of the mutations below are only observable by executing the step's
	// shell, which the harness only does on a POSIX host with bash and jq (the
	// step itself runs on ubuntu-latest). The structural pin above keeps its
	// teeth on every platform; the execution half skips where it cannot run
	// rather than reporting a false "weakening survived".
	if runtime.GOOS == "windows" {
		t.Skip("the review gate's shell harness needs bash + jq; only the structural pin runs on Windows")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		if _, err := exec.LookPath("sh"); err != nil {
			t.Skipf("no bash/sh on PATH: %v", err)
		}
	}
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skipf("the review gate parses its payload with jq and it is not on PATH: %v", err)
	}

	baseline := releasePleaseWorkflowSource(t, "..")

	mutations := []releasePleaseWorkflowMutation{
		// The defect itself: no gate at all is the pre-fix workflow, and it is
		// the mutation "the merge happens while CodeRabbit is running" reduces
		// to. Approving everything is the same defect in disguise.
		replaceReleasePleaseWorkflow("approve-everything", `REVIEW_APPROVED="false"`, `REVIEW_APPROVED="true"`),
		// The conclusion #256 recorded — the one a "just merge it" change would
		// quietly add to the accepted set.
		replaceReleasePleaseWorkflow("accept-cancelled", `success | neutral | skipped)`, `success | neutral | skipped | cancelled)`),
		// Treating "no terminal result" (reviewer down, uninstalled, or still
		// running at the cap) as an approval.
		replaceReleasePleaseWorkflow("treat-timeout-as-approval", `REVIEW_OUTCOME="no terminal result"`, `REVIEW_OUTCOME="success"`),
		// Removing the refusal, which is what "fail open" looks like.
		replaceReleasePleaseWorkflow("refusal-removed", `if [ "$REVIEW_APPROVED" != "true" ]; then`, `if false; then`),
		// Not waiting at all: the deadline is already expired, so the gate
		// looks at nothing and refuses on a check it never saw.
		replaceReleasePleaseWorkflow("deadline-removed",
			`REVIEW_DEADLINE=$(( $(date -u +%s) + REVIEW_WAIT_SECONDS ))`,
			`REVIEW_DEADLINE=$(( $(date -u +%s) ))`),
		// A request that cannot outlive anything: without the per-request
		// bound one hung call sits outside the wall-clock cap entirely.
		replaceReleasePleaseWorkflow("request-timeout-removed",
			`timeout "$REVIEW_TIMEOUT" `, ``),
		// Treating a failed lookup as an approving one — the shortcut that
		// makes a transient API error merge an unreviewed release.
		replaceReleasePleaseWorkflow("request-failure-treated-as-approval",
			"              REVIEW_FAILURES=$((REVIEW_FAILURES + 1))\n              REVIEW_REASON=\"request failed\"",
			"              REVIEW_STATUS=\"completed\"\n              REVIEW_CONCLUSION=\"success\"\n              REVIEW_REASON=\"request failed\""),
		// Sleeping past the deadline the loop just checked.
		replaceReleasePleaseWorkflow("sleep-not-clipped",
			`if [ "$REVIEW_SLEEP" -gt "$REVIEW_REMAINING" ]; then`, `if false; then`),
		// Stopping at the first look even though the review is still running —
		// the exact ordering #256 shipped.
		replaceReleasePleaseWorkflow("break-on-running-review", `if [ "$REVIEW_STATUS" = "completed" ]; then`, `if true; then`),
		// Reviewing one commit and merging another.
		replaceReleasePleaseWorkflow("head-recheck-removed", `if [ "$MERGED_HEAD_SHA" != "$HEAD_SHA" ]; then`, `if false; then`),
		// Re-checking the head and then merging without binding the merge to it
		// leaves the same window open (CodeRabbit's finding on this PR).
		replaceReleasePleaseWorkflow("merge-not-bound-to-reviewed-sha",
			`--merge \
              --match-head-commit "$HEAD_SHA"; then`,
			`--merge; then`),
		// Accepting a skip without recording why.
		replaceReleasePleaseWorkflow("waiver-detail-dropped", `printf '| detail | %s |\n' "$REVIEW_REASON"`, `printf '| detail | %s |\n' "none"`),
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
			if bytes.Equal(candidate, baseline) {
				t.Fatalf("mutation %s is a no-op", mutation.id)
			}
			var parsed yaml.Node
			if err := yaml.Unmarshal(candidate, &parsed); err != nil {
				t.Fatalf("mutation is not YAML-parseable: %v", err)
			}

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

			cmd := exec.Command(executable, "-test.run=^TestReleasePleaseReviewGate", "-test.count=1")
			cmd.Dir = filepath.Join(tmp, "go")
			cmd.Env = append(os.Environ(), releasePleaseMutationChildEnv+"=1")
			output, runErr := cmd.CombinedOutput()
			if runErr == nil {
				t.Fatalf("weakening survived the review gate tests\n%s", output)
			}
			if !bytes.Contains(output, []byte(releaseReviewGatePrefix)) {
				t.Fatalf("weakening failed for the wrong reason; want a review-gate assertion\n%s", output)
			}
			t.Logf("MUTATION_REJECTED|%s|reason=release-please-review-gate", mutation.id)
		})
	}
}

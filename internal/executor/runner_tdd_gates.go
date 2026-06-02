package executor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ylcn91/pilot/internal/quality"
)

// TDD gate abort/fail reasons. These are surfaced as ExecutionResult.Error so the
// autopilot/feedback loop can distinguish a TDD-mode abort from an ordinary
// quality-gate failure.
const (
	// reasonTDDRedGateNotRed aborts the run when the TEST-AUTHOR's tests pass
	// (i.e. the RED gate is not red) even after a role retry. We refuse to fall
	// through to IMPLEMENTER because tests that already pass cannot drive TDD.
	reasonTDDRedGateNotRed = "tdd_red_gate_not_red"
	// reasonTDDGreenGateFailed fails the run when IMPLEMENTER cannot make the
	// authored tests pass within green_max_retries.
	reasonTDDGreenGateFailed = "tdd_green_gate_failed"
	// reasonTDDImplementerNoCommit fails the run when the GREEN gate passes but the
	// IMPLEMENTER never committed its implementation (the working tree is green but
	// no commit landed beyond the test-author baseline). Without this, the PR would
	// carry ONLY the test-author commit and the implementation would be lost; we
	// refuse to silently finalize a tests-only PR.
	reasonTDDImplementerNoCommit = "tdd_implementer_no_commit"
)

const (
	defaultTDDGreenMaxRetries = 2
	defaultTDDRoleMaxRetries  = 1
)

// TDDGateCheckerFactory builds a QualityChecker that runs a SINGLE test gate
// with the given command. It mirrors QualityCheckerFactory but adds the gate
// command so the RED/GREEN gates can scope to the newly authored test names.
//
// The factory seam keeps package quality out of runner.go and makes the gates
// unit-testable via a mock factory. When cmd/pilot does not wire one,
// defaultTDDGateCheckerFactory builds a simpleQualityChecker over a single test
// gate, so the executor remains self-sufficient.
type TDDGateCheckerFactory func(taskID, projectPath, gateCommand string) QualityChecker

// tddGateChecker returns the wired TDD gate checker factory, falling back to the
// in-package default that builds a single-test-gate simpleQualityChecker.
func (r *Runner) tddGateChecker() TDDGateCheckerFactory {
	if r.tddGateCheckerFactory != nil {
		return r.tddGateCheckerFactory
	}
	return defaultTDDGateCheckerFactory
}

// defaultTDDGateCheckerFactory builds a QualityChecker that runs exactly one
// required test gate with the supplied command. It reuses simpleQualityChecker
// (already in the executor package), so runner.go never imports package quality.
func defaultTDDGateCheckerFactory(taskID, projectPath, gateCommand string) QualityChecker {
	cfg := &quality.Config{
		Enabled: true,
		Gates: []*quality.Gate{
			{
				Name:        "tdd-test",
				Type:        quality.GateTest,
				Command:     gateCommand,
				Required:    true,
				Timeout:     5 * time.Minute,
				MaxRetries:  0, // gates re-run is driven by the role retry loop, not the gate
				FailureHint: "Make the authored tests pass without weakening them",
			},
		},
		OnFailure: quality.FailureConfig{Action: quality.ActionRetry},
	}
	return &simpleQualityChecker{config: cfg, projectPath: projectPath, taskID: taskID}
}

// tddGateCommand builds the scoped test command for the RED/GREEN gates.
//
// For Go projects, when scopeToNew is true and test names were parsed, it scopes
// to just those names via `go test -run '^(TestX|TestY)$' ./...` so the RED gate
// asserts ONLY the new tests fail (pre-existing failures elsewhere don't poison
// it). For every other language — or when no names are known — it falls back to
// the whole-suite command and logs that RED is suite-scoped (a pre-existing
// failing suite can make RED spuriously red).
func (r *Runner) tddGateCommand(projectPath string, testNames []string, scopeToNew bool) string {
	base := quality.DetectTestCommand(projectPath)
	isGo := tddFileExists(filepath.Join(projectPath, "go.mod"))
	if scopeToNew && isGo && len(testNames) > 0 {
		return fmt.Sprintf("go test -run '^(%s)$' ./...", strings.Join(testNames, "|"))
	}
	if scopeToNew && len(testNames) > 0 && !isGo {
		r.log.Warn("TDD RED gate is suite-scoped (non-Go project); a pre-existing failing suite can make RED spuriously red",
			"command", base)
	}
	return base
}

var tddTestNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// parseTestsAdded extracts test names from a TESTS_ADDED signal payload. The
// TEST-AUTHOR role is asked to emit a comma/newline/space separated list of the
// test function names it created (e.g. "TestFoo, TestBar"). Empty/invalid tokens
// are dropped; duplicates are de-duped preserving order.
func parseTestsAdded(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == ' ' || r == '\t' || r == '\r'
	})
	seen := make(map[string]struct{}, len(fields))
	var names []string
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" || !tddTestNameRe.MatchString(f) {
			continue
		}
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		names = append(names, f)
	}
	return names
}

// roleRerunFunc re-invokes a TDD role with optional feedback (the gate's failure
// text). It returns an error if the role invocation itself failed. The
// orchestrator supplies this; the gate loops below own the bounded retry/abort
// policy so the abort/fail reasons live in one place.
type roleRerunFunc func(ctx context.Context, feedback string) error

// enforceTDDRedGate asserts the TEST-AUTHOR's tests FAIL. If they already pass,
// the TEST-AUTHOR is re-run once (rerunTestAuthor) to author genuinely failing
// tests; if RED still is not red after that bounded retry, the run is ABORTED
// with reasonTDDRedGateNotRed (we do NOT fall through to IMPLEMENTER). roleMax
// bounds the TEST-AUTHOR retries (default defaultTDDRoleMaxRetries when <0).
func (r *Runner) enforceTDDRedGate(
	ctx context.Context,
	taskID, projectPath string,
	testNames []string,
	scopeToNew bool,
	roleMax int,
	rerunTestAuthor roleRerunFunc,
) error {
	if roleMax < 0 {
		roleMax = defaultTDDRoleMaxRetries
	}
	for attempt := 0; ; attempt++ {
		redOK, _, err := r.runTDDRedGate(ctx, taskID, projectPath, testNames, scopeToNew)
		if err != nil {
			return err
		}
		if redOK {
			return nil // tests fail as required — RED satisfied
		}
		if attempt >= roleMax {
			return fmt.Errorf("%s: authored tests pass before implementation (after %d test-author retr%s)",
				reasonTDDRedGateNotRed, roleMax, plural(roleMax))
		}
		if rerunTestAuthor == nil {
			return fmt.Errorf("%s: authored tests pass and no test-author rerun is available", reasonTDDRedGateNotRed)
		}
		if rErr := rerunTestAuthor(ctx, "Your tests passed before any implementation. Author tests that FAIL against current code, then re-emit TESTS_ADDED."); rErr != nil {
			return rErr
		}
	}
}

// enforceTDDGreenGate asserts the authored tests PASS. On failure it loops the
// IMPLEMENTER (rerunImplementer), feeding the gate's failure text back, bounded
// by greenMax (default defaultTDDGreenMaxRetries when <0). On exhaustion it fails
// the run with reasonTDDGreenGateFailed.
func (r *Runner) enforceTDDGreenGate(
	ctx context.Context,
	taskID, projectPath string,
	testNames []string,
	scopeToNew bool,
	greenMax int,
	rerunImplementer roleRerunFunc,
) error {
	if greenMax < 0 {
		greenMax = defaultTDDGreenMaxRetries
	}
	for attempt := 0; ; attempt++ {
		greenOK, feedback, err := r.runTDDGreenGate(ctx, taskID, projectPath, testNames, scopeToNew)
		if err != nil {
			return err
		}
		if greenOK {
			return nil // tests pass — GREEN satisfied
		}
		if attempt >= greenMax {
			return fmt.Errorf("%s: tests still failing after %d implementer retr%s", reasonTDDGreenGateFailed, greenMax, plural(greenMax))
		}
		if rerunImplementer == nil {
			return fmt.Errorf("%s: tests failing and no implementer rerun is available", reasonTDDGreenGateFailed)
		}
		if rErr := rerunImplementer(ctx, feedback); rErr != nil {
			return rErr
		}
	}
}

// plural returns "y"/"ies" suffix helper for "retr".
func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

// runTDDRedGate runs the scoped test gate once and asserts the authored tests
// FAIL (RED). It returns redOK=true when the named tests are PROVEN failing.
//
// For Go projects with known test names, the proof is per-test: `go test -json
// -count=1 -run '^(Name1|Name2)$'` is parsed so RED requires every named test to
// be present and FAILING (a compile error also counts — the new test references
// unimplemented symbols). An exit code alone is not accepted as proof. For non-Go
// projects, or when no test names are known, it falls back to the exit-code
// QualityChecker (suite-scope) and the gate is best-effort, not per-test proof.
func (r *Runner) runTDDRedGate(ctx context.Context, taskID, projectPath string, testNames []string, scopeToNew bool) (redOK bool, feedback string, err error) {
	if r.tddUsePerTestProof(projectPath, testNames, scopeToNew) {
		run, runErr := r.goTestRunner()(ctx, projectPath, testNames, tddGateTimeout)
		if runErr != nil {
			return false, "", fmt.Errorf("tdd red gate (go -json): %w", runErr)
		}
		v := evalGoTestVerdict(run, testNames, phaseRed)
		return v.OK, v.Feedback, nil
	}
	return r.runExitCodeGate(ctx, taskID, projectPath, testNames, scopeToNew, false)
}

// runTDDGreenGate runs the same scoped test gate once and asserts the authored
// tests PASS (GREEN). greenOK is true when the named tests are PROVEN passing.
//
// As with the RED gate, Go projects with known names get per-test proof via
// `go test -json -count=1`; every other case falls back to the exit-code
// QualityChecker (best-effort).
func (r *Runner) runTDDGreenGate(ctx context.Context, taskID, projectPath string, testNames []string, scopeToNew bool) (greenOK bool, feedback string, err error) {
	if r.tddUsePerTestProof(projectPath, testNames, scopeToNew) {
		run, runErr := r.goTestRunner()(ctx, projectPath, testNames, tddGateTimeout)
		if runErr != nil {
			return false, "", fmt.Errorf("tdd green gate (go -json): %w", runErr)
		}
		v := evalGoTestVerdict(run, testNames, phaseGreen)
		return v.OK, v.Feedback, nil
	}
	return r.runExitCodeGate(ctx, taskID, projectPath, testNames, scopeToNew, true)
}

// tddGateTimeout bounds each scoped go-test invocation for the RED/GREEN gates.
const tddGateTimeout = 5 * time.Minute

// tddUsePerTestProof reports whether the gate can produce per-test proof: a Go
// module, with scoping enabled and at least one parsed test name. Otherwise the
// gate falls back to the exit-code QualityChecker (best-effort, suite-scope).
func (r *Runner) tddUsePerTestProof(projectPath string, testNames []string, scopeToNew bool) bool {
	return scopeToNew && len(testNames) > 0 && goProjectAt(projectPath)
}

// runExitCodeGate is the non-Go / no-names fallback: it runs the whole-suite (or
// scoped) test command through the QualityChecker and uses ONLY the exit code.
// For the RED gate (wantPass=false) this is the legacy "did NOT pass" semantics;
// for GREEN (wantPass=true) it is "passed". It logs that the result is exit-code
// only, not per-test proof.
func (r *Runner) runExitCodeGate(ctx context.Context, taskID, projectPath string, testNames []string, scopeToNew, wantPass bool) (ok bool, feedback string, err error) {
	cmd := r.tddGateCommand(projectPath, testNames, scopeToNew)
	if cmd == "" {
		return false, "", fmt.Errorf("tdd gate: no test command detected for %s", projectPath)
	}
	r.log.Warn("TDD gate using exit-code suite-scope (no per-test proof; non-Go or no TESTS_ADDED names)",
		"command", cmd, "want_pass", wantPass)
	checker := r.tddGateChecker()(taskID, projectPath, cmd)
	outcome, cErr := checker.Check(ctx)
	if cErr != nil {
		return false, "", fmt.Errorf("tdd gate check: %w", cErr)
	}
	if wantPass {
		return outcome.Passed, outcome.RetryFeedback, nil
	}
	// RED fallback: satisfied when the gate did NOT pass.
	return !outcome.Passed, outcome.RetryFeedback, nil
}

// enforceTDDBaselineGreen is the BASELINE-GREEN precondition: BEFORE the
// TEST-AUTHOR runs, the scoped/whole suite must be GREEN so that a later RED is
// attributable to the newly authored tests (not a pre-existing failing suite).
//
// At baseline no TESTS_ADDED names exist yet, so the check is necessarily
// suite-scope. For Go it runs `go test -json -count=1 ./...` and requires zero
// failing tests; for non-Go it uses the exit-code QualityChecker. A RED baseline
// aborts the run with reasonTDDBaselineNotGreen (surfaced, never silent).
func (r *Runner) enforceTDDBaselineGreen(ctx context.Context, taskID, projectPath string) error {
	if goProjectAt(projectPath) {
		run, err := r.goTestRunner()(ctx, projectPath, nil, tddGateTimeout)
		if err != nil {
			return fmt.Errorf("tdd baseline (go -json): %w", err)
		}
		if failing := goTestFailures(run); len(failing) > 0 || run.CompileFailed {
			return fmt.Errorf("%s: suite is not green before TEST-AUTHOR (failing: %s) — a later RED could not be attributed to the new tests",
				reasonTDDBaselineNotGreen, baselineFailureDetail(run, failing))
		}
		return nil
	}
	cmd := quality.DetectTestCommand(projectPath)
	if cmd == "" {
		r.log.Warn("TDD baseline-green skipped: no test command detected (non-Go)", "project", projectPath)
		return nil
	}
	r.log.Warn("TDD baseline-green using exit-code suite-scope (non-Go; best-effort)", "command", cmd)
	checker := r.tddGateChecker()(taskID, projectPath, cmd)
	outcome, cErr := checker.Check(ctx)
	if cErr != nil {
		return fmt.Errorf("tdd baseline check: %w", cErr)
	}
	if !outcome.Passed {
		return fmt.Errorf("%s: suite is not green before TEST-AUTHOR — a later RED could not be attributed to the new tests", reasonTDDBaselineNotGreen)
	}
	return nil
}

// goTestFailures returns the sorted names of tests with a "fail" terminal action.
func goTestFailures(run *goTestRun) []string {
	if run == nil {
		return nil
	}
	var failing []string
	for name, res := range run.Results {
		if res.Action == "fail" {
			failing = append(failing, name)
		}
	}
	sort.Strings(failing)
	return failing
}

// baselineFailureDetail renders a compact failure summary for the baseline abort
// message — the failing test names, or a compile-error note when nothing built.
func baselineFailureDetail(run *goTestRun, failing []string) string {
	if len(failing) > 0 {
		return strings.Join(failing, ", ")
	}
	if run != nil && run.CompileFailed {
		return "suite did not compile"
	}
	return "unknown"
}

// tddFileExists reports whether a path exists. Named distinctly to avoid
// colliding with other unexported file-exists helpers in this package.
func tddFileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

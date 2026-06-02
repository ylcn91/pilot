package executor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
// FAIL (RED). It returns redOK=true when the gate did NOT pass (the desired RED
// state). A nil error with redOK=false means the tests already pass; a non-nil
// error means the gate could not be evaluated.
func (r *Runner) runTDDRedGate(ctx context.Context, taskID, projectPath string, testNames []string, scopeToNew bool) (redOK bool, feedback string, err error) {
	cmd := r.tddGateCommand(projectPath, testNames, scopeToNew)
	if cmd == "" {
		return false, "", fmt.Errorf("tdd red gate: no test command detected for %s", projectPath)
	}
	checker := r.tddGateChecker()(taskID, projectPath, cmd)
	outcome, cErr := checker.Check(ctx)
	if cErr != nil {
		return false, "", fmt.Errorf("tdd red gate check: %w", cErr)
	}
	// RED is satisfied when the gate did NOT pass.
	return !outcome.Passed, outcome.RetryFeedback, nil
}

// runTDDGreenGate runs the same scoped test gate once and asserts the authored
// tests PASS (GREEN). greenOK is true when the gate passed; feedback carries the
// gate's failure text for the IMPLEMENTER retry loop when it did not.
func (r *Runner) runTDDGreenGate(ctx context.Context, taskID, projectPath string, testNames []string, scopeToNew bool) (greenOK bool, feedback string, err error) {
	cmd := r.tddGateCommand(projectPath, testNames, scopeToNew)
	if cmd == "" {
		return false, "", fmt.Errorf("tdd green gate: no test command detected for %s", projectPath)
	}
	checker := r.tddGateChecker()(taskID, projectPath, cmd)
	outcome, cErr := checker.Check(ctx)
	if cErr != nil {
		return false, "", fmt.Errorf("tdd green gate check: %w", cErr)
	}
	return outcome.Passed, outcome.RetryFeedback, nil
}

// tddFileExists reports whether a path exists. Named distinctly to avoid
// colliding with other unexported file-exists helpers in this package.
func tddFileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

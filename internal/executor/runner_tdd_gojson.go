package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// goTestResult is the per-test outcome distilled from a `go test -json` stream.
// Action is one of "pass", "fail", or "skip" — the terminal action observed for
// a given test name. Elapsed is the test's wall time (seconds) as reported by go.
type goTestResult struct {
	Action  string
	Elapsed float64
}

// goTestRun is the aggregate outcome of one scoped `go test -json` invocation:
// the per-test map plus build/exec metadata. CompileFailed is set when the test
// binary failed to build (no per-test events were emitted), which is itself a
// legitimate RED signal (the new test references unimplemented symbols).
type goTestRun struct {
	// Results maps a fully-qualified test name to its terminal action.
	Results map[string]goTestResult
	// CompileFailed is true when `go test` could not build the package (a
	// compile error). For a RED gate this counts as the new tests "failing".
	CompileFailed bool
	// Raw is the trimmed combined output, used for retry feedback.
	Raw string
}

// goTestJSONEvent mirrors the subset of `go test -json` event fields we consume.
// The stream emits one JSON object per line; "Test" is absent on package-level
// events (which we ignore for per-test results).
type goTestJSONEvent struct {
	Action  string  `json:"Action"`
	Test    string  `json:"Test"`
	Elapsed float64 `json:"Elapsed"`
	Output  string  `json:"Output"`
}

// goTestJSONCommand builds the `go test -json -count=1` command scoped to the
// given test names via `-run '^(Name1|Name2)$'`. -count=1 defeats the Go test
// cache so RED/GREEN reflect the real run. With no names it runs the whole
// suite (still -json -count=1) so the caller can fall back to suite scope.
func goTestJSONCommand(testNames []string) []string {
	args := []string{"test", "-json", "-count=1"}
	if run := goTestRunExpr(testNames); run != "" {
		args = append(args, "-run", run)
	}
	args = append(args, "./...")
	return args
}

// goTestRunExpr renders the anchored alternation `^(Name1|Name2)$` for -run from
// the supplied test names. It returns "" when no names are given so callers can
// decide to fall back to a whole-suite run.
func goTestRunExpr(testNames []string) string {
	if len(testNames) == 0 {
		return ""
	}
	return fmt.Sprintf("^(%s)$", strings.Join(testNames, "|"))
}

// goTestRunnerFunc is the injectable seam for the per-test go-test -json runner.
// Production wires runGoTestJSON; tests inject a function that returns scripted
// *goTestRun values so the gate logic is exercised deterministically without a
// real toolchain. The signature mirrors runGoTestJSON.
type goTestRunnerFunc func(ctx context.Context, projectPath string, testNames []string, timeout time.Duration) (*goTestRun, error)

// goTestRunner returns the wired per-test go-test runner, falling back to the
// real runGoTestJSON when no seam is injected.
func (r *Runner) goTestRunner() goTestRunnerFunc {
	if r.tddGoTestRunner != nil {
		return r.tddGoTestRunner
	}
	return runGoTestJSON
}

// runGoTestJSON executes the scoped `go test -json -count=1` command in
// projectPath and parses the per-test results. A non-zero exit status is NOT an
// error here (failing tests exit non-zero by design); only an inability to start
// the process or a context cancellation yields a non-nil error. A compile
// failure (no per-test events, exit non-zero) sets CompileFailed.
func runGoTestJSON(ctx context.Context, projectPath string, testNames []string, timeout time.Duration) (*goTestRun, error) {
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "go", goTestJSONCommand(testNames)...)
	cmd.Dir = projectPath
	out, runErr := cmd.CombinedOutput()

	if ctxErr := runCtx.Err(); ctxErr != nil {
		return nil, fmt.Errorf("go test -json timed out after %s: %w", timeout, ctxErr)
	}

	run := parseGoTestJSON(out)
	// A non-zero exit with zero per-test results means the package did not even
	// build (compile error) or `go` itself failed to run. Treat a compile error
	// as a legitimate RED signal; surface a genuine exec failure as an error.
	if runErr != nil && len(run.Results) == 0 {
		if isExecStartFailure(runErr) {
			return nil, fmt.Errorf("go test -json failed to run: %w\n%s", runErr, run.Raw)
		}
		// "matched no packages" / "no packages to test" is a benign empty suite
		// (exit 1, no JSON events), NOT a build failure — leave CompileFailed false
		// so an empty module reads as a green/empty run, not a spurious RED.
		if !isNoPackagesOutput(run.Raw) {
			run.CompileFailed = true
		}
	}
	return run, nil
}

// isNoPackagesOutput reports whether the go-test output is the benign
// "no packages to test" / "matched no packages" condition (an empty module),
// which must not be mistaken for a compile/build failure.
func isNoPackagesOutput(raw string) bool {
	return strings.Contains(raw, "no packages to test") ||
		strings.Contains(raw, "matched no packages")
}

// isExecStartFailure reports whether the error from CombinedOutput indicates the
// process could not be started/located (vs. an ordinary non-zero test exit). An
// *exec.ExitError means the process ran and exited non-zero — the normal "tests
// failed / compile error" case, NOT a start failure.
func isExecStartFailure(err error) bool {
	var exitErr *exec.ExitError
	return !errors.As(err, &exitErr)
}

// parseGoTestJSON consumes a `go test -json` stream (one JSON object per line)
// and reduces it to a per-test terminal-action map. Lines that are not valid
// JSON events (e.g. a leading build error printed before the JSON stream) are
// skipped for the map but retained in Raw. The LAST terminal action seen for a
// test wins (a test can emit "run" then "pass"); only pass/fail/skip are stored.
func parseGoTestJSON(output []byte) *goTestRun {
	run := &goTestRun{Results: map[string]goTestResult{}, Raw: strings.TrimSpace(string(output))}
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line[0] != '{' {
			continue
		}
		var ev goTestJSONEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev.Test == "" {
			continue
		}
		switch ev.Action {
		case "pass", "fail", "skip":
			run.Results[ev.Test] = goTestResult{Action: ev.Action, Elapsed: ev.Elapsed}
		}
	}
	return run
}

// goProjectAt reports whether projectPath is a Go module (has a go.mod).
func goProjectAt(projectPath string) bool {
	return tddFileExists(filepath.Join(projectPath, "go.mod"))
}

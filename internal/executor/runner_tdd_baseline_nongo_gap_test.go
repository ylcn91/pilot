package executor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// nonGoProjectDir creates a temp project that is NOT a Go module (no go.mod) but
// has a detectable test command (package.json -> "npm test"). This forces
// enforceTDDBaselineGreen down the non-Go exit-code fallback leg.
func nonGoProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"x","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	return dir
}

// emptyNonGoDir creates a temp project that is neither a Go module nor has any
// detectable test command, exercising the "no test command" skip branch.
func emptyNonGoDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// A lone README is not a Go/Python/Node/Rust project marker, and no Makefile
	// test target exists, so DetectTestCommand returns "".
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# notes\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	return dir
}

// TestEnforceBaselineGreen_NonGoPasses verifies the non-Go exit-code fallback:
// when DetectTestCommand finds a runner and the wired TDD gate checker reports a
// green suite, enforceTDDBaselineGreen returns nil (proceed to TEST-AUTHOR).
func TestEnforceBaselineGreen_NonGoPasses(t *testing.T) {
	dir := nonGoProjectDir(t)
	r := NewRunner()

	var gotCmd string
	r.SetTDDGateCheckerFactory(func(_, _, gateCommand string) QualityChecker {
		gotCmd = gateCommand
		return &mockQualityChecker{outcome: &QualityOutcome{Passed: true}}
	})

	if err := r.enforceTDDBaselineGreen(context.Background(), "t-nongo-green", dir); err != nil {
		t.Fatalf("non-Go green baseline should pass, got: %v", err)
	}
	if gotCmd != "npm test" {
		t.Errorf("gate command = %q, want %q (DetectTestCommand for package.json)", gotCmd, "npm test")
	}
}

// TestEnforceBaselineGreen_NonGoAbortsOnRed verifies the non-Go exit-code fallback
// aborts with reasonTDDBaselineNotGreen when the checker reports a failing suite.
func TestEnforceBaselineGreen_NonGoAbortsOnRed(t *testing.T) {
	dir := nonGoProjectDir(t)
	r := NewRunner()

	r.SetTDDGateCheckerFactory(func(_, _, _ string) QualityChecker {
		return &mockQualityChecker{outcome: &QualityOutcome{Passed: false}}
	})

	err := r.enforceTDDBaselineGreen(context.Background(), "t-nongo-red", dir)
	if err == nil || !strings.Contains(err.Error(), reasonTDDBaselineNotGreen) {
		t.Fatalf("non-Go red baseline = %v, want error containing %q", err, reasonTDDBaselineNotGreen)
	}
}

// TestEnforceBaselineGreen_NonGoSkipsWhenNoTestCommand verifies the non-Go path
// is best-effort: with no detectable test runner, the baseline check is skipped
// (returns nil) and the gate checker factory is never invoked.
func TestEnforceBaselineGreen_NonGoSkipsWhenNoTestCommand(t *testing.T) {
	dir := emptyNonGoDir(t)
	r := NewRunner()

	called := false
	r.SetTDDGateCheckerFactory(func(_, _, _ string) QualityChecker {
		called = true
		return &mockQualityChecker{outcome: &QualityOutcome{Passed: false}}
	})

	if err := r.enforceTDDBaselineGreen(context.Background(), "t-nongo-skip", dir); err != nil {
		t.Fatalf("no test command should skip baseline (return nil), got: %v", err)
	}
	if called {
		t.Error("gate checker factory should not be invoked when no test command is detected")
	}
}

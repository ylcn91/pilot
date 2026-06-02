package executor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestEnforceBaselineGreenAbortsOnRedSuite verifies the BASELINE-GREEN
// precondition: when the suite is already RED before the TEST-AUTHOR runs, the
// gate aborts with reasonTDDBaselineNotGreen (a later RED could not be
// attributed to the new tests).
func TestEnforceBaselineGreenAbortsOnRedSuite(t *testing.T) {
	dir := goProjectDir(t)
	r := NewRunner()
	// Baseline probe uses nil testNames; report a pre-existing failing test.
	r.tddGoTestRunner = func(_ context.Context, _ string, _ []string, _ time.Duration) (*goTestRun, error) {
		return &goTestRun{Results: map[string]goTestResult{"TestPreexisting": {Action: "fail"}}}, nil
	}
	err := r.enforceTDDBaselineGreen(context.Background(), "t1", dir)
	if err == nil || !strings.Contains(err.Error(), reasonTDDBaselineNotGreen) {
		t.Fatalf("baseline = %v, want %s", err, reasonTDDBaselineNotGreen)
	}
	if !strings.Contains(err.Error(), "TestPreexisting") {
		t.Errorf("abort should name the failing test: %v", err)
	}
}

// TestEnforceBaselineGreenPassesOnGreenSuite verifies a green baseline proceeds.
func TestEnforceBaselineGreenPassesOnGreenSuite(t *testing.T) {
	dir := goProjectDir(t)
	r := NewRunner()
	r.tddGoTestRunner = func(_ context.Context, _ string, _ []string, _ time.Duration) (*goTestRun, error) {
		return &goTestRun{Results: map[string]goTestResult{"TestExisting": {Action: "pass"}}}, nil
	}
	if err := r.enforceTDDBaselineGreen(context.Background(), "t1", dir); err != nil {
		t.Fatalf("green baseline should pass: %v", err)
	}
}

// TestEnforceBaselineGreenAbortsOnCompileFailure verifies a non-compiling suite
// at baseline aborts (a broken suite can't anchor a later RED).
func TestEnforceBaselineGreenAbortsOnCompileFailure(t *testing.T) {
	dir := goProjectDir(t)
	r := NewRunner()
	r.tddGoTestRunner = func(_ context.Context, _ string, _ []string, _ time.Duration) (*goTestRun, error) {
		return &goTestRun{Results: map[string]goTestResult{}, CompileFailed: true}, nil
	}
	err := r.enforceTDDBaselineGreen(context.Background(), "t1", dir)
	if err == nil || !strings.Contains(err.Error(), reasonTDDBaselineNotGreen) {
		t.Fatalf("baseline compile-fail = %v, want %s", err, reasonTDDBaselineNotGreen)
	}
	if !strings.Contains(err.Error(), "did not compile") {
		t.Errorf("abort should mention compile failure: %v", err)
	}
}

// TestRunTDDSequenceTestFreezeFires drives the full sequence where the IMPLEMENTER
// rewrites the test file the TEST-AUTHOR committed. The TEST-FREEZE guard must
// abort with reasonTDDTestsModified rather than accepting the weakened tests.
func TestRunTDDSequenceTestFreezeFires(t *testing.T) {
	dir, defaultBranch := tddGitRepo(t)
	order := &[]string{}

	r, s, _ := newTDDRunner(t, dir, defaultBranch, order, []*QualityOutcome{fail2(), pass()})
	r.architectBackend = &tddRoleBackend{name: "arch", prompts: order, role: "architect"}
	r.testAuthorBackend = &tddRoleBackend{
		name: "ta", prompts: order, role: "test-author",
		output: "TESTS_ADDED: TestAdd",
		action: commitFile(t, dir, "add_test.go", "package tddtest\n// original red test\n", "test: add red"),
	}
	// Implementer rewrites the frozen test file (cheats) and commits.
	r.implementerBackend = &tddRoleBackend{
		name: "impl", prompts: order, role: "implementer",
		output: "IMPLEMENTED",
		action: func() {
			// Find the committed test file (commitFile prefixes a counter).
			var testFile string
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), "add_test.go") {
					testFile = e.Name()
				}
			}
			if testFile == "" {
				t.Fatal("test file not found for implementer cheat")
			}
			if err := os.WriteFile(filepath.Join(dir, testFile), []byte("package tddtest\n// WEAKENED\n"), 0o644); err != nil {
				t.Fatalf("weaken test: %v", err)
			}
			for _, args := range [][]string{{"add", "."}, {"commit", "-m", "feat: cheat"}} {
				cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v\n%s", args, err, out)
				}
			}
		},
	}

	_, err := r.runTDDSequence(s)
	if err == nil || !strings.Contains(err.Error(), reasonTDDTestsModified) {
		t.Fatalf("expected %s when implementer rewrites the red test, got %v", reasonTDDTestsModified, err)
	}
}

// TestRunTDDSequenceTestFreezePassesWhenUntouched confirms an honest IMPLEMENTER
// (touches only production files) passes the TEST-FREEZE guard.
func TestRunTDDSequenceTestFreezePassesWhenUntouched(t *testing.T) {
	dir, defaultBranch := tddGitRepo(t)
	order := &[]string{}

	r, s, _ := newTDDRunner(t, dir, defaultBranch, order, []*QualityOutcome{fail2(), pass()})
	r.architectBackend = &tddRoleBackend{name: "arch", prompts: order, role: "architect"}
	r.testAuthorBackend = &tddRoleBackend{
		name: "ta", prompts: order, role: "test-author",
		output: "TESTS_ADDED: TestAdd",
		action: commitFile(t, dir, "add_test.go", "package tddtest\n", "test: add red"),
	}
	r.implementerBackend = &tddRoleBackend{
		name: "impl", prompts: order, role: "implementer",
		output: "IMPLEMENTED",
		action: commitFile(t, dir, "add.go", "package tddtest\n", "feat: implement"),
	}

	res, err := r.runTDDSequence(s)
	if err != nil {
		t.Fatalf("honest implementer should pass freeze: %v", err)
	}
	if res == nil || res.Output != "IMPLEMENTED" {
		t.Fatalf("expected implementer result, got %+v", res)
	}
}

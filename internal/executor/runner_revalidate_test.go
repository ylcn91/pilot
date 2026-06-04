package executor

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// TestRevalidateAfterReview guards the post-gate finality fix: if self-review /
// intent-retry commits new code after the quality gate passed, the gate must
// re-run and fail the task when the new code breaks it.
func TestRevalidateAfterReview(t *testing.T) {
	newState := func(dir string, passed bool) (*Runner, *executeState) {
		runner := NewRunnerWithBackend(&mockSelfReviewBackend{output: "x"})
		runner.SetQualityCheckerFactory(func(_, _ string) QualityChecker {
			return &mockQualityChecker{outcome: &QualityOutcome{Passed: passed}}
		})
		s := &executeState{
			task:               &Task{ID: "RV", ProjectPath: dir},
			ctx:                context.Background(),
			git:                NewGitOperations(dir),
			executionPath:      dir,
			result:             &ExecutionResult{Success: true},
			state:              &progressState{},
			qualityGatesPassed: true,
			log:                slog.Default(),
		}
		return runner, s
	}

	t.Run("fails when a post-review commit breaks the gate", func(t *testing.T) {
		dir := t.TempDir()
		headBefore := initGitRepoWithCommit(t, dir, "package main\n")

		// Simulate a self-review fix committing new code after the gate passed.
		if err := os.WriteFile(filepath.Join(dir, "extra.go"), []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "add", ".")
		runGit(t, dir, "commit", "-m", "review fix")

		runner, s := newState(dir, false) // gate now fails
		res, err := runner.revalidateAfterReview(s, headBefore)
		if err != nil {
			t.Fatalf("revalidateAfterReview: %v", err)
		}
		if res == nil || res.Success {
			t.Errorf("expected a failure result, got %+v", res)
		}
	})

	t.Run("no-op when HEAD is unchanged", func(t *testing.T) {
		dir := t.TempDir()
		head := initGitRepoWithCommit(t, dir, "package main\n")

		runner, s := newState(dir, false) // would fail if it ran
		res, err := runner.revalidateAfterReview(s, head)
		if err != nil || res != nil {
			t.Errorf("expected no-op, got res=%v err=%v", res, err)
		}
		if !s.result.Success {
			t.Error("result should stay successful when HEAD is unchanged")
		}
	})
}

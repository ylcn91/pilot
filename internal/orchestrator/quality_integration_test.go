package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/executor"
)

// TestQualityGatesHappyPath tests that quality gates pass and task completes successfully
func TestQualityGatesHappyPath(t *testing.T) {
	// Setup mock backend that returns success
	// With self-review enabled (GH-364), backend is called twice:
	// 1. Initial execution
	// 2. Self-review phase (after quality gates pass)
	backend := &mockBackend{
		name: "test-backend",
		execResults: []*executor.BackendResult{
			{
				Success: true,
				Output:  "Task completed",
			},
			{
				Success: true,
				Output:  "REVIEW_PASSED",
			},
		},
	}

	// Setup mock quality checker that passes
	qualityChecker := &mockQualityChecker{
		outcomes: []*executor.QualityOutcome{
			{
				Passed:      true,
				ShouldRetry: false,
				Attempt:     0,
			},
		},
	}

	// Create runner with mock backend
	runner := executor.NewRunnerWithBackend(backend)
	runner.SetRecordingEnabled(false)   // Disable recording for tests
	runner.SetSkipPreflightChecks(true) // Skip preflight checks (no Claude CLI in CI)

	// Set quality checker factory
	runner.SetQualityCheckerFactory(func(taskID, projectPath string) executor.QualityChecker {
		return qualityChecker
	})

	// Create task with a proper git repo so pre-flight checks pass
	task := &executor.Task{
		ID:          "TEST-001",
		Title:       "Test happy path",
		Description: "Test that quality gates pass",
		ProjectPath: setupTestGitRepo(t),
	}

	// Execute task
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := runner.Execute(ctx, task)
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}

	// Verify task succeeded
	if !result.Success {
		t.Errorf("Expected task to succeed, got failure: %s", result.Error)
	}

	// Verify quality checker was called exactly once
	if callCount := qualityChecker.callCount(); callCount != 1 {
		t.Errorf("Expected quality checker to be called 1 time, got %d", callCount)
	}

	// Verify backend was called twice (initial + self-review, GH-364)
	if execCount := backend.execCount(); execCount != 2 {
		t.Errorf("Expected backend to be called 2 times (initial + self-review), got %d", execCount)
	}
}

// TestQualityGatesRetrySuccess tests that failing quality gates trigger retry and succeed
func TestQualityGatesRetrySuccess(t *testing.T) {
	// Setup mock backend:
	// With self-review enabled (GH-364), backend is called 3 times:
	// 1. Initial execution
	// 2. Retry execution (after quality gate fails)
	// 3. Self-review phase (after quality gates pass on retry)
	backend := &mockBackend{
		name: "test-backend",
		execResults: []*executor.BackendResult{
			{
				Success: true,
				Output:  "Initial implementation",
			},
			{
				Success: true,
				Output:  "Fixed implementation",
			},
			{
				Success: true,
				Output:  "REVIEW_PASSED",
			},
		},
	}

	// Setup mock quality checker:
	// - First call fails with retry
	// - Second call passes
	qualityChecker := &mockQualityChecker{
		outcomes: []*executor.QualityOutcome{
			{
				Passed:        false,
				ShouldRetry:   true,
				RetryFeedback: "Test failed: expected 2 got 1",
				Attempt:       0,
			},
			{
				Passed:      true,
				ShouldRetry: false,
				Attempt:     1,
			},
		},
	}

	// Create runner with mock backend
	runner := executor.NewRunnerWithBackend(backend)
	runner.SetRecordingEnabled(false)
	runner.SetSkipPreflightChecks(true) // Skip preflight checks (no Claude CLI in CI)

	runner.SetQualityCheckerFactory(func(taskID, projectPath string) executor.QualityChecker {
		return qualityChecker
	})

	task := &executor.Task{
		ID:          "TEST-002",
		Title:       "Test retry success",
		Description: "Test that retry fixes quality gate failure",
		ProjectPath: setupTestGitRepo(t),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := runner.Execute(ctx, task)
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}

	// Verify task succeeded after retry
	if !result.Success {
		t.Errorf("Expected task to succeed after retry, got failure: %s", result.Error)
	}

	// Verify quality checker was called twice (initial + after retry)
	if callCount := qualityChecker.callCount(); callCount != 2 {
		t.Errorf("Expected quality checker to be called 2 times, got %d", callCount)
	}

	// Verify backend was called 3 times (initial + retry + self-review, GH-364)
	if execCount := backend.execCount(); execCount != 3 {
		t.Errorf("Expected backend to be called 3 times (initial + retry + self-review), got %d", execCount)
	}
}

// TestQualityGatesMaxRetriesExhausted tests that task fails after max retries
func TestQualityGatesMaxRetriesExhausted(t *testing.T) {
	// Setup mock backend - all calls return success
	backend := &mockBackend{
		name: "test-backend",
		execResults: []*executor.BackendResult{
			{Success: true, Output: "Attempt 1"},
			{Success: true, Output: "Attempt 2"},
			{Success: true, Output: "Attempt 3"},
		},
	}

	// Setup mock quality checker - always fails with retry enabled
	// After maxAutoRetries (2), the runner should stop
	qualityChecker := &mockQualityChecker{
		outcomes: []*executor.QualityOutcome{
			{Passed: false, ShouldRetry: true, RetryFeedback: "Error 1", Attempt: 0},
			{Passed: false, ShouldRetry: true, RetryFeedback: "Error 2", Attempt: 1},
			{Passed: false, ShouldRetry: true, RetryFeedback: "Error 3", Attempt: 2},
		},
	}

	runner := executor.NewRunnerWithBackend(backend)
	runner.SetRecordingEnabled(false)
	runner.SetSkipPreflightChecks(true) // Skip preflight checks (no Claude CLI in CI)

	runner.SetQualityCheckerFactory(func(taskID, projectPath string) executor.QualityChecker {
		return qualityChecker
	})

	task := &executor.Task{
		ID:          "TEST-003",
		Title:       "Test max retries",
		Description: "Test that task fails after max retries",
		ProjectPath: setupTestGitRepo(t),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := runner.Execute(ctx, task)
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}

	// Verify task failed
	if result.Success {
		t.Error("Expected task to fail after max retries, but it succeeded")
	}

	// Verify error mentions retries
	if result.Error == "" {
		t.Error("Expected error message to be set")
	}

	// Quality checker should be called 3 times:
	// 1. After initial execution (fails)
	// 2. After retry 1 (fails)
	// 3. After retry 2 (fails, max reached)
	if callCount := qualityChecker.callCount(); callCount != 3 {
		t.Errorf("Expected quality checker to be called 3 times, got %d", callCount)
	}

	// Backend should be called 3 times:
	// 1. Initial execution
	// 2. Retry 1
	// 3. Retry 2
	if execCount := backend.execCount(); execCount != 3 {
		t.Errorf("Expected backend to be called 3 times, got %d", execCount)
	}
}

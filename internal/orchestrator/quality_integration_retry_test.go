package orchestrator

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/executor"
)

// TestQualityGatesDisabled tests that gates are skipped when factory is not set
func TestQualityGatesDisabled(t *testing.T) {
	// Setup mock backend
	backend := &mockBackend{
		name: "test-backend",
		execResults: []*executor.BackendResult{
			{
				Success: true,
				Output:  "Task completed without quality gates",
			},
		},
	}

	// Create runner WITHOUT setting quality checker factory
	runner := executor.NewRunnerWithBackend(backend)
	runner.SetRecordingEnabled(false)
	runner.SetSkipPreflightChecks(true) // Skip preflight checks (no Claude CLI in CI)

	// No quality checker factory set - gates should be skipped

	task := &executor.Task{
		ID:          "TEST-004",
		Title:       "Test disabled gates",
		Description: "Test that quality gates are skipped when disabled",
		ProjectPath: setupTestGitRepo(t),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := runner.Execute(ctx, task)
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}

	// Verify task succeeded (no quality gates to fail)
	if !result.Success {
		t.Errorf("Expected task to succeed with disabled gates, got failure: %s", result.Error)
	}

	// Verify backend was called exactly once
	if execCount := backend.execCount(); execCount != 1 {
		t.Errorf("Expected backend to be called 1 time, got %d", execCount)
	}
}

// TestQualityGatesNoRetryOnNoShouldRetry tests that when ShouldRetry is false, no retry happens
func TestQualityGatesNoRetryOnNoShouldRetry(t *testing.T) {
	backend := &mockBackend{
		name: "test-backend",
		execResults: []*executor.BackendResult{
			{Success: true, Output: "Completed"},
		},
	}

	// Quality checker fails but indicates no retry should happen
	qualityChecker := &mockQualityChecker{
		outcomes: []*executor.QualityOutcome{
			{
				Passed:      false,
				ShouldRetry: false, // No retry
				Attempt:     0,
			},
		},
	}

	runner := executor.NewRunnerWithBackend(backend)
	runner.SetRecordingEnabled(false)
	runner.SetSkipPreflightChecks(true) // Skip preflight checks (no Claude CLI in CI)

	runner.SetQualityCheckerFactory(func(taskID, projectPath string) executor.QualityChecker {
		return qualityChecker
	})

	task := &executor.Task{
		ID:          "TEST-005",
		Title:       "Test no retry when ShouldRetry=false",
		Description: "Test that task fails immediately when ShouldRetry is false",
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
		t.Error("Expected task to fail when quality gates fail with ShouldRetry=false")
	}

	// Quality checker called only once - no retry
	if callCount := qualityChecker.callCount(); callCount != 1 {
		t.Errorf("Expected quality checker to be called 1 time, got %d", callCount)
	}

	// Backend called only once - no retry
	if execCount := backend.execCount(); execCount != 1 {
		t.Errorf("Expected backend to be called 1 time, got %d", execCount)
	}
}

// TestQualityGatesOrchestratorWiring tests that orchestrator properly wires quality checker to runner
func TestQualityGatesOrchestratorWiring(t *testing.T) {
	// Create orchestrator
	cfg := &Config{
		MaxConcurrent: 1,
	}
	orch, err := NewOrchestrator(cfg, nil)
	if err != nil {
		t.Fatalf("Failed to create orchestrator: %v", err)
	}
	defer orch.Stop()

	// Track whether factory was called
	var factoryCalled bool
	var passedTaskID, passedProjectPath string

	// Set quality checker factory on orchestrator
	orch.SetQualityCheckerFactory(func(taskID, projectPath string) executor.QualityChecker {
		factoryCalled = true
		passedTaskID = taskID
		passedProjectPath = projectPath
		return &mockQualityChecker{
			outcomes: []*executor.QualityOutcome{
				{Passed: true},
			},
		}
	})

	// Verify internal runner has the factory set
	// We can test this indirectly by verifying the orchestrator's qualityCheckerFactory field is set
	if orch.qualityCheckerFactory == nil {
		t.Error("Expected orchestrator's qualityCheckerFactory to be set")
	}

	// Test that factory produces valid checker
	checker := orch.qualityCheckerFactory("TEST-TASK", "/test/path")
	if checker == nil {
		t.Error("Expected factory to produce a checker")
	}

	if !factoryCalled {
		t.Error("Expected factory to be called")
	}
	if passedTaskID != "TEST-TASK" {
		t.Errorf("Expected taskID 'TEST-TASK', got '%s'", passedTaskID)
	}
	if passedProjectPath != "/test/path" {
		t.Errorf("Expected projectPath '/test/path', got '%s'", passedProjectPath)
	}

	// Verify checker works
	outcome, err := checker.Check(context.Background())
	if err != nil {
		t.Errorf("Checker returned error: %v", err)
	}
	if !outcome.Passed {
		t.Error("Expected checker to pass")
	}
}

// TestQualityGatesRetryFeedbackPropagation tests that retry feedback is properly passed to backend
func TestQualityGatesRetryFeedbackPropagation(t *testing.T) {
	// Backend that captures prompts via the mockBackend struct
	// With self-review enabled (GH-364), backend is called 3 times:
	// 1. Initial execution
	// 2. Retry execution
	// 3. Self-review phase
	backend := &mockBackend{
		name: "test-backend",
		execResults: []*executor.BackendResult{
			{Success: true, Output: "Initial"},
			{Success: true, Output: "After retry"},
			{Success: true, Output: "REVIEW_PASSED"},
		},
	}

	qualityChecker := &mockQualityChecker{
		outcomes: []*executor.QualityOutcome{
			{
				Passed:        false,
				ShouldRetry:   true,
				RetryFeedback: "### Test Failure\n\nExpected: 42\nActual: 0",
				Attempt:       0,
			},
			{Passed: true},
		},
	}

	runner := executor.NewRunnerWithBackend(backend)
	runner.SetRecordingEnabled(false)
	runner.SetSkipPreflightChecks(true) // Skip preflight checks (no Claude CLI in CI)

	runner.SetQualityCheckerFactory(func(taskID, projectPath string) executor.QualityChecker {
		return qualityChecker
	})

	task := &executor.Task{
		ID:          "TEST-006",
		Title:       "Test retry feedback",
		Description: "Original task description",
		ProjectPath: setupTestGitRepo(t),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := runner.Execute(ctx, task)
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected task to succeed, got failure: %s", result.Error)
	}

	// Get captured prompts
	receivedPrompts := backend.getPrompts()

	// Should have 3 prompts (initial + retry + self-review, GH-364)
	if len(receivedPrompts) != 3 {
		t.Fatalf("Expected 3 prompts (initial + retry + self-review), got %d", len(receivedPrompts))
	}

	// First prompt should be original task
	if receivedPrompts[0] == "" {
		t.Error("First prompt should not be empty")
	}

	// Second prompt (retry) should contain the feedback
	retryPrompt := receivedPrompts[1]
	if retryPrompt == "" {
		t.Error("Retry prompt should not be empty")
	}

	// Verify retry prompt contains Quality Gate Retry header and feedback
	if !strings.Contains(retryPrompt, "Quality Gate Retry") {
		t.Error("Retry prompt should contain 'Quality Gate Retry'")
	}
	if !strings.Contains(retryPrompt, "Expected: 42") {
		t.Error("Retry prompt should contain the error feedback")
	}

	// Third prompt should be self-review
	selfReviewPrompt := receivedPrompts[2]
	if !strings.Contains(selfReviewPrompt, "Self-Review Phase") {
		t.Error("Third prompt should be self-review prompt")
	}
}

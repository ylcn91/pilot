package executor

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// GH-539: Test per-task budget limit enforcement in processBackendEvent
func TestProcessBackendEvent_TokenLimitExceeded(t *testing.T) {
	runner := NewRunner()

	cancelCalled := false
	state := &progressState{
		phase:        "Starting",
		budgetCancel: func() { cancelCalled = true },
	}

	// Set a token limit callback that triggers on > 1000 total tokens
	var totalTokens int64
	runner.SetTokenLimitCheck(func(taskID string, deltaInput, deltaOutput int64) bool {
		totalTokens += deltaInput + deltaOutput
		return totalTokens <= 1000
	})

	// First event: 500 tokens — should be allowed
	runner.processBackendEvent("TASK-1", BackendEvent{
		Type:         EventTypeText,
		TokensInput:  300,
		TokensOutput: 200,
	}, state)

	if state.budgetExceeded {
		t.Error("budget should not be exceeded after 500 tokens")
	}
	if cancelCalled {
		t.Error("cancel should not be called yet")
	}

	// Second event: 600 more tokens — total 1100, should exceed
	runner.processBackendEvent("TASK-1", BackendEvent{
		Type:         EventTypeText,
		TokensInput:  400,
		TokensOutput: 200,
	}, state)

	if !state.budgetExceeded {
		t.Error("budget should be exceeded after 1100 tokens")
	}
	if !cancelCalled {
		t.Error("cancel function should have been called")
	}
	if state.budgetReason == "" {
		t.Error("budget reason should be set")
	}
}

func TestProcessBackendEvent_NoTokenLimitCallback(t *testing.T) {
	runner := NewRunner()
	// No tokenLimitCheck set — budget enforcement disabled

	state := &progressState{phase: "Starting"}

	// Send a large number of tokens — should not trigger any budget breach
	runner.processBackendEvent("TASK-1", BackendEvent{
		Type:         EventTypeText,
		TokensInput:  1000000,
		TokensOutput: 500000,
	}, state)

	if state.budgetExceeded {
		t.Error("budget should not be exceeded when no callback is set")
	}
}

func TestProcessBackendEvent_BudgetExceededSkipsFurtherChecks(t *testing.T) {
	runner := NewRunner()

	callCount := 0
	runner.SetTokenLimitCheck(func(taskID string, deltaInput, deltaOutput int64) bool {
		callCount++
		return false // Always exceeds
	})

	state := &progressState{
		phase:        "Starting",
		budgetCancel: func() {},
	}

	// First event triggers budget exceeded
	runner.processBackendEvent("TASK-1", BackendEvent{
		Type:         EventTypeText,
		TokensInput:  100,
		TokensOutput: 50,
	}, state)

	if !state.budgetExceeded {
		t.Error("budget should be exceeded")
	}
	if callCount != 1 {
		t.Errorf("expected 1 callback call, got %d", callCount)
	}

	// Second event should NOT trigger callback again (already exceeded)
	runner.processBackendEvent("TASK-1", BackendEvent{
		Type:         EventTypeText,
		TokensInput:  100,
		TokensOutput: 50,
	}, state)

	if callCount != 1 {
		t.Errorf("expected callback not called again after exceeded, got %d calls", callCount)
	}
}

func TestProcessBackendEvent_TokensStillTrackedWithBudget(t *testing.T) {
	runner := NewRunner()

	runner.SetTokenLimitCheck(func(taskID string, deltaInput, deltaOutput int64) bool {
		return true // Always allow
	})

	state := &progressState{
		phase:        "Starting",
		budgetCancel: func() {},
	}

	runner.processBackendEvent("TASK-1", BackendEvent{
		Type:         EventTypeText,
		TokensInput:  300,
		TokensOutput: 200,
	}, state)

	runner.processBackendEvent("TASK-1", BackendEvent{
		Type:         EventTypeText,
		TokensInput:  400,
		TokensOutput: 100,
	}, state)

	if state.tokensInput != 700 {
		t.Errorf("expected 700 input tokens, got %d", state.tokensInput)
	}
	if state.tokensOutput != 300 {
		t.Errorf("expected 300 output tokens, got %d", state.tokensOutput)
	}
}

func TestSetTokenLimitCheck(t *testing.T) {
	runner := NewRunner()

	if runner.tokenLimitCheck != nil {
		t.Error("expected nil tokenLimitCheck by default")
	}

	runner.SetTokenLimitCheck(func(taskID string, deltaInput, deltaOutput int64) bool {
		return true
	})

	if runner.tokenLimitCheck == nil {
		t.Error("expected non-nil tokenLimitCheck after setting")
	}
}

// TestExecute_PopulatesEffortAndComplexityOnResult verifies that after Execute() returns,
// the ExecutionResult contains non-empty EffortLevel and ComplexityLevel when model routing
// is enabled. GH-2807: these values flow to the executions DB via dispatcher.
func TestExecute_PopulatesEffortAndComplexityOnResult(t *testing.T) {
	projectDir := t.TempDir()

	backend := &mockSelfReviewBackend{output: "done"}
	runner := NewRunnerWithBackend(backend)
	runner.skipPreflightChecks = true
	// Enable effort routing so SelectEffort returns a non-empty string.
	runner.modelRouter = NewModelRouterWithEffort(
		&ModelRoutingConfig{
			Enabled: true,
			Trivial: "claude-haiku",
			Simple:  "claude-sonnet-4-6",
			Medium:  "claude-sonnet-4-6",
			Complex: "claude-sonnet-4-6",
		},
		DefaultTimeoutConfig(),
		&EffortRoutingConfig{
			Enabled: true,
			Trivial: "low",
			Simple:  "medium",
			Medium:  "medium",
			Complex: "high",
		},
	)

	task := &Task{
		ID:          "RT-EFFORT-001",
		Title:       "Fix typo in comment",
		Description: "Fix typo in comment",
		ProjectPath: projectDir,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := runner.Execute(ctx, task)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() not successful: %s", result.Error)
	}
	if result.ComplexityLevel == "" {
		t.Error("result.ComplexityLevel is empty; want a non-empty tier string")
	}
	if result.EffortLevel == "" {
		t.Error("result.EffortLevel is empty; want a non-empty effort string (routing was enabled)")
	}
}

// TestMetricsRecorder_CalledOncePerExecution verifies that SetMetricsRecorder
// wires the recorder and that all three Record methods are called exactly once
// per execution (GH-2855).
func TestMetricsRecorder_CalledOncePerExecution(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	const branch = "pilot/GH-test-metrics"
	dir := setupPRGuardRepo(t, branch, true) // adds a real commit so no-commit guard doesn't fire

	backend := &mockFixedBackend{
		result: &BackendResult{
			Success:      true,
			Output:       "done",
			TokensInput:  500,
			TokensOutput: 150,
			Model:        "claude-sonnet-test",
		},
	}

	rec := &fakeMetricsRecorder{}
	runner := NewRunnerWithBackend(backend)
	runner.SetRecordingEnabled(false)
	runner.skipPreflightChecks = true
	runner.config = &BackendConfig{SkipSelfReview: true}
	runner.SetMetricsRecorder(rec)

	task := &Task{
		ID:          "GH-test-metrics",
		Title:       "test metrics recording",
		Description: "verify recorder is called",
		ProjectPath: dir,
		Branch:      branch,
		CreatePR:    false,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := runner.Execute(ctx, task)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() not successful: %s", result.Error)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()

	if len(rec.execCalls) != 1 {
		t.Errorf("RecordExecution called %d times, want exactly 1", len(rec.execCalls))
	} else if rec.execCalls[0].result != "success" {
		t.Errorf("RecordExecution result = %q, want %q", rec.execCalls[0].result, "success")
	}

	if len(rec.costCalls) != 1 {
		t.Errorf("RecordCost called %d times, want exactly 1", len(rec.costCalls))
	}

	if len(rec.tokenCalls) == 0 {
		t.Error("RecordTokens not called; want at least one call for input tokens")
	}

	if len(rec.durationCalls) != 1 {
		t.Errorf("RecordExecutionDuration called %d times, want exactly 1", len(rec.durationCalls))
	} else if rec.durationCalls[0] <= 0 {
		t.Errorf("RecordExecutionDuration got %v, want positive duration", rec.durationCalls[0])
	}
}

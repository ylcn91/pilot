package executor

import (
	"context"
	"sync"
	"testing"
	"time"
)

// scriptedBackend returns a scripted (result, err) per Execute call, repeating the
// last entry once exhausted. Used to drive the runner's failure→retry orchestration
// without a real subprocess.
type scriptedBackend struct {
	mu        sync.Mutex
	responses []scriptedResponse
	calls     int
}

type scriptedResponse struct {
	result *BackendResult
	err    error
}

func (b *scriptedBackend) Name() string      { return "scripted" }
func (b *scriptedBackend) IsAvailable() bool { return true }
func (b *scriptedBackend) Execute(_ context.Context, _ ExecuteOptions) (*BackendResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	idx := b.calls
	b.calls++
	if idx >= len(b.responses) {
		idx = len(b.responses) - 1
	}
	resp := b.responses[idx]
	return resp.result, resp.err
}

func (b *scriptedBackend) callCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

// zeroBackoffRetrier builds a Retrier whose api_error strategy retries once with
// no backoff, so the smart-retry success path runs fast and deterministically.
func zeroBackoffRetrier() *Retrier {
	return NewRetrier(&RetryConfig{
		Enabled: true,
		APIError: &RetryStrategy{
			MaxAttempts:       3,
			InitialBackoff:    0,
			BackoffMultiplier: 1.0,
		},
	})
}

// TestExecute_SmartRetrySucceeds drives Execute through the failure branch where
// the first backend call returns a classified api_error, the smart retrier fires,
// and the retried backend call succeeds (runner.go goto retrySucceeded). The run
// must then finalize as a success.
func TestExecute_SmartRetrySucceeds(t *testing.T) {
	const branch = "pilot/GH-smart-retry"
	dir := setupPRGuardRepo(t, branch, true) // pre-existing commit so no-commit guard is moot

	backend := &scriptedBackend{responses: []scriptedResponse{
		// First call: transient API error → retryable.
		{result: nil, err: &ClaudeCodeError{Type: ErrorTypeAPIError, Message: "transient 500", Stderr: "boom"}},
		// Retry call: success.
		{result: &BackendResult{Success: true, Output: "done", Model: "claude-test"}, err: nil},
	}}

	r := NewRunnerWithBackend(backend)
	r.SetRecordingEnabled(false)
	r.skipPreflightChecks = true
	r.config = &BackendConfig{SkipSelfReview: true}
	r.retrier = zeroBackoffRetrier()
	// Quality factory that passes so finalize completes cleanly.
	r.SetQualityCheckerFactory(func(_, _ string) QualityChecker {
		return &mockQualityChecker{outcome: &QualityOutcome{Passed: true}}
	})

	task := &Task{
		ID:          "GH-smart-retry",
		Title:       "smart retry success",
		Description: "verify retried backend success finalizes",
		ProjectPath: dir,
		Branch:      branch,
		CreatePR:    false,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := r.Execute(ctx, task)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success after smart retry, got failure: %s", result.Error)
	}
	if backend.callCount() != 2 {
		t.Errorf("backend called %d times, want 2 (initial fail + successful retry)", backend.callCount())
	}
}

// TestExecute_DecomposeOnKillFallback drives Execute through the failure branch
// where the first backend call returns a timeout BackendError, the retrier is
// configured with DecomposeOnKill, and the decomposer splits the task — exercising
// DecomposeForRetry + executeDecomposedTask (runner.go ~276-288).
func TestExecute_DecomposeOnKillFallback(t *testing.T) {
	const branch = "pilot/GH-decompose-kill"
	dir := setupPRGuardRepo(t, branch, true)

	// Backend: first (primary) call times out; every subsequent subtask call succeeds.
	backend := &scriptedBackend{responses: []scriptedResponse{
		{result: nil, err: &ClaudeCodeError{Type: ErrorTypeTimeout, Message: "killed: deadline", Stderr: "killed"}},
		{result: &BackendResult{Success: true, Output: "subtask done", Model: "claude-test"}, err: nil},
	}}

	r := NewRunnerWithBackend(backend)
	r.SetRecordingEnabled(false)
	r.skipPreflightChecks = true
	r.config = &BackendConfig{SkipSelfReview: true}
	// Retrier with DecomposeOnKill enabled; timeout strategy absent so smart-retry
	// does NOT fire, leaving the decompose fallback as the active path.
	r.retrier = NewRetrier(&RetryConfig{
		Enabled:         true,
		DecomposeOnKill: true,
	})
	r.decomposer = NewTaskDecomposer(&DecomposeConfig{
		Enabled:             true,
		MaxSubtasks:         5,
		MinComplexity:       "complex",
		MinDescriptionWords: 1,
	})
	r.SetQualityCheckerFactory(func(_, _ string) QualityChecker {
		return &mockQualityChecker{outcome: &QualityOutcome{Passed: true}}
	})

	// Title carries a trivial pattern so DetectComplexity returns trivial and the
	// up-front Decompose (which requires complex) is skipped — the task reaches the
	// primary backend, fails with timeout, and only THEN does DecomposeForRetry
	// (which bypasses the complexity/word-count gates) split on the numbered steps.
	task := &Task{
		ID:    "GH-decompose-kill",
		Title: "fix typo in handler",
		Description: "Adjust the handler in steps:\n" +
			"1. Add the response field\n" +
			"2. Wire the field into the encoder\n",
		ProjectPath: dir,
		Branch:      branch,
		CreatePR:    false,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := r.Execute(ctx, task)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	// The decompose fallback returns the aggregate result keyed by the parent ID.
	if result.TaskID != task.ID {
		t.Errorf("aggregate result TaskID = %q, want parent %q (decompose fallback)", result.TaskID, task.ID)
	}
	if !result.Success {
		t.Fatalf("expected decomposed subtasks to succeed, got failure: %s", result.Error)
	}
	// Primary timeout (1) + at least one subtask execution proves the fallback ran.
	if backend.callCount() < 2 {
		t.Errorf("backend called %d times, want >=2 (primary timeout + subtask executions)", backend.callCount())
	}
}

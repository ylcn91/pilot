package executor

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// seqQualityChecker returns a different QualityOutcome per Check call (then repeats
// the last), tracking how many times Check was invoked.
type seqQualityChecker struct {
	mu       sync.Mutex
	outcomes []*QualityOutcome
	calls    int
}

func (c *seqQualityChecker) Check(_ context.Context) (*QualityOutcome, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	idx := c.calls
	c.calls++
	if idx >= len(c.outcomes) {
		idx = len(c.outcomes) - 1
	}
	return c.outcomes[idx], nil
}

func newQualityGatesState(t *testing.T, r *Runner) *executeState {
	t.Helper()
	return &executeState{
		start:    time.Now(),
		task:     &Task{ID: "GH-QG-1", Title: "quality gate test", ProjectPath: t.TempDir()},
		ctx:      context.Background(),
		log:      r.log,
		state:    &progressState{},
		result:   &ExecutionResult{TaskID: "GH-QG-1", Success: true},
		duration: time.Second,
	}
}

// TestExecuteQualityGates_PassFirstTry verifies that when the gate passes on the
// first attempt, executeQualityGates returns (nil,nil) to continue, flips
// s.qualityGatesPassed, and invokes the checker exactly once (no retry).
func TestExecuteQualityGates_PassFirstTry(t *testing.T) {
	backend := &mockFixedBackend{result: &BackendResult{Success: true}}
	r := NewRunnerWithBackend(backend)

	checker := &seqQualityChecker{outcomes: []*QualityOutcome{{Passed: true}}}
	r.SetQualityCheckerFactory(func(_, _ string) QualityChecker { return checker })

	s := newQualityGatesState(t, r)

	res, err := r.executeQualityGates(s)
	if err != nil {
		t.Fatalf("executeQualityGates returned err: %v", err)
	}
	if res != nil {
		t.Fatalf("executeQualityGates returned non-nil abort result on pass: %+v", res)
	}
	if !s.qualityGatesPassed {
		t.Error("s.qualityGatesPassed should be true after a passing gate")
	}
	if checker.calls != 1 {
		t.Errorf("checker called %d times, want 1 (no retry on first-try pass)", checker.calls)
	}
	if backend.execCount != 0 {
		t.Errorf("backend retry invoked %d times, want 0 on first-try pass", backend.execCount)
	}
}

// TestExecuteQualityGates_RetryThenPassAccumulatesTokens verifies the retry loop:
// gate fails-with-retry once (re-invoking the backend), then passes. The retry
// backend's token usage must accumulate into the ExecutionResult.
func TestExecuteQualityGates_RetryThenPassAccumulatesTokens(t *testing.T) {
	backend := &mockFixedBackend{result: &BackendResult{
		Success:      true,
		TokensInput:  120,
		TokensOutput: 45,
		Model:        "claude-retry-model",
	}}
	r := NewRunnerWithBackend(backend)

	checker := &seqQualityChecker{outcomes: []*QualityOutcome{
		{Passed: false, ShouldRetry: true, RetryFeedback: "fix the failing test"},
		{Passed: true},
	}}
	r.SetQualityCheckerFactory(func(_, _ string) QualityChecker { return checker })

	s := newQualityGatesState(t, r)

	res, err := r.executeQualityGates(s)
	if err != nil {
		t.Fatalf("executeQualityGates returned err: %v", err)
	}
	if res != nil {
		t.Fatalf("executeQualityGates returned non-nil abort result on retry-then-pass: %+v", res)
	}
	if !s.qualityGatesPassed {
		t.Error("s.qualityGatesPassed should be true after the retry passes")
	}
	if checker.calls != 2 {
		t.Errorf("checker called %d times, want 2 (fail+retry pass)", checker.calls)
	}
	if backend.execCount != 1 {
		t.Errorf("retry backend invoked %d times, want 1", backend.execCount)
	}
	if s.result.TokensInput != 120 || s.result.TokensOutput != 45 {
		t.Errorf("token accumulation = (%d,%d), want (120,45) from the retry backend", s.result.TokensInput, s.result.TokensOutput)
	}
	if s.result.TokensTotal != 165 {
		t.Errorf("TokensTotal = %d, want 165", s.result.TokensTotal)
	}
	if s.result.ModelName != "claude-retry-model" {
		t.Errorf("ModelName = %q, want %q from the retry backend", s.result.ModelName, "claude-retry-model")
	}
}

// TestExecuteQualityGates_CircuitBreakerExhaustsRetries verifies the maxAutoRetries
// circuit breaker: a gate that always fails-with-retry stops after the bounded
// number of backend retries and aborts with a "failed after N auto-retries" error.
func TestExecuteQualityGates_CircuitBreakerExhaustsRetries(t *testing.T) {
	backend := &mockFixedBackend{result: &BackendResult{Success: true, TokensInput: 10, TokensOutput: 5}}
	r := NewRunnerWithBackend(backend)

	checker := &seqQualityChecker{outcomes: []*QualityOutcome{
		{Passed: false, ShouldRetry: true, RetryFeedback: "still failing"},
	}}
	r.SetQualityCheckerFactory(func(_, _ string) QualityChecker { return checker })

	s := newQualityGatesState(t, r)

	res, err := r.executeQualityGates(s)
	if err != nil {
		t.Fatalf("executeQualityGates returned err: %v", err)
	}
	if res == nil {
		t.Fatal("executeQualityGates returned nil; want a non-nil abort result after exhausting retries")
	}
	if res.Success {
		t.Error("result.Success should be false after the circuit breaker trips")
	}
	if !strings.Contains(res.Error, "auto-retries") {
		t.Errorf("Error = %q, want it to mention auto-retries (circuit breaker)", res.Error)
	}
	// maxAutoRetries=2 -> attempts at retryAttempt 0,1,2; gate checked 3 times,
	// backend retried at attempts 0 and 1 (attempt 2 has no retries left).
	if checker.calls != 3 {
		t.Errorf("checker called %d times, want 3 (initial + 2 retries)", checker.calls)
	}
	if backend.execCount != 2 {
		t.Errorf("retry backend invoked %d times, want 2 (bounded by maxAutoRetries)", backend.execCount)
	}
	if s.qualityGatesPassed {
		t.Error("s.qualityGatesPassed must stay false when gates never pass")
	}
}

// TestExecuteQualityGates_CheckerErrorAborts verifies a hard checker error
// (not a normal failed-outcome) aborts immediately with a quality-gate-error.
func TestExecuteQualityGates_CheckerErrorAborts(t *testing.T) {
	backend := &mockFixedBackend{result: &BackendResult{Success: true}}
	r := NewRunnerWithBackend(backend)

	r.SetQualityCheckerFactory(func(_, _ string) QualityChecker {
		return &mockQualityChecker{err: context.DeadlineExceeded}
	})

	s := newQualityGatesState(t, r)

	res, err := r.executeQualityGates(s)
	if err != nil {
		t.Fatalf("executeQualityGates returned err: %v", err)
	}
	if res == nil {
		t.Fatal("executeQualityGates returned nil; want a non-nil abort result on checker error")
	}
	if res.Success {
		t.Error("result.Success should be false on a checker error")
	}
	if !strings.Contains(res.Error, "quality gate error") {
		t.Errorf("Error = %q, want it to mention 'quality gate error'", res.Error)
	}
}

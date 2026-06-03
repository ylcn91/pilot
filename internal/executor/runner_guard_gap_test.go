package executor

import (
	"testing"
	"time"
)

// TestExecuteBudgetGuard_FiresOnBudgetExceeded verifies the budget-guard abort
// decision (GH-539): when state.budgetExceeded is set, executeBudgetGuard returns
// a non-nil result tagged "budget_exceeded" and populated with the tracked tokens.
func TestExecuteBudgetGuard_FiresOnBudgetExceeded(t *testing.T) {
	r := NewRunner()

	s := &executeState{
		task: &Task{ID: "GH-BUDGET-1", Title: "budget test", ProjectPath: t.TempDir()},
		log:  r.log,
		result: &ExecutionResult{
			TaskID: "GH-BUDGET-1",
		},
		duration: 2 * time.Second,
		state: &progressState{
			budgetExceeded: true,
			budgetReason:   "per-task token limit of 1000 exceeded",
			tokensInput:    700,
			tokensOutput:   400,
			modelName:      "claude-sonnet-test",
		},
	}

	res := r.executeBudgetGuard(s)
	if res == nil {
		t.Fatal("executeBudgetGuard returned nil; want a non-nil abort result")
	}
	if res.Outcome != "budget_exceeded" {
		t.Errorf("Outcome = %q, want %q", res.Outcome, "budget_exceeded")
	}
	if res.Error == "" {
		t.Error("Error should be set on a budget abort")
	}
	if res.TokensInput != 700 || res.TokensOutput != 400 {
		t.Errorf("token snapshot = (%d,%d), want (700,400)", res.TokensInput, res.TokensOutput)
	}
	if res.TokensTotal != 1100 {
		t.Errorf("TokensTotal = %d, want 1100", res.TokensTotal)
	}
	if res.ModelName != "claude-sonnet-test" {
		t.Errorf("ModelName = %q, want %q", res.ModelName, "claude-sonnet-test")
	}
}

// TestExecuteBudgetGuard_NoFireWhenNotExceeded verifies executeBudgetGuard returns
// nil (continue) when the budget was not exceeded.
func TestExecuteBudgetGuard_NoFireWhenNotExceeded(t *testing.T) {
	r := NewRunner()

	s := &executeState{
		task:     &Task{ID: "GH-BUDGET-2", ProjectPath: t.TempDir()},
		log:      r.log,
		result:   &ExecutionResult{TaskID: "GH-BUDGET-2"},
		duration: time.Second,
		state:    &progressState{budgetExceeded: false},
	}

	if res := r.executeBudgetGuard(s); res != nil {
		t.Errorf("executeBudgetGuard returned %+v; want nil when budget not exceeded", res)
	}
}

// TestExecuteBudgetGuard_FallbackModelWhenUnset verifies the abort result falls
// back to the runner's default model name when state.modelName is empty.
func TestExecuteBudgetGuard_FallbackModelWhenUnset(t *testing.T) {
	r := NewRunner()

	s := &executeState{
		task:     &Task{ID: "GH-BUDGET-3", ProjectPath: t.TempDir()},
		log:      r.log,
		result:   &ExecutionResult{TaskID: "GH-BUDGET-3"},
		duration: time.Second,
		state: &progressState{
			budgetExceeded: true,
			budgetReason:   "exceeded",
			// modelName intentionally empty
		},
	}

	res := r.executeBudgetGuard(s)
	if res == nil {
		t.Fatal("executeBudgetGuard returned nil; want a non-nil abort result")
	}
	if res.ModelName == "" {
		t.Error("ModelName should fall back to the runner default when state.modelName is empty")
	}
}

// TestExecuteStallGuard_FiresOnStall verifies the stall-guard abort decision
// (TASK-308): when state.stallDetected is set, executeStallGuard returns a non-nil
// result tagged "stalled" with the stall timeout reflected in the error.
func TestExecuteStallGuard_FiresOnStall(t *testing.T) {
	r := NewRunner()

	const stallTimeout = 90 * time.Second
	s := &executeState{
		task:     &Task{ID: "GH-STALL-1", Title: "stall test", ProjectPath: t.TempDir()},
		log:      r.log,
		result:   &ExecutionResult{TaskID: "GH-STALL-1"},
		duration: 3 * time.Second,
		state: &progressState{
			stallDetected: true,
			tokensInput:   120,
			tokensOutput:  30,
			modelName:     "claude-sonnet-test",
		},
	}

	res := r.executeStallGuard(s, stallTimeout)
	if res == nil {
		t.Fatal("executeStallGuard returned nil; want a non-nil abort result")
	}
	if res.Outcome != "stalled" {
		t.Errorf("Outcome = %q, want %q", res.Outcome, "stalled")
	}
	if res.Error == "" {
		t.Error("Error should be set on a stall abort")
	}
	if res.TokensTotal != 150 {
		t.Errorf("TokensTotal = %d, want 150", res.TokensTotal)
	}
}

// TestExecuteStallGuard_NoFireWhenLive verifies executeStallGuard returns nil
// (continue) when no stall was detected.
func TestExecuteStallGuard_NoFireWhenLive(t *testing.T) {
	r := NewRunner()

	s := &executeState{
		task:     &Task{ID: "GH-STALL-2", ProjectPath: t.TempDir()},
		log:      r.log,
		result:   &ExecutionResult{TaskID: "GH-STALL-2"},
		duration: time.Second,
		state:    &progressState{stallDetected: false},
	}

	if res := r.executeStallGuard(s, 60*time.Second); res != nil {
		t.Errorf("executeStallGuard returned %+v; want nil when no stall detected", res)
	}
}

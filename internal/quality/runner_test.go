package quality

import (
	"context"
	"testing"
	"time"
)

func TestRunner_RunAll_Disabled(t *testing.T) {
	config := &Config{
		Enabled: false,
		Gates:   []*Gate{},
	}

	runner := NewRunner(config, "/tmp")
	results, err := runner.RunAll(context.Background(), "test-task")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !results.AllPassed {
		t.Error("expected AllPassed to be true when disabled")
	}
	if len(results.Results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results.Results))
	}
}

func TestRunner_RunAll_PassingGates(t *testing.T) {
	config := &Config{
		Enabled: true,
		Gates: []*Gate{
			{
				Name:     "echo",
				Type:     GateCustom,
				Command:  "echo 'hello'",
				Required: true,
				Timeout:  10 * time.Second,
			},
			{
				Name:     "true",
				Type:     GateCustom,
				Command:  "true",
				Required: true,
				Timeout:  10 * time.Second,
			},
		},
	}

	runner := NewRunner(config, "/tmp")
	results, err := runner.RunAll(context.Background(), "test-task")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !results.AllPassed {
		t.Error("expected AllPassed to be true for passing gates")
	}
	if len(results.Results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results.Results))
	}

	for _, r := range results.Results {
		if r.Status != StatusPassed {
			t.Errorf("gate %s: expected status Passed, got %s", r.GateName, r.Status)
		}
	}
}

func TestRunner_RunAll_FailingRequiredGate(t *testing.T) {
	config := &Config{
		Enabled: true,
		Gates: []*Gate{
			{
				Name:       "failing",
				Type:       GateCustom,
				Command:    "exit 1",
				Required:   true,
				Timeout:    10 * time.Second,
				MaxRetries: 0,
			},
		},
	}

	runner := NewRunner(config, "/tmp")
	results, err := runner.RunAll(context.Background(), "test-task")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results.AllPassed {
		t.Error("expected AllPassed to be false for failing required gate")
	}
	if len(results.Results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results.Results))
	}
	if results.Results[0].Status != StatusFailed {
		t.Errorf("expected status Failed, got %s", results.Results[0].Status)
	}
}

func TestRunner_RunAll_FailingOptionalGate(t *testing.T) {
	config := &Config{
		Enabled: true,
		Gates: []*Gate{
			{
				Name:       "optional-failing",
				Type:       GateCustom,
				Command:    "exit 1",
				Required:   false, // Not required
				Timeout:    10 * time.Second,
				MaxRetries: 0,
			},
		},
	}

	runner := NewRunner(config, "/tmp")
	results, err := runner.RunAll(context.Background(), "test-task")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !results.AllPassed {
		t.Error("expected AllPassed to be true for failing optional gate")
	}
}

func TestRunner_OnProgress(t *testing.T) {
	config := &Config{
		Enabled: true,
		Gates: []*Gate{
			{
				Name:     "progress-gate",
				Type:     GateCustom,
				Command:  "echo 'test'",
				Required: true,
				Timeout:  5 * time.Second,
			},
		},
	}

	runner := NewRunner(config, "/tmp")

	var progressEvents []struct {
		gateName string
		status   GateStatus
		message  string
	}

	runner.OnProgress(func(gateName string, status GateStatus, message string) {
		progressEvents = append(progressEvents, struct {
			gateName string
			status   GateStatus
			message  string
		}{gateName, status, message})
	})

	_, err := runner.RunAll(context.Background(), "progress-task")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(progressEvents) < 2 {
		t.Errorf("expected at least 2 progress events (running + final), got %d", len(progressEvents))
	}

	// Verify first event is running
	if len(progressEvents) > 0 && progressEvents[0].status != StatusRunning {
		t.Errorf("expected first event status Running, got %s", progressEvents[0].status)
	}

	// Verify last event is a terminal status
	if len(progressEvents) > 0 {
		lastStatus := progressEvents[len(progressEvents)-1].status
		if lastStatus != StatusPassed && lastStatus != StatusFailed {
			t.Errorf("expected last event to be terminal status, got %s", lastStatus)
		}
	}
}

func TestRunner_RunAll_ParallelExecution(t *testing.T) {
	// Each gate sleeps for 100ms. In parallel, total time should be ~100ms.
	// In sequential, total time would be ~300ms.
	// TASK-289: parallel default flipped to false, so this test must opt in explicitly.
	parallelTrue := true
	config := &Config{
		Enabled:  true,
		Parallel: &parallelTrue,
		Gates: []*Gate{
			{Name: "gate1", Type: GateCustom, Command: "sleep 0.1", Required: true, Timeout: 5 * time.Second},
			{Name: "gate2", Type: GateCustom, Command: "sleep 0.1", Required: true, Timeout: 5 * time.Second},
			{Name: "gate3", Type: GateCustom, Command: "sleep 0.1", Required: true, Timeout: 5 * time.Second},
		},
	}

	runner := NewRunner(config, "/tmp")
	start := time.Now()
	results, err := runner.RunAll(context.Background(), "parallel-test")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !results.AllPassed {
		t.Error("expected all gates to pass")
	}
	// Parallel execution: should complete in ~100-200ms, not 300ms+
	if elapsed > 250*time.Millisecond {
		t.Errorf("expected parallel execution to complete in <250ms, took %v", elapsed)
	}
}

func TestRunner_RunAll_SequentialExecution(t *testing.T) {
	// Each gate sleeps for 50ms. In sequential, total time should be ~150ms.
	parallelFalse := false
	config := &Config{
		Enabled:  true,
		Parallel: &parallelFalse,
		Gates: []*Gate{
			{Name: "gate1", Type: GateCustom, Command: "sleep 0.05", Required: true, Timeout: 5 * time.Second},
			{Name: "gate2", Type: GateCustom, Command: "sleep 0.05", Required: true, Timeout: 5 * time.Second},
			{Name: "gate3", Type: GateCustom, Command: "sleep 0.05", Required: true, Timeout: 5 * time.Second},
		},
	}

	runner := NewRunner(config, "/tmp")
	start := time.Now()
	results, err := runner.RunAll(context.Background(), "sequential-test")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !results.AllPassed {
		t.Error("expected all gates to pass")
	}
	// Sequential execution: should take at least 150ms (3 x 50ms)
	if elapsed < 140*time.Millisecond {
		t.Errorf("expected sequential execution to take at least 140ms, took %v", elapsed)
	}
}

func TestConfig_IsParallel(t *testing.T) {
	// TASK-289: nil now defaults to false (was true) — see SOP parallel-gate-cache-race.
	tests := []struct {
		name     string
		parallel *bool
		expected bool
	}{
		{name: "nil defaults to false (TASK-289)", parallel: nil, expected: false},
		{name: "explicit true", parallel: boolPtr(true), expected: true},
		{name: "explicit false", parallel: boolPtr(false), expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{Parallel: tt.parallel}
			if got := config.IsParallel(); got != tt.expected {
				t.Errorf("IsParallel() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestRunner_RunAll_DefaultsToSequential asserts that a Config with no explicit
// Parallel setting runs gates sequentially. TASK-289: prevents regression of the
// 2026-05-21 workshop incident where 11 spurious quality-gate failures hit in 3h
// due to concurrent gates racing on ~/.cache/go-build and ~/.cache/golangci-lint.
func TestRunner_RunAll_DefaultsToSequential(t *testing.T) {
	// Each gate sleeps for 50ms. Sequential: ~150ms; parallel would be ~50ms.
	// No Parallel field set → must default to sequential.
	config := &Config{
		Enabled: true,
		Gates: []*Gate{
			{Name: "gate1", Type: GateCustom, Command: "sleep 0.05", Required: true, Timeout: 5 * time.Second},
			{Name: "gate2", Type: GateCustom, Command: "sleep 0.05", Required: true, Timeout: 5 * time.Second},
			{Name: "gate3", Type: GateCustom, Command: "sleep 0.05", Required: true, Timeout: 5 * time.Second},
		},
	}

	if config.IsParallel() {
		t.Fatal("Config with nil Parallel must default to sequential (TASK-289)")
	}

	runner := NewRunner(config, "/tmp")
	start := time.Now()
	results, err := runner.RunAll(context.Background(), "default-sequential-test")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !results.AllPassed {
		t.Error("expected all gates to pass")
	}
	if elapsed < 140*time.Millisecond {
		t.Errorf("expected sequential (default) execution to take at least 140ms, took %v", elapsed)
	}
}

func boolPtr(b bool) *bool {
	return &b
}

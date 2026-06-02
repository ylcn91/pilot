package quality

import (
	"context"
	"testing"
	"time"
)

func TestNewExecutor(t *testing.T) {
	cfg := &ExecutorConfig{
		Config:      DefaultConfig(),
		ProjectPath: "/tmp/test",
		TaskID:      "test-task-1",
	}

	executor := NewExecutor(cfg)

	if executor == nil {
		t.Fatal("expected non-nil executor")
	}
	if executor.config != cfg.Config {
		t.Error("config not set correctly")
	}
	if executor.taskID != cfg.TaskID {
		t.Error("taskID not set correctly")
	}
	if executor.runner == nil {
		t.Error("runner not initialized")
	}
}

func TestExecutor_Check_Disabled(t *testing.T) {
	cfg := &ExecutorConfig{
		Config: &Config{
			Enabled: false,
		},
		ProjectPath: "/tmp",
		TaskID:      "task-1",
	}

	executor := NewExecutor(cfg)
	outcome, err := executor.Check(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !outcome.Passed {
		t.Error("expected Passed to be true when disabled")
	}
	if outcome.Attempt != 0 {
		t.Errorf("expected Attempt 0, got %d", outcome.Attempt)
	}
}

func TestExecutor_Check_Passing(t *testing.T) {
	cfg := &ExecutorConfig{
		Config: &Config{
			Enabled: true,
			Gates: []*Gate{
				{
					Name:     "echo",
					Type:     GateCustom,
					Command:  "echo 'test'",
					Required: true,
					Timeout:  5 * time.Second,
				},
			},
		},
		ProjectPath: "/tmp",
		TaskID:      "task-2",
	}

	executor := NewExecutor(cfg)
	outcome, err := executor.Check(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !outcome.Passed {
		t.Error("expected Passed to be true for passing gates")
	}
	if outcome.ShouldRetry {
		t.Error("expected ShouldRetry to be false when passed")
	}
	if outcome.Results == nil {
		t.Error("expected non-nil Results")
	}
}

func TestExecutor_Check_Failing(t *testing.T) {
	cfg := &ExecutorConfig{
		Config: &Config{
			Enabled: true,
			Gates: []*Gate{
				{
					Name:       "fail",
					Type:       GateCustom,
					Command:    "exit 1",
					Required:   true,
					Timeout:    5 * time.Second,
					MaxRetries: 0,
				},
			},
			OnFailure: FailureConfig{
				Action:     ActionRetry,
				MaxRetries: 2,
			},
		},
		ProjectPath: "/tmp",
		TaskID:      "task-3",
	}

	executor := NewExecutor(cfg)
	outcome, err := executor.Check(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Passed {
		t.Error("expected Passed to be false for failing gates")
	}
	if !outcome.ShouldRetry {
		t.Error("expected ShouldRetry to be true on first failure")
	}
	if outcome.RetryFeedback == "" {
		t.Error("expected RetryFeedback to be non-empty")
	}
}

func TestExecutor_CheckWithAttempt(t *testing.T) {
	tests := []struct {
		name        string
		attempt     int
		maxRetries  int
		shouldRetry bool
		gatePasses  bool
	}{
		{
			name:        "first attempt failure should retry",
			attempt:     0,
			maxRetries:  2,
			shouldRetry: true,
			gatePasses:  false,
		},
		{
			name:        "last attempt failure should not retry",
			attempt:     2,
			maxRetries:  2,
			shouldRetry: false,
			gatePasses:  false,
		},
		{
			name:        "passing gate should not retry",
			attempt:     0,
			maxRetries:  2,
			shouldRetry: false,
			gatePasses:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command := "exit 1"
			if tt.gatePasses {
				command = "true"
			}

			cfg := &ExecutorConfig{
				Config: &Config{
					Enabled: true,
					Gates: []*Gate{
						{
							Name:       "test",
							Type:       GateCustom,
							Command:    command,
							Required:   true,
							Timeout:    5 * time.Second,
							MaxRetries: 0,
						},
					},
					OnFailure: FailureConfig{
						Action:     ActionRetry,
						MaxRetries: tt.maxRetries,
					},
				},
				ProjectPath: "/tmp",
				TaskID:      "task",
			}

			executor := NewExecutor(cfg)
			outcome, err := executor.CheckWithAttempt(context.Background(), tt.attempt)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if outcome.Attempt != tt.attempt {
				t.Errorf("expected Attempt %d, got %d", tt.attempt, outcome.Attempt)
			}
			if outcome.ShouldRetry != tt.shouldRetry {
				t.Errorf("expected ShouldRetry %v, got %v", tt.shouldRetry, outcome.ShouldRetry)
			}
		})
	}
}

func TestExecutor_OnProgress(t *testing.T) {
	cfg := &ExecutorConfig{
		Config: &Config{
			Enabled: true,
			Gates: []*Gate{
				{
					Name:     "progress-test",
					Type:     GateCustom,
					Command:  "echo 'hello'",
					Required: true,
					Timeout:  5 * time.Second,
				},
			},
		},
		ProjectPath: "/tmp",
		TaskID:      "task-progress",
	}

	executor := NewExecutor(cfg)

	var progressCalls []struct {
		gateName string
		status   GateStatus
		message  string
	}

	executor.OnProgress(func(gateName string, status GateStatus, message string) {
		progressCalls = append(progressCalls, struct {
			gateName string
			status   GateStatus
			message  string
		}{gateName, status, message})
	})

	_, err := executor.Check(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(progressCalls) == 0 {
		t.Error("expected progress callback to be called")
	}

	// Should have at least running and passed/failed status
	hasRunning := false
	hasFinal := false
	for _, call := range progressCalls {
		if call.status == StatusRunning {
			hasRunning = true
		}
		if call.status == StatusPassed || call.status == StatusFailed {
			hasFinal = true
		}
	}

	if !hasRunning {
		t.Error("expected running status in progress calls")
	}
	if !hasFinal {
		t.Error("expected final status in progress calls")
	}
}

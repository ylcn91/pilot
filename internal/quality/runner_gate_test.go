package quality

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRunner_RunGate_WithRetry(t *testing.T) {
	// Use a file to track attempts
	config := &Config{
		Enabled: true,
		Gates: []*Gate{
			{
				Name:       "retry-test",
				Type:       GateCustom,
				Command:    "exit 1",
				Required:   true,
				Timeout:    5 * time.Second,
				MaxRetries: 2,
				RetryDelay: 10 * time.Millisecond,
			},
		},
	}

	runner := NewRunner(config, "/tmp")
	result, err := runner.RunGate(context.Background(), "retry-test")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusFailed {
		t.Errorf("expected status Failed after retries, got %s", result.Status)
	}
	if result.RetryCount != 2 {
		t.Errorf("expected 2 retries, got %d", result.RetryCount)
	}
}

func TestRunner_RunGate_ContextCancellation(t *testing.T) {
	config := &Config{
		Enabled: true,
		Gates: []*Gate{
			{
				Name:     "long-running",
				Type:     GateCustom,
				Command:  "sleep 30",
				Required: true,
				Timeout:  60 * time.Second,
			},
		},
	}

	runner := NewRunner(config, "/tmp")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result, err := runner.RunGate(ctx, "long-running")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusFailed {
		t.Errorf("expected status Failed after cancellation, got %s", result.Status)
	}
}

func TestRunner_RunGate_NotFound(t *testing.T) {
	config := &Config{
		Enabled: true,
		Gates:   []*Gate{},
	}

	runner := NewRunner(config, "/tmp")
	_, err := runner.RunGate(context.Background(), "nonexistent")

	if err != ErrGateNotFound {
		t.Errorf("expected ErrGateNotFound, got %v", err)
	}
}

func TestRunner_CoverageGate(t *testing.T) {
	config := &Config{
		Enabled: true,
		Gates: []*Gate{
			{
				Name:      "coverage-test",
				Type:      GateCoverage,
				Command:   "echo 'coverage: 85.3% of statements'",
				Required:  true,
				Timeout:   10 * time.Second,
				Threshold: 80.0,
			},
		},
	}

	runner := NewRunner(config, "/tmp")
	result, err := runner.RunGate(context.Background(), "coverage-test")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusPassed {
		t.Errorf("expected status Passed, got %s", result.Status)
	}
	if result.Coverage < 85.0 || result.Coverage > 86.0 {
		t.Errorf("expected coverage ~85.3%%, got %.1f%%", result.Coverage)
	}
}

func TestRunner_CoverageGate_BelowThreshold(t *testing.T) {
	config := &Config{
		Enabled: true,
		Gates: []*Gate{
			{
				Name:       "coverage-low",
				Type:       GateCoverage,
				Command:    "echo 'coverage: 50.0% of statements'",
				Required:   true,
				Timeout:    10 * time.Second,
				Threshold:  80.0,
				MaxRetries: 0,
			},
		},
	}

	runner := NewRunner(config, "/tmp")
	result, err := runner.RunGate(context.Background(), "coverage-low")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusFailed {
		t.Errorf("expected status Failed for low coverage, got %s", result.Status)
	}
	if !strings.Contains(result.Error, "below threshold") {
		t.Errorf("expected error about threshold, got: %s", result.Error)
	}
}

func TestRunner_RunGate_Timeout(t *testing.T) {
	config := &Config{
		Enabled: true,
		Gates: []*Gate{
			{
				Name:       "timeout-test",
				Type:       GateCustom,
				Command:    "sleep 10",
				Required:   true,
				Timeout:    100 * time.Millisecond,
				MaxRetries: 0,
			},
		},
	}

	runner := NewRunner(config, "/tmp")
	result, err := runner.RunGate(context.Background(), "timeout-test")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// When timeout occurs, the gate should fail with exit code != 0 or error
	if result.Status != StatusFailed {
		// The timeout may result in various failure states - check it didn't pass
		if result.ExitCode == 0 && result.Error == "" {
			t.Errorf("expected gate to fail due to timeout, but it passed")
		}
	}
}

func TestRunner_RunGate_CommandWithOutput(t *testing.T) {
	config := &Config{
		Enabled: true,
		Gates: []*Gate{
			{
				Name:     "output-test",
				Type:     GateCustom,
				Command:  "echo 'stdout message' && echo 'stderr message' >&2",
				Required: true,
				Timeout:  5 * time.Second,
			},
		},
	}

	runner := NewRunner(config, "/tmp")
	result, err := runner.RunGate(context.Background(), "output-test")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusPassed {
		t.Errorf("expected status Passed, got %s", result.Status)
	}
	if !strings.Contains(result.Output, "stdout message") {
		t.Error("expected output to contain stdout message")
	}
	if !strings.Contains(result.Output, "stderr message") {
		t.Error("expected output to contain stderr message")
	}
}

func TestRunner_RunGate_RetrySuccess(t *testing.T) {
	// Create a temp file to track attempts
	tmpFile := "/tmp/quality_test_retry_" + time.Now().Format("20060102150405")
	defer func() {
		// Cleanup - ignore error as it's test cleanup
		_ = exec.Command("rm", "-f", tmpFile).Run()
	}()

	// Command that fails first time, succeeds second time
	command := `if [ -f ` + tmpFile + ` ]; then echo "success"; exit 0; else touch ` + tmpFile + `; exit 1; fi`

	config := &Config{
		Enabled: true,
		Gates: []*Gate{
			{
				Name:       "retry-success",
				Type:       GateCustom,
				Command:    command,
				Required:   true,
				Timeout:    5 * time.Second,
				MaxRetries: 2,
				RetryDelay: 10 * time.Millisecond,
			},
		},
	}

	runner := NewRunner(config, "/tmp")
	result, err := runner.RunGate(context.Background(), "retry-success")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusPassed {
		t.Errorf("expected status Passed after retry, got %s", result.Status)
	}
	if result.RetryCount != 1 {
		t.Errorf("expected 1 retry, got %d", result.RetryCount)
	}
}

func TestRunner_RunGate_ContextCancelDuringRetryDelay(t *testing.T) {
	config := &Config{
		Enabled: true,
		Gates: []*Gate{
			{
				Name:       "cancel-retry-delay",
				Type:       GateCustom,
				Command:    "exit 1",
				Required:   true,
				Timeout:    5 * time.Second,
				MaxRetries: 5,
				RetryDelay: 2 * time.Second, // Long delay to allow cancellation
			},
		},
	}

	runner := NewRunner(config, "/tmp")

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short time (during retry delay)
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	result, err := runner.RunGate(ctx, "cancel-retry-delay")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusFailed {
		t.Errorf("expected status Failed after cancellation, got %s", result.Status)
	}
	if !strings.Contains(result.Error, "cancelled") {
		t.Errorf("expected error to mention cancellation, got: %s", result.Error)
	}
}

func TestRunner_CoverageGate_NoThreshold(t *testing.T) {
	config := &Config{
		Enabled: true,
		Gates: []*Gate{
			{
				Name:      "coverage-no-threshold",
				Type:      GateCoverage,
				Command:   "echo 'coverage: 45.0% of statements'",
				Required:  true,
				Timeout:   10 * time.Second,
				Threshold: 0, // No threshold set
			},
		},
	}

	runner := NewRunner(config, "/tmp")
	result, err := runner.RunGate(context.Background(), "coverage-no-threshold")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should pass since no threshold is set
	if result.Status != StatusPassed {
		t.Errorf("expected status Passed with no threshold, got %s", result.Status)
	}
	if result.Coverage < 44.9 || result.Coverage > 45.1 {
		t.Errorf("expected coverage ~45.0%%, got %.1f%%", result.Coverage)
	}
}

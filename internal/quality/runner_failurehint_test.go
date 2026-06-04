package quality

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestRunner_FailingGate_CarriesFailureHint verifies that a gate's configured
// failure_hint travels onto the Result and reaches the retry feedback the
// executor sends to the model on failure.
func TestRunner_FailingGate_CarriesFailureHint(t *testing.T) {
	const hint = "Fix compilation errors in the changed files"

	config := &Config{
		Enabled: true,
		Gates: []*Gate{
			{
				Name:        "build",
				Type:        GateBuild,
				Command:     "exit 1",
				Required:    true,
				Timeout:     5 * time.Second,
				MaxRetries:  0,
				FailureHint: hint,
			},
		},
	}

	runner := NewRunner(config, "/tmp")
	result, err := runner.RunGate(context.Background(), "build")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusFailed {
		t.Fatalf("expected failed gate, got %s", result.Status)
	}
	if result.FailureHint != hint {
		t.Errorf("result.FailureHint = %q, want %q", result.FailureHint, hint)
	}

	feedback := FormatErrorFeedback(&CheckResults{Results: []*Result{result}})
	if !strings.Contains(feedback, hint) {
		t.Errorf("retry feedback missing failure hint; got:\n%s", feedback)
	}
}

// TestRunner_PassingGate_NoHintInFeedback verifies that a passing gate carries
// its hint on the Result but does not pollute the failure feedback.
func TestRunner_PassingGate_NoHintInFeedback(t *testing.T) {
	const hint = "should not appear"

	config := &Config{
		Enabled: true,
		Gates: []*Gate{
			{
				Name:        "build",
				Type:        GateBuild,
				Command:     "exit 0",
				Required:    true,
				Timeout:     5 * time.Second,
				FailureHint: hint,
			},
		},
	}

	runner := NewRunner(config, "/tmp")
	result, err := runner.RunGate(context.Background(), "build")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusPassed {
		t.Fatalf("expected passed gate, got %s", result.Status)
	}

	feedback := FormatErrorFeedback(&CheckResults{Results: []*Result{result}})
	if strings.Contains(feedback, hint) {
		t.Errorf("passing gate hint leaked into failure feedback:\n%s", feedback)
	}
}

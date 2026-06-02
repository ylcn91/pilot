package azuredevops

import (
	"context"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestNewPoller(t *testing.T) {
	client := NewClient(testutil.FakeAzureDevOpsPAT, "org", "project")
	poller := NewPoller(client, "pilot", 30*time.Second)

	if poller.tag != "pilot" {
		t.Errorf("expected tag 'pilot', got '%s'", poller.tag)
	}

	if poller.interval != 30*time.Second {
		t.Errorf("expected interval 30s, got %v", poller.interval)
	}

	if poller.executionMode != ExecutionModeParallel {
		t.Errorf("expected default mode parallel, got %s", poller.executionMode)
	}

	if !poller.waitForMerge {
		t.Error("expected waitForMerge to be true by default")
	}
}

func TestPollerWithOptions(t *testing.T) {
	client := NewClient(testutil.FakeAzureDevOpsPAT, "org", "project")

	handler := func(ctx context.Context, wi *WorkItem) error {
		return nil
	}

	poller := NewPoller(client, "pilot", 30*time.Second,
		WithOnWorkItem(handler),
		WithExecutionMode(ExecutionModeSequential),
		WithSequentialConfig(false, 10*time.Second, 30*time.Minute),
		WithWorkItemTypes([]string{"Bug", "Task"}),
	)

	if poller.executionMode != ExecutionModeSequential {
		t.Errorf("expected mode sequential, got %s", poller.executionMode)
	}

	if poller.waitForMerge {
		t.Error("expected waitForMerge to be false")
	}

	if poller.prPollInterval != 10*time.Second {
		t.Errorf("expected prPollInterval 10s, got %v", poller.prPollInterval)
	}

	if poller.prTimeout != 30*time.Minute {
		t.Errorf("expected prTimeout 30m, got %v", poller.prTimeout)
	}

	if len(poller.workItemTypes) != 2 {
		t.Errorf("expected 2 work item types, got %d", len(poller.workItemTypes))
	}

	// Test handler is set
	if poller.onWorkItem == nil {
		t.Error("expected onWorkItem handler to be set")
	}
}

func TestPollerMarkProcessed(t *testing.T) {
	client := NewClient(testutil.FakeAzureDevOpsPAT, "org", "project")
	poller := NewPoller(client, "pilot", 30*time.Second)

	if poller.IsProcessed(123) {
		t.Error("expected 123 NOT to be processed initially")
	}

	poller.markProcessed(123)

	if !poller.IsProcessed(123) {
		t.Error("expected 123 to be processed after marking")
	}

	if poller.ProcessedCount() != 1 {
		t.Errorf("expected processed count 1, got %d", poller.ProcessedCount())
	}

	poller.Reset()

	if poller.IsProcessed(123) {
		t.Error("expected 123 NOT to be processed after reset")
	}

	if poller.ProcessedCount() != 0 {
		t.Errorf("expected processed count 0 after reset, got %d", poller.ProcessedCount())
	}
}

// GH-1358: Tests for parallel execution pattern

func TestNewPollerWithMaxConcurrent(t *testing.T) {
	client := NewClient(testutil.FakeAzureDevOpsPAT, "org", "project")

	// Test default maxConcurrent
	poller := NewPoller(client, "pilot", 30*time.Second)
	if poller.maxConcurrent != 2 {
		t.Errorf("default maxConcurrent = %d, want 2", poller.maxConcurrent)
	}

	// Test custom maxConcurrent
	poller = NewPoller(client, "pilot", 30*time.Second, WithMaxConcurrent(5))
	if poller.maxConcurrent != 5 {
		t.Errorf("custom maxConcurrent = %d, want 5", poller.maxConcurrent)
	}

	// Test semaphore is created with correct capacity
	if cap(poller.semaphore) != 5 {
		t.Errorf("semaphore capacity = %d, want 5", cap(poller.semaphore))
	}

	// Test minimum maxConcurrent enforcement - WithMaxConcurrent enforces minimum of 1
	poller = NewPoller(client, "pilot", 30*time.Second, WithMaxConcurrent(0))
	if poller.maxConcurrent != 1 {
		t.Errorf("zero maxConcurrent should become 1, got %d", poller.maxConcurrent)
	}

	poller = NewPoller(client, "pilot", 30*time.Second, WithMaxConcurrent(-1))
	if poller.maxConcurrent != 1 {
		t.Errorf("negative maxConcurrent should become 1, got %d", poller.maxConcurrent)
	}
}

func TestPoller_ClearProcessed(t *testing.T) {
	client := NewClient(testutil.FakeAzureDevOpsPAT, "org", "project")
	poller := NewPoller(client, "pilot", 30*time.Second)

	// Mark a work item as processed
	poller.markProcessed(42)
	if !poller.IsProcessed(42) {
		t.Error("expected work item 42 to be processed after marking")
	}

	// Clear the processed flag
	poller.ClearProcessed(42)
	if poller.IsProcessed(42) {
		t.Error("expected work item 42 to not be processed after clearing")
	}

	// Clearing a non-existent ID should not panic
	poller.ClearProcessed(999)
}

func TestPoller_DrainAndWaitForActive(t *testing.T) {
	client := NewClient(testutil.FakeAzureDevOpsPAT, "org", "project")
	poller := NewPoller(client, "pilot", 30*time.Second, WithMaxConcurrent(2))

	// Test that WaitForActive sets stopping flag
	poller.WaitForActive()
	if !poller.stopping.Load() {
		t.Error("expected stopping flag to be true after WaitForActive")
	}

	// Reset for next test
	poller.stopping.Store(false)

	// Test that Drain sets stopping flag
	poller.Drain()
	if !poller.stopping.Load() {
		t.Error("expected stopping flag to be true after Drain")
	}
}

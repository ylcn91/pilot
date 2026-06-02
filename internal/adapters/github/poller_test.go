package github

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestNewPoller(t *testing.T) {
	tests := []struct {
		name     string
		repo     string
		label    string
		interval time.Duration
		wantErr  bool
	}{
		{
			name:     "valid repo format",
			repo:     "owner/repo",
			label:    "pilot",
			interval: 30 * time.Second,
			wantErr:  false,
		},
		{
			name:     "invalid repo format - no slash",
			repo:     "ownerrepo",
			label:    "pilot",
			interval: 30 * time.Second,
			wantErr:  true,
		},
		{
			name:     "invalid repo format - multiple slashes",
			repo:     "owner/repo/extra",
			label:    "pilot",
			interval: 30 * time.Second,
			wantErr:  true,
		},
		{
			name:     "invalid repo format - empty",
			repo:     "",
			label:    "pilot",
			interval: 30 * time.Second,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(testutil.FakeGitHubToken)
			poller, err := NewPoller(client, tt.repo, tt.label, tt.interval)

			if (err != nil) != tt.wantErr {
				t.Errorf("NewPoller() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if poller == nil {
					t.Fatal("NewPoller returned nil")
				}
				if poller.client != client {
					t.Error("poller.client not set correctly")
				}
				if poller.label != tt.label {
					t.Errorf("poller.label = %s, want %s", poller.label, tt.label)
				}
				if poller.interval != tt.interval {
					t.Errorf("poller.interval = %v, want %v", poller.interval, tt.interval)
				}
			}
		})
	}
}

func TestNewPoller_ParsesOwnerAndRepo(t *testing.T) {
	client := NewClient(testutil.FakeGitHubToken)
	poller, err := NewPoller(client, "myorg/myrepo", "pilot", 30*time.Second)

	if err != nil {
		t.Fatalf("NewPoller() error = %v", err)
	}

	if poller.owner != "myorg" {
		t.Errorf("poller.owner = %s, want 'myorg'", poller.owner)
	}
	if poller.repo != "myrepo" {
		t.Errorf("poller.repo = %s, want 'myrepo'", poller.repo)
	}
}

func TestWithPollerLogger(t *testing.T) {
	client := NewClient(testutil.FakeGitHubToken)

	// Create a custom logger
	customLogger := slog.Default()

	poller, err := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithPollerLogger(customLogger),
	)
	if err != nil {
		t.Fatalf("NewPoller() error = %v", err)
	}

	if poller.logger != customLogger {
		t.Error("custom logger should be set")
	}
}

func TestWithPollerLogger_DefaultLogger(t *testing.T) {
	client := NewClient(testutil.FakeGitHubToken)

	// Without custom logger, should use default
	poller, err := NewPoller(client, "owner/repo", "pilot", 30*time.Second)
	if err != nil {
		t.Fatalf("NewPoller() error = %v", err)
	}

	if poller.logger == nil {
		t.Error("default logger should be set")
	}
}

func TestWithOnIssue(t *testing.T) {
	client := NewClient(testutil.FakeGitHubToken)

	called := false
	callback := func(ctx context.Context, issue *Issue) error {
		called = true
		return nil
	}

	poller, err := NewPoller(client, "owner/repo", "pilot", 30*time.Second, WithOnIssue(callback))
	if err != nil {
		t.Fatalf("NewPoller() error = %v", err)
	}

	if poller.onIssue == nil {
		t.Error("onIssue callback not set")
	}

	// Call the callback to verify it was set correctly
	_ = poller.onIssue(context.Background(), &Issue{})
	if !called {
		t.Error("callback was not called")
	}
}

func TestPoller_IsProcessed(t *testing.T) {
	client := NewClient(testutil.FakeGitHubToken)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second)

	// Initially should not be processed
	if poller.IsProcessed(42) {
		t.Error("issue should not be processed initially")
	}

	// Mark as processed
	poller.markProcessed(42)

	// Now should be processed
	if !poller.IsProcessed(42) {
		t.Error("issue should be processed after marking")
	}

	// Another issue should not be processed
	if poller.IsProcessed(43) {
		t.Error("other issues should not be processed")
	}
}

func TestPoller_ProcessedCount(t *testing.T) {
	client := NewClient(testutil.FakeGitHubToken)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second)

	if poller.ProcessedCount() != 0 {
		t.Errorf("ProcessedCount() = %d, want 0", poller.ProcessedCount())
	}

	poller.markProcessed(1)
	if poller.ProcessedCount() != 1 {
		t.Errorf("ProcessedCount() = %d, want 1", poller.ProcessedCount())
	}

	poller.markProcessed(2)
	poller.markProcessed(3)
	if poller.ProcessedCount() != 3 {
		t.Errorf("ProcessedCount() = %d, want 3", poller.ProcessedCount())
	}

	// Re-marking same issue shouldn't increase count
	poller.markProcessed(1)
	if poller.ProcessedCount() != 3 {
		t.Errorf("ProcessedCount() = %d, want 3 after re-marking", poller.ProcessedCount())
	}
}

func TestPoller_Reset(t *testing.T) {
	client := NewClient(testutil.FakeGitHubToken)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second)

	// Mark some issues
	poller.markProcessed(1)
	poller.markProcessed(2)
	poller.markProcessed(3)

	if poller.ProcessedCount() != 3 {
		t.Errorf("ProcessedCount() = %d, want 3", poller.ProcessedCount())
	}

	// Reset
	poller.Reset()

	if poller.ProcessedCount() != 0 {
		t.Errorf("ProcessedCount() after reset = %d, want 0", poller.ProcessedCount())
	}

	if poller.IsProcessed(1) {
		t.Error("issue 1 should not be processed after reset")
	}
}

func TestPoller_ClearProcessed(t *testing.T) {
	client := NewClient(testutil.FakeGitHubToken)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second)

	// Mark some issues as processed
	poller.markProcessed(1)
	poller.markProcessed(2)
	poller.markProcessed(3)

	if poller.ProcessedCount() != 3 {
		t.Errorf("ProcessedCount() = %d, want 3", poller.ProcessedCount())
	}

	// Clear single issue
	poller.ClearProcessed(2)

	if poller.ProcessedCount() != 2 {
		t.Errorf("ProcessedCount() after clear = %d, want 2", poller.ProcessedCount())
	}

	if poller.IsProcessed(2) {
		t.Error("issue 2 should not be processed after ClearProcessed")
	}
	if !poller.IsProcessed(1) {
		t.Error("issue 1 should still be processed")
	}
	if !poller.IsProcessed(3) {
		t.Error("issue 3 should still be processed")
	}
}

func TestPoller_ConcurrentAccess(t *testing.T) {
	client := NewClient(testutil.FakeGitHubToken)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second)

	var wg sync.WaitGroup
	const numGoroutines = 10
	const numOpsPerGoroutine = 100

	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for j := 0; j < numOpsPerGoroutine; j++ {
				poller.markProcessed(base*numOpsPerGoroutine + j)
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOpsPerGoroutine; j++ {
				_ = poller.IsProcessed(j)
				_ = poller.ProcessedCount()
			}
		}()
	}

	wg.Wait()

	// Should have all unique issues marked
	expectedCount := numGoroutines * numOpsPerGoroutine
	if poller.ProcessedCount() != expectedCount {
		t.Errorf("ProcessedCount() = %d, want %d", poller.ProcessedCount(), expectedCount)
	}
}

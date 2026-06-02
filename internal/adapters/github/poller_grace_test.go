package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

// --- GH-2201: Retry grace period and task checker tests ---

// mockTaskChecker implements TaskChecker for testing
type mockTaskChecker struct {
	queued map[string]bool
}

func (m *mockTaskChecker) IsTaskQueued(taskID string) bool {
	return m.queued[taskID]
}

func TestPoller_SkipsRecentlyProcessed(t *testing.T) {
	issues := []*Issue{
		{Number: 1, Title: "Issue 1", Labels: []Label{{Name: "pilot"}}},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

	var callCount int32
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithRetryGracePeriod(10*time.Minute), // Long grace period
		WithOnIssue(func(ctx context.Context, issue *Issue) error {
			atomic.AddInt32(&callCount, 1)
			return nil
		}),
	)

	// Pre-mark as processed (just now — within grace period)
	poller.markProcessed(1)

	poller.checkForNewIssues(context.Background())
	poller.WaitForActive()

	// Should NOT retry — still within grace period
	if got := atomic.LoadInt32(&callCount); got != 0 {
		t.Errorf("callback called %d times, want 0 (should skip recently processed issue)", got)
	}
	// Should still be in processed map
	if !poller.IsProcessed(1) {
		t.Error("issue should still be in processed map during grace period")
	}
}

func TestPoller_AllowsRetryAfterGrace(t *testing.T) {
	issues := []*Issue{
		{Number: 1, Title: "Issue 1", Labels: []Label{{Name: "pilot"}}},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

	var callCount int32
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithRetryGracePeriod(1*time.Millisecond), // Tiny grace period
		WithOnIssue(func(ctx context.Context, issue *Issue) error {
			atomic.AddInt32(&callCount, 1)
			return nil
		}),
	)

	// Pre-mark as processed, then wait for grace period to expire
	poller.markProcessed(1)
	time.Sleep(5 * time.Millisecond)

	poller.checkForNewIssues(context.Background())
	poller.WaitForActive()

	// Should retry — grace period expired
	if got := atomic.LoadInt32(&callCount); got != 1 {
		t.Errorf("callback called %d times, want 1 (should retry after grace period)", got)
	}
}

func TestPoller_SkipsQueuedTask(t *testing.T) {
	issues := []*Issue{
		{Number: 42, Title: "Issue 42", Labels: []Label{{Name: "pilot"}}},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

	checker := &mockTaskChecker{queued: map[string]bool{"GH-42": true}}

	var callCount int32
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithRetryGracePeriod(0), // No grace period — only task checker blocks
		WithTaskChecker(checker),
		WithOnIssue(func(ctx context.Context, issue *Issue) error {
			atomic.AddInt32(&callCount, 1)
			return nil
		}),
	)

	// Pre-mark as processed
	poller.markProcessed(42)

	poller.checkForNewIssues(context.Background())
	poller.WaitForActive()

	// Should NOT retry — task is still queued
	if got := atomic.LoadInt32(&callCount); got != 0 {
		t.Errorf("callback called %d times, want 0 (should skip queued task)", got)
	}
	// Should still be in processed map
	if !poller.IsProcessed(42) {
		t.Error("issue should still be in processed map when task is queued")
	}
}

func TestPoller_AllowsRetryCompletedTask(t *testing.T) {
	issues := []*Issue{
		{Number: 42, Title: "Issue 42", Labels: []Label{{Name: "pilot"}}},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

	checker := &mockTaskChecker{queued: map[string]bool{}} // Not queued

	var callCount int32
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithRetryGracePeriod(0), // No grace period
		WithTaskChecker(checker),
		WithOnIssue(func(ctx context.Context, issue *Issue) error {
			atomic.AddInt32(&callCount, 1)
			return nil
		}),
	)

	// Pre-mark as processed
	poller.markProcessed(42)

	poller.checkForNewIssues(context.Background())
	poller.WaitForActive()

	// Should retry — task is not queued and grace period is 0
	if got := atomic.LoadInt32(&callCount); got != 1 {
		t.Errorf("callback called %d times, want 1 (should retry completed task)", got)
	}
}

func TestPoller_ProcessedMapStoresTimestamps(t *testing.T) {
	client := NewClient(testutil.FakeGitHubToken)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second)

	before := time.Now()
	poller.markProcessed(1)
	after := time.Now()

	poller.mu.RLock()
	ts, ok := poller.processed[1]
	poller.mu.RUnlock()

	if !ok {
		t.Fatal("issue 1 should be in processed map")
	}
	if ts.Before(before) || ts.After(after) {
		t.Errorf("processed timestamp %v not between %v and %v", ts, before, after)
	}

	// Verify IsProcessed still works
	if !poller.IsProcessed(1) {
		t.Error("IsProcessed should return true")
	}
	if poller.IsProcessed(2) {
		t.Error("IsProcessed should return false for unprocessed issue")
	}

	// Verify Reset clears timestamps
	poller.Reset()
	if poller.IsProcessed(1) {
		t.Error("IsProcessed should return false after Reset")
	}
}

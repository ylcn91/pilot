package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

// GH-2276: Auto-retry issues with pilot-retry-ready (PR closed without merge)

func TestPoller_AutoRetryRetryReadyIssue_FirstRetry(t *testing.T) {
	now := time.Now()
	issues := []*Issue{
		{Number: 42, State: "open", Title: "Retry-ready issue", Labels: []Label{{Name: "pilot"}, {Name: LabelRetryReady}}, CreatedAt: now.Add(-1 * time.Hour)},
	}

	var labelRemoved atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Handle label removal
		if r.Method == http.MethodDelete && r.URL.Path == "/repos/owner/repo/issues/42/labels/"+LabelRetryReady {
			labelRemoved.Store(true)
			w.WriteHeader(http.StatusOK)
			return
		}
		// Handle search (hasMergedWork check) — no merged PRs
		if r.URL.Path == "/search/issues" {
			_, _ = w.Write([]byte(`{"total_count": 0}`))
			return
		}
		// Handle list issues
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithRetryGracePeriod(0),
	)

	issue, err := poller.findOldestUnprocessedIssue(context.Background())
	if err != nil {
		t.Fatalf("findOldestUnprocessedIssue() error = %v", err)
	}
	if issue == nil {
		t.Fatal("issue should not be nil — pilot-retry-ready issue should be retried on first attempt")
	}
	if issue.Number != 42 {
		t.Errorf("got issue #%d, want #42", issue.Number)
	}
	if !labelRemoved.Load() {
		t.Error("pilot-retry-ready label should have been removed")
	}

	// Verify retry count incremented
	poller.mu.RLock()
	retries := poller.retryReadyCount[42]
	poller.mu.RUnlock()
	if retries != 1 {
		t.Errorf("retry count = %d, want 1", retries)
	}
}

func TestPoller_AutoRetryRetryReadyIssue_RetryLimitReached(t *testing.T) {
	now := time.Now()
	// GH-2432: Retry state is now persisted via labels — issue already at
	// pilot-retry-2 means the next escalation goes to pilot-retry-exhausted
	// (no dispatch).
	issues := []*Issue{
		{Number: 42, State: "open", Title: "Retry-ready issue", Labels: []Label{{Name: "pilot"}, {Name: LabelRetryReady}, {Name: LabelRetry2}}, CreatedAt: now.Add(-1 * time.Hour)},
		{Number: 43, State: "open", Title: "Available issue", Labels: []Label{{Name: "pilot"}}, CreatedAt: now},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithMaxRetryReadyRetries(3),
	)

	issue, err := poller.findOldestUnprocessedIssue(context.Background())
	if err != nil {
		t.Fatalf("findOldestUnprocessedIssue() error = %v", err)
	}
	if issue == nil {
		t.Fatal("issue should not be nil — #43 should be picked")
	}
	if issue.Number != 43 {
		t.Errorf("got issue #%d, want #43 (should skip #42 at retry limit)", issue.Number)
	}
}

func TestPoller_AutoRetryRetryReadyIssue_SkipsDoneIssues(t *testing.T) {
	now := time.Now()
	issues := []*Issue{
		// Has both pilot-retry-ready AND pilot-done — should NOT be retried
		{Number: 42, State: "open", Title: "Done+RetryReady", Labels: []Label{{Name: "pilot"}, {Name: LabelRetryReady}, {Name: LabelDone}}, CreatedAt: now.Add(-1 * time.Hour)},
		{Number: 43, State: "open", Title: "Available", Labels: []Label{{Name: "pilot"}}, CreatedAt: now},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second)

	issue, err := poller.findOldestUnprocessedIssue(context.Background())
	if err != nil {
		t.Fatalf("findOldestUnprocessedIssue() error = %v", err)
	}
	if issue == nil {
		t.Fatal("issue should not be nil — #43 should be picked")
	}
	if issue.Number != 43 {
		t.Errorf("got issue #%d, want #43 (should skip #42 with pilot-done)", issue.Number)
	}
}

func TestPoller_AutoRetryRetryReadyIssue_SkipsClosedIssues(t *testing.T) {
	now := time.Now()
	issues := []*Issue{
		// Closed issue with pilot-retry-ready label — should NOT be retried
		{Number: 42, State: "closed", Title: "Closed+RetryReady", Labels: []Label{{Name: "pilot"}, {Name: LabelRetryReady}}, CreatedAt: now.Add(-1 * time.Hour)},
		{Number: 43, State: "open", Title: "Available", Labels: []Label{{Name: "pilot"}}, CreatedAt: now},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second)

	issue, err := poller.findOldestUnprocessedIssue(context.Background())
	if err != nil {
		t.Fatalf("findOldestUnprocessedIssue() error = %v", err)
	}
	if issue == nil {
		t.Fatal("issue should not be nil — #43 should be picked")
	}
	if issue.Number != 43 {
		t.Errorf("got issue #%d, want #43 (should skip closed #42 with pilot-retry-ready)", issue.Number)
	}
}

func TestPoller_AutoRetryRetryReadyIssue_ParallelMode(t *testing.T) {
	now := time.Now()
	issues := []*Issue{
		{Number: 42, State: "open", Title: "Retry-ready issue", Labels: []Label{{Name: "pilot"}, {Name: LabelRetryReady}}, CreatedAt: now.Add(-1 * time.Hour)},
	}

	var labelRemoved atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodDelete && r.URL.Path == "/repos/owner/repo/issues/42/labels/"+LabelRetryReady {
			labelRemoved.Store(true)
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/search/issues" {
			_, _ = w.Write([]byte(`{"total_count": 0}`))
			return
		}
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

	var processedIssues []*Issue
	var mu sync.Mutex

	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithOnIssue(func(ctx context.Context, issue *Issue) error {
			mu.Lock()
			processedIssues = append(processedIssues, issue)
			mu.Unlock()
			return nil
		}),
		WithRetryGracePeriod(0),
	)

	poller.checkForNewIssues(context.Background())
	poller.WaitForActive()

	mu.Lock()
	got := len(processedIssues)
	mu.Unlock()

	if got != 1 {
		t.Errorf("processed %d issues, want 1", got)
	}
	if !labelRemoved.Load() {
		t.Error("pilot-retry-ready label should have been removed in parallel mode")
	}
}

func TestPoller_WithMaxRetryReadyRetries(t *testing.T) {
	client := NewClientWithBaseURL(testutil.FakeGitHubToken, "http://localhost")
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithMaxRetryReadyRetries(5),
	)
	if poller.maxRetryReadyRetries != 5 {
		t.Errorf("maxRetryReadyRetries = %d, want 5", poller.maxRetryReadyRetries)
	}

	// Negative values should be clamped to 0
	poller2, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithMaxRetryReadyRetries(-1),
	)
	if poller2.maxRetryReadyRetries != 0 {
		t.Errorf("maxRetryReadyRetries = %d, want 0 (clamped from -1)", poller2.maxRetryReadyRetries)
	}
}

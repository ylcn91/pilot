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

func TestPoller_AutoRetryFailedIssue_FirstFailure(t *testing.T) {
	now := time.Now()
	issues := []*Issue{
		{Number: 42, State: "open", Title: "Stuck issue", Labels: []Label{{Name: "pilot"}, {Name: LabelFailed}}, CreatedAt: now.Add(-1 * time.Hour)},
	}

	var labelRemoved atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Handle label removal
		if r.Method == http.MethodDelete && r.URL.Path == "/repos/owner/repo/issues/42/labels/"+LabelFailed {
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
		t.Fatal("issue should not be nil — pilot-failed issue should be retried on first failure")
	}
	if issue.Number != 42 {
		t.Errorf("got issue #%d, want #42", issue.Number)
	}
	if !labelRemoved.Load() {
		t.Error("pilot-failed label should have been removed")
	}

	// Verify retry count incremented
	poller.mu.RLock()
	retries := poller.dispatch.failedRetryCount[42]
	poller.mu.RUnlock()
	if retries != 1 {
		t.Errorf("retry count = %d, want 1", retries)
	}
}

func TestPoller_AutoRetryFailedIssue_RetryLimitReached(t *testing.T) {
	now := time.Now()
	issues := []*Issue{
		{Number: 42, State: "open", Title: "Stuck issue", Labels: []Label{{Name: "pilot"}, {Name: LabelFailed}}, CreatedAt: now.Add(-1 * time.Hour)},
		{Number: 43, State: "open", Title: "Available issue", Labels: []Label{{Name: "pilot"}}, CreatedAt: now},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithMaxFailedRetries(3),
	)

	// Simulate: already retried 3 times
	poller.mu.Lock()
	poller.dispatch.failedRetryCount[42] = 3
	poller.mu.Unlock()

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

func TestPoller_AutoRetryFailedIssue_SkipsDoneIssues(t *testing.T) {
	now := time.Now()
	issues := []*Issue{
		// Has both pilot-failed AND pilot-done — should NOT be retried
		{Number: 42, State: "open", Title: "Done+Failed", Labels: []Label{{Name: "pilot"}, {Name: LabelFailed}, {Name: LabelDone}}, CreatedAt: now.Add(-1 * time.Hour)},
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

func TestPoller_AutoRetryFailedIssue_SkipsClosedIssues(t *testing.T) {
	now := time.Now()
	issues := []*Issue{
		// Closed issue with stale pilot-failed label — should NOT be retried (GH-2252)
		{Number: 42, State: "closed", Title: "Closed+Failed", Labels: []Label{{Name: "pilot"}, {Name: LabelFailed}}, CreatedAt: now.Add(-1 * time.Hour)},
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
		t.Errorf("got issue #%d, want #43 (should skip closed #42 with pilot-failed)", issue.Number)
	}
}

func TestPoller_AutoRetryFailedIssue_ParallelMode(t *testing.T) {
	now := time.Now()
	issues := []*Issue{
		{Number: 42, State: "open", Title: "Stuck issue", Labels: []Label{{Name: "pilot"}, {Name: LabelFailed}}, CreatedAt: now.Add(-1 * time.Hour)},
	}

	var labelRemoved atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodDelete && r.URL.Path == "/repos/owner/repo/issues/42/labels/"+LabelFailed {
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
		t.Error("pilot-failed label should have been removed in parallel mode")
	}
}

func TestPoller_AutoRetryFailedIssue_ParallelMode_LimitReached(t *testing.T) {
	now := time.Now()
	issues := []*Issue{
		{Number: 42, State: "open", Title: "Stuck issue", Labels: []Label{{Name: "pilot"}, {Name: LabelFailed}}, CreatedAt: now.Add(-1 * time.Hour)},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

	var processedCount atomic.Int32
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithOnIssue(func(ctx context.Context, issue *Issue) error {
			processedCount.Add(1)
			return nil
		}),
		WithMaxFailedRetries(2),
	)

	// Simulate: already at max retries
	poller.mu.Lock()
	poller.dispatch.failedRetryCount[42] = 2
	poller.mu.Unlock()

	poller.checkForNewIssues(context.Background())
	poller.WaitForActive()

	if processedCount.Load() != 0 {
		t.Errorf("processed %d issues, want 0 (should skip at retry limit)", processedCount.Load())
	}
}

// GH-2768: Skip issues with pilot-needs-clarification label

func TestPoller_SkipsNeedsClarification(t *testing.T) {
	now := time.Now()
	issues := []*Issue{
		// Has pilot-needs-clarification — should be skipped until label is removed
		{Number: 42, State: "open", Title: "Declined issue", Labels: []Label{{Name: "pilot"}, {Name: LabelNeedsClarification}}, CreatedAt: now.Add(-1 * time.Hour)},
		{Number: 43, State: "open", Title: "Available issue", Labels: []Label{{Name: "pilot"}}, CreatedAt: now},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second)

	// Test findOldestUnprocessedIssue skips #42
	issue, err := poller.findOldestUnprocessedIssue(context.Background())
	if err != nil {
		t.Fatalf("findOldestUnprocessedIssue() error = %v", err)
	}
	if issue == nil {
		t.Fatal("issue should not be nil — #43 should be picked")
	}
	if issue.Number != 43 {
		t.Errorf("findOldestUnprocessedIssue: got issue #%d, want #43 (should skip #42 with pilot-needs-clarification)", issue.Number)
	}

	// Test checkForNewIssues (parallel mode) also skips #42
	var processedIssues []*Issue
	var mu sync.Mutex
	poller2, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithOnIssue(func(ctx context.Context, iss *Issue) error {
			mu.Lock()
			processedIssues = append(processedIssues, iss)
			mu.Unlock()
			return nil
		}),
	)
	poller2.checkForNewIssues(context.Background())
	poller2.WaitForActive()

	mu.Lock()
	got := processedIssues
	mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("checkForNewIssues: dispatched %d issues, want 1", len(got))
	}
	if got[0].Number != 43 {
		t.Errorf("checkForNewIssues: dispatched issue #%d, want #43", got[0].Number)
	}
}

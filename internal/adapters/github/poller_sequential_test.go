package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

// Tests for sequential execution mode

func TestWithExecutionMode(t *testing.T) {
	client := NewClient(testutil.FakeGitHubToken)

	t.Run("sequential mode", func(t *testing.T) {
		poller, err := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
			WithExecutionMode(ExecutionModeSequential),
		)
		if err != nil {
			t.Fatalf("NewPoller() error = %v", err)
		}
		if poller.executionMode != ExecutionModeSequential {
			t.Errorf("executionMode = %v, want %v", poller.executionMode, ExecutionModeSequential)
		}
	})

	t.Run("parallel mode", func(t *testing.T) {
		poller, err := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
			WithExecutionMode(ExecutionModeParallel),
		)
		if err != nil {
			t.Fatalf("NewPoller() error = %v", err)
		}
		if poller.executionMode != ExecutionModeParallel {
			t.Errorf("executionMode = %v, want %v", poller.executionMode, ExecutionModeParallel)
		}
	})

	t.Run("auto mode", func(t *testing.T) {
		poller, err := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
			WithExecutionMode(ExecutionModeAuto),
		)
		if err != nil {
			t.Fatalf("NewPoller() error = %v", err)
		}
		if poller.executionMode != ExecutionModeAuto {
			t.Errorf("executionMode = %v, want %v", poller.executionMode, ExecutionModeAuto)
		}
	})

	t.Run("default is auto matching config default", func(t *testing.T) {
		poller, err := NewPoller(client, "owner/repo", "pilot", 30*time.Second)
		if err != nil {
			t.Fatalf("NewPoller() error = %v", err)
		}
		if poller.executionMode != ExecutionModeAuto {
			t.Errorf("default executionMode = %v, want %v", poller.executionMode, ExecutionModeAuto)
		}
	})
}

func TestWithSequentialConfig(t *testing.T) {
	client := NewClient(testutil.FakeGitHubToken)

	poller, err := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithExecutionMode(ExecutionModeSequential),
		WithSequentialConfig(true, 15*time.Second, 2*time.Hour),
	)
	if err != nil {
		t.Fatalf("NewPoller() error = %v", err)
	}

	if !poller.waitForMerge {
		t.Error("waitForMerge should be true")
	}
	if poller.prPollInterval != 15*time.Second {
		t.Errorf("prPollInterval = %v, want 15s", poller.prPollInterval)
	}
	if poller.prTimeout != 2*time.Hour {
		t.Errorf("prTimeout = %v, want 2h", poller.prTimeout)
	}
	if poller.mergeWaiter == nil {
		t.Error("mergeWaiter should be created in sequential mode with waitForMerge")
	}
}

func TestWithOnIssueWithResult(t *testing.T) {
	client := NewClient(testutil.FakeGitHubToken)

	called := false
	callback := func(ctx context.Context, issue *Issue) (*IssueResult, error) {
		called = true
		return &IssueResult{
			Success:  true,
			PRNumber: 42,
			PRURL:    "https://github.com/owner/repo/pull/42",
		}, nil
	}

	poller, err := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithOnIssueWithResult(callback),
	)
	if err != nil {
		t.Fatalf("NewPoller() error = %v", err)
	}

	if poller.onIssueWithResult == nil {
		t.Error("onIssueWithResult callback not set")
	}

	// Call the callback to verify it was set correctly
	result, _ := poller.onIssueWithResult(context.Background(), &Issue{})
	if !called {
		t.Error("callback was not called")
	}
	if result.PRNumber != 42 {
		t.Errorf("PRNumber = %d, want 42", result.PRNumber)
	}
}

func TestPoller_FindOldestUnprocessedIssue(t *testing.T) {
	now := time.Now()
	issues := []*Issue{
		{Number: 3, Title: "Newest", Labels: []Label{{Name: "pilot"}}, CreatedAt: now},
		{Number: 1, Title: "Oldest", Labels: []Label{{Name: "pilot"}}, CreatedAt: now.Add(-2 * time.Hour)},
		{Number: 2, Title: "Middle", Labels: []Label{{Name: "pilot"}}, CreatedAt: now.Add(-1 * time.Hour)},
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
		t.Fatal("issue should not be nil")
	}
	if issue.Number != 1 {
		t.Errorf("found issue #%d, want #1 (oldest)", issue.Number)
	}
}

func TestPoller_FindOldestUnprocessedIssue_SkipsProcessedWithDoneLabel(t *testing.T) {
	now := time.Now()
	issues := []*Issue{
		{Number: 1, Title: "Oldest Done", Labels: []Label{{Name: "pilot"}, {Name: LabelDone}}, CreatedAt: now.Add(-2 * time.Hour)},
		{Number: 2, Title: "Second", Labels: []Label{{Name: "pilot"}}, CreatedAt: now.Add(-1 * time.Hour)},
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
		t.Fatal("issue should not be nil")
	}
	if issue.Number != 2 {
		t.Errorf("found issue #%d, want #2 (oldest without status label)", issue.Number)
	}
}

func TestPoller_FindOldestUnprocessedIssue_AllowsRetryWhenFailedLabelRemoved(t *testing.T) {
	now := time.Now()
	issues := []*Issue{
		{Number: 1, Title: "Was Failed", Labels: []Label{{Name: "pilot"}}, CreatedAt: now.Add(-2 * time.Hour)},
		{Number: 2, Title: "Second", Labels: []Label{{Name: "pilot"}}, CreatedAt: now.Add(-1 * time.Hour)},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithRetryGracePeriod(0), // GH-2201: disable grace period for test
	)

	// Simulate: issue was processed (failed) but pilot-failed label was removed
	poller.markProcessed(1)

	issue, err := poller.findOldestUnprocessedIssue(context.Background())

	if err != nil {
		t.Fatalf("findOldestUnprocessedIssue() error = %v", err)
	}
	if issue == nil {
		t.Fatal("issue should not be nil - should allow retry")
	}
	// Should return #1 because pilot-failed was removed, allowing retry
	if issue.Number != 1 {
		t.Errorf("found issue #%d, want #1 (should retry after pilot-failed removed)", issue.Number)
	}
	// Verify it was removed from processed map
	if poller.IsProcessed(1) {
		t.Error("issue #1 should no longer be marked as processed")
	}
}

func TestPoller_FindOldestUnprocessedIssue_SkipsInProgress(t *testing.T) {
	now := time.Now()
	issues := []*Issue{
		{Number: 1, Title: "In Progress", Labels: []Label{{Name: "pilot"}, {Name: LabelInProgress}}, CreatedAt: now.Add(-2 * time.Hour)},
		{Number: 2, Title: "Available", Labels: []Label{{Name: "pilot"}}, CreatedAt: now.Add(-1 * time.Hour)},
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
		t.Fatal("issue should not be nil")
	}
	if issue.Number != 2 {
		t.Errorf("found issue #%d, want #2 (skips in-progress)", issue.Number)
	}
}

func TestPoller_FindOldestUnprocessedIssue_ReturnsNilWhenEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]*Issue{})
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second)

	issue, err := poller.findOldestUnprocessedIssue(context.Background())

	if err != nil {
		t.Fatalf("findOldestUnprocessedIssue() error = %v", err)
	}
	if issue != nil {
		t.Error("issue should be nil when no unprocessed issues")
	}
}

func TestPoller_ProcessIssueSequential_UsesResultCallback(t *testing.T) {
	client := NewClient(testutil.FakeGitHubToken)

	expectedResult := &IssueResult{
		Success:  true,
		PRNumber: 99,
		PRURL:    "https://github.com/owner/repo/pull/99",
	}

	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithOnIssueWithResult(func(ctx context.Context, issue *Issue) (*IssueResult, error) {
			return expectedResult, nil
		}),
	)

	result, err := poller.processIssueSequential(context.Background(), &Issue{Number: 1})

	if err != nil {
		t.Fatalf("processIssueSequential() error = %v", err)
	}
	if result.PRNumber != 99 {
		t.Errorf("PRNumber = %d, want 99", result.PRNumber)
	}
}

func TestPoller_ProcessIssueSequential_FallsBackToLegacyCallback(t *testing.T) {
	client := NewClient(testutil.FakeGitHubToken)

	legacyCalled := false
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithOnIssue(func(ctx context.Context, issue *Issue) error {
			legacyCalled = true
			return nil
		}),
	)

	result, err := poller.processIssueSequential(context.Background(), &Issue{Number: 1})

	if err != nil {
		t.Fatalf("processIssueSequential() error = %v", err)
	}
	if !legacyCalled {
		t.Error("legacy callback should be called")
	}
	if !result.Success {
		t.Error("result.Success should be true")
	}
	// No PR info from legacy callback
	if result.PRNumber != 0 {
		t.Errorf("PRNumber = %d, want 0 (legacy callback doesn't return PR)", result.PRNumber)
	}
}

func TestPoller_StartSequential_ProcessesOneAtATime(t *testing.T) {
	now := time.Now()
	issues := []*Issue{
		{Number: 1, Title: "First", Labels: []Label{{Name: "pilot"}}, CreatedAt: now.Add(-1 * time.Hour)},
		{Number: 2, Title: "Second", Labels: []Label{{Name: "pilot"}}, CreatedAt: now},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

	processedOrder := []int{}
	var mu sync.Mutex

	poller, _ := NewPoller(client, "owner/repo", "pilot", 10*time.Millisecond,
		WithExecutionMode(ExecutionModeSequential),
		WithSequentialConfig(false, 10*time.Millisecond, 100*time.Millisecond), // No merge waiting
		WithOnIssueWithResult(func(ctx context.Context, issue *Issue) (*IssueResult, error) {
			mu.Lock()
			processedOrder = append(processedOrder, issue.Number)
			mu.Unlock()
			return &IssueResult{Success: true}, nil
		}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go poller.Start(ctx)

	// Wait for processing
	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// Should process oldest first (issue #1)
	if len(processedOrder) < 1 {
		t.Fatal("should have processed at least one issue")
	}
	if processedOrder[0] != 1 {
		t.Errorf("first processed issue = %d, want 1 (oldest)", processedOrder[0])
	}
}

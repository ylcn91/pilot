package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestPoller_CheckForNewIssues_SkipsRetryWithMergedPRs(t *testing.T) {
	issues := []*Issue{
		{Number: 1, Title: "GH-1 feature", Labels: []Label{{Name: "pilot"}}},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/owner/repo/issues":
			_ = json.NewEncoder(w).Encode(issues)
		case "/search/issues":
			_, _ = w.Write([]byte(`{"total_count": 1}`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

	var callCount int32
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithRetryGracePeriod(0), // GH-2201: disable grace period for test
		WithOnIssue(func(ctx context.Context, issue *Issue) error {
			atomic.AddInt32(&callCount, 1)
			return nil
		}),
	)

	// Pre-mark as processed (simulating previous failed attempt)
	poller.markProcessed(1)

	poller.checkForNewIssues(context.Background())
	poller.WaitForActive()

	// Should NOT retry since merged PRs exist
	if got := atomic.LoadInt32(&callCount); got != 0 {
		t.Errorf("callback called %d times, want 0 (should skip issue with merged PRs)", got)
	}
}

func TestPoller_FindOldestUnprocessedIssue_SkipsRetryWithMergedPRs(t *testing.T) {
	issues := []*Issue{
		{
			Number:    1,
			Title:     "GH-1 feature",
			Labels:    []Label{{Name: "pilot"}},
			CreatedAt: time.Now().Add(-1 * time.Hour),
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/owner/repo/issues":
			_ = json.NewEncoder(w).Encode(issues)
		case "/search/issues":
			_, _ = w.Write([]byte(`{"total_count": 3}`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithExecutionMode(ExecutionModeSequential),
		WithRetryGracePeriod(0), // GH-2201: disable grace period for test
	)

	// Pre-mark as processed (simulating previous failed attempt)
	poller.markProcessed(1)

	issue, err := poller.findOldestUnprocessedIssue(context.Background())
	if err != nil {
		t.Fatalf("findOldestUnprocessedIssue() error = %v", err)
	}
	if issue != nil {
		t.Errorf("findOldestUnprocessedIssue() returned issue %d, want nil (should skip issue with merged PRs)", issue.Number)
	}
}

func TestPoller_CheckForNewIssues_SkipsPullRequests(t *testing.T) {
	pr := &struct{}{}
	items := []*Issue{
		{Number: 1, Title: "Real issue", Labels: []Label{{Name: "pilot"}}},
		{Number: 2, Title: "A pull request", Labels: []Label{{Name: "pilot"}}, PullRequest: pr},
		{Number: 3, Title: "Another issue", Labels: []Label{{Name: "pilot"}}},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(items)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

	var dispatched []int
	var mu sync.Mutex

	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithOnIssue(func(ctx context.Context, issue *Issue) error {
			mu.Lock()
			dispatched = append(dispatched, issue.Number)
			mu.Unlock()
			return nil
		}),
	)

	poller.checkForNewIssues(context.Background())
	poller.WaitForActive()

	mu.Lock()
	defer mu.Unlock()

	if len(dispatched) != 2 {
		t.Fatalf("dispatched %d issues, want 2 (PRs should be skipped)", len(dispatched))
	}

	for _, num := range dispatched {
		if num == 2 {
			t.Errorf("PR (issue #2) should not have been dispatched")
		}
	}
}

func TestPoller_FindOldestUnprocessedIssue_SkipsPullRequests(t *testing.T) {
	pr := &struct{}{}
	now := time.Now()
	items := []*Issue{
		{Number: 10, Title: "A pull request", Labels: []Label{{Name: "pilot"}}, PullRequest: pr, CreatedAt: now.Add(-2 * time.Hour)},
		{Number: 20, Title: "Real issue", Labels: []Label{{Name: "pilot"}}, CreatedAt: now.Add(-1 * time.Hour)},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(items)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second)

	issue, err := poller.findOldestUnprocessedIssue(context.Background())
	if err != nil {
		t.Fatalf("findOldestUnprocessedIssue() error = %v", err)
	}
	if issue == nil {
		t.Fatal("expected an issue, got nil")
	}
	if issue.Number != 20 {
		t.Errorf("got issue #%d, want #20 (PR #10 should be skipped)", issue.Number)
	}
}

// mockExecutionChecker implements ExecutionChecker for testing.
type mockExecutionChecker struct {
	completed map[string]bool // key: "taskID:projectPath"
}

func (m *mockExecutionChecker) HasCompletedExecution(taskID, projectPath string) (bool, error) {
	return m.completed[taskID+":"+projectPath], nil
}

func TestPoller_SkipsCompletedExecution(t *testing.T) {
	// GH-2242: Issue is open, no pilot-done label, but has completed execution — should NOT dispatch
	issues := []*Issue{
		{Number: 42, Title: "Issue 42", Labels: []Label{{Name: "pilot"}}},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/issues") {
			_ = json.NewEncoder(w).Encode(issues)
			return
		}
		_, _ = w.Write([]byte("[]"))
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

	execChecker := &mockExecutionChecker{
		completed: map[string]bool{
			"GH-42:/project": true,
		},
	}

	var callCount int32
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithExecutionChecker(execChecker, "/project"),
		WithOnIssue(func(ctx context.Context, issue *Issue) error {
			atomic.AddInt32(&callCount, 1)
			return nil
		}),
	)

	poller.checkForNewIssues(context.Background())
	poller.WaitForActive()

	if got := atomic.LoadInt32(&callCount); got != 0 {
		t.Errorf("callback called %d times, want 0 (completed execution should skip dispatch)", got)
	}

	// Should be marked as processed
	if !poller.IsProcessed(42) {
		t.Error("completed issue should be marked as processed")
	}
}

func TestPoller_DispatchesWhenNoCompletedExecution(t *testing.T) {
	// GH-2242: Issue has no completed execution — should dispatch normally
	issues := []*Issue{
		{Number: 99, Title: "Issue 99", Labels: []Label{{Name: "pilot"}}},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/issues") {
			_ = json.NewEncoder(w).Encode(issues)
			return
		}
		_, _ = w.Write([]byte("[]"))
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

	execChecker := &mockExecutionChecker{
		completed: map[string]bool{}, // No completed executions
	}

	var callCount int32
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithExecutionChecker(execChecker, "/project"),
		WithOnIssue(func(ctx context.Context, issue *Issue) error {
			atomic.AddInt32(&callCount, 1)
			return nil
		}),
	)

	poller.checkForNewIssues(context.Background())
	poller.WaitForActive()

	if got := atomic.LoadInt32(&callCount); got != 1 {
		t.Errorf("callback called %d times, want 1 (should dispatch when no completed execution)", got)
	}
}

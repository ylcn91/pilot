package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/memory"
	"github.com/ylcn91/pilot/internal/testutil"
)

func TestCleaner_Cleanup_StaleFailedLabelsRemoved(t *testing.T) {
	store := createTestStore(t)
	defer func() { _ = store.Close() }()

	// Create stale failed issue (updated 25 hours ago - over 24h threshold)
	staleTime := time.Now().Add(-25 * time.Hour)
	// Note: ListIssues does post-fetch filtering (client filters by label after API call)
	// So we return all issues with any label, and the client filters by label name
	allIssues := []*Issue{
		{
			Number:    456,
			Title:     "Stale Failed Issue",
			Labels:    []Label{{Name: LabelFailed}},
			UpdatedAt: staleTime,
		},
	}

	var removeLabelCalled bool
	var addCommentCalled bool
	var onFailedCleanedCalled bool
	var cleanedIssueNumber int
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		w.Header().Set("Content-Type", "application/json")

		// List issues endpoint - return all issues (client filters by label)
		if r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/issues" {
			_ = json.NewEncoder(w).Encode(allIssues)
			return
		}

		// Remove label endpoint
		if r.Method == http.MethodDelete && r.URL.Path == "/repos/owner/repo/issues/456/labels/"+LabelFailed {
			removeLabelCalled = true
			w.WriteHeader(http.StatusOK)
			return
		}

		// Add comment endpoint
		if r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/issues/456/comments" {
			addCommentCalled = true
			_ = json.NewEncoder(w).Encode(&Comment{ID: 1, Body: "cleanup"})
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cleaner, _ := NewCleaner(client, store, "owner/repo", &StaleLabelCleanupConfig{
		Enabled:         true,
		Interval:        30 * time.Minute,
		Threshold:       1 * time.Hour,
		FailedThreshold: 24 * time.Hour,
	}, WithOnFailedCleaned(func(issueNumber int) {
		mu.Lock()
		onFailedCleanedCalled = true
		cleanedIssueNumber = issueNumber
		mu.Unlock()
	}))

	err := cleaner.Cleanup(context.Background())
	if err != nil {
		t.Errorf("Cleanup() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if !removeLabelCalled {
		t.Error("RemoveLabel should have been called for stale failed issue")
	}
	if !addCommentCalled {
		t.Error("AddComment should have been called for stale failed issue")
	}
	if !onFailedCleanedCalled {
		t.Error("OnFailedCleaned callback should have been called")
	}
	if cleanedIssueNumber != 456 {
		t.Errorf("OnFailedCleaned called with issue %d, want 456", cleanedIssueNumber)
	}
}

// GH-2402: Cleaner removes stale pilot-blocked labels and fires OnBlockedCleaned
// so the poller can clear the issue from its processed map.
func TestCleaner_Cleanup_StaleBlockedLabelsRemoved(t *testing.T) {
	store := createTestStore(t)
	defer func() { _ = store.Close() }()

	staleTime := time.Now().Add(-25 * time.Hour)
	allIssues := []*Issue{
		{
			Number:    789,
			Title:     "Stale Blocked Issue",
			Labels:    []Label{{Name: LabelBlocked}},
			UpdatedAt: staleTime,
		},
	}

	var (
		mu                     sync.Mutex
		removeLabelCalled      bool
		addCommentCalled       bool
		onBlockedCleanedCalled bool
		cleanedIssueNumber     int
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/issues" {
			_ = json.NewEncoder(w).Encode(allIssues)
			return
		}
		if r.Method == http.MethodDelete && r.URL.Path == "/repos/owner/repo/issues/789/labels/"+LabelBlocked {
			removeLabelCalled = true
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/issues/789/comments" {
			addCommentCalled = true
			_ = json.NewEncoder(w).Encode(&Comment{ID: 1, Body: "cleanup"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cleaner, _ := NewCleaner(client, store, "owner/repo", &StaleLabelCleanupConfig{
		Enabled:         true,
		Interval:        30 * time.Minute,
		Threshold:       1 * time.Hour,
		FailedThreshold: 24 * time.Hour,
	}, WithOnBlockedCleaned(func(issueNumber int) {
		mu.Lock()
		onBlockedCleanedCalled = true
		cleanedIssueNumber = issueNumber
		mu.Unlock()
	}))

	if err := cleaner.Cleanup(context.Background()); err != nil {
		t.Errorf("Cleanup() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !removeLabelCalled {
		t.Error("RemoveLabel should have been called for stale blocked issue")
	}
	if !addCommentCalled {
		t.Error("AddComment should have been called for stale blocked issue")
	}
	if !onBlockedCleanedCalled {
		t.Error("OnBlockedCleaned callback should have been called")
	}
	if cleanedIssueNumber != 789 {
		t.Errorf("OnBlockedCleaned called with issue %d, want 789", cleanedIssueNumber)
	}
}

func TestCleaner_Cleanup_RecentFailedIssuesSkipped(t *testing.T) {
	store := createTestStore(t)
	defer func() { _ = store.Close() }()

	// Create recent failed issue (updated 12 hours ago - under 24h threshold)
	recentTime := time.Now().Add(-12 * time.Hour)
	allIssues := []*Issue{
		{
			Number:    789,
			Title:     "Recent Failed Issue",
			Labels:    []Label{{Name: LabelFailed}},
			UpdatedAt: recentTime,
		},
	}

	removeLabelCalled := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// List issues endpoint - return all issues (client filters by label)
		if r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/issues" {
			_ = json.NewEncoder(w).Encode(allIssues)
			return
		}

		// Remove label endpoint - should not be called
		if r.Method == http.MethodDelete {
			removeLabelCalled = true
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cleaner, _ := NewCleaner(client, store, "owner/repo", &StaleLabelCleanupConfig{
		Enabled:         true,
		Interval:        30 * time.Minute,
		Threshold:       1 * time.Hour,
		FailedThreshold: 24 * time.Hour,
	})

	err := cleaner.Cleanup(context.Background())
	if err != nil {
		t.Errorf("Cleanup() error = %v", err)
	}

	if removeLabelCalled {
		t.Error("RemoveLabel should NOT have been called for recent failed issue")
	}
}

// GH-2354: pilot-in-progress label on externally-closed issues should be
// cleaned up immediately, and the dashboard monitor callback should fire.
func TestCleaner_Cleanup_ClosedInProgressIssueCleaned(t *testing.T) {
	store := createTestStore(t)
	defer func() { _ = store.Close() }()

	closedIssues := []*Issue{
		{
			Number:    2348,
			Title:     "Externally closed issue",
			Labels:    []Label{{Name: LabelInProgress}},
			State:     StateClosed,
			UpdatedAt: time.Now(), // recent — threshold must be ignored for closed
		},
	}

	var (
		mu              sync.Mutex
		removeLabelHit  bool
		sawStateClosed  bool
		commentOnClosed bool
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/issues" {
			state := r.URL.Query().Get("state")
			if state == "closed" {
				sawStateClosed = true
				_ = json.NewEncoder(w).Encode(closedIssues)
				return
			}
			_ = json.NewEncoder(w).Encode([]*Issue{})
			return
		}

		if r.Method == http.MethodDelete && r.URL.Path == "/repos/owner/repo/issues/2348/labels/"+LabelInProgress {
			removeLabelHit = true
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/issues/2348/comments" {
			commentOnClosed = true
			_ = json.NewEncoder(w).Encode(&Comment{ID: 1})
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	var callbackIssue int
	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cleaner, _ := NewCleaner(client, store, "owner/repo", &StaleLabelCleanupConfig{
		Enabled:   true,
		Interval:  30 * time.Minute,
		Threshold: 1 * time.Hour,
	}, WithOnInProgressCleaned(func(n int) { callbackIssue = n }))

	if err := cleaner.Cleanup(context.Background()); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if !sawStateClosed {
		t.Error("expected ListIssues to be called with state=closed")
	}
	if !removeLabelHit {
		t.Error("RemoveLabel should have been called for closed in-progress issue")
	}
	if commentOnClosed {
		t.Error("AddComment must NOT be called for closed-issue cleanup (silent)")
	}
	if callbackIssue != 2348 {
		t.Errorf("OnInProgressCleaned callback issue = %d, want 2348", callbackIssue)
	}
}

// GH-2354: active executions for closed issues must NOT have the label stripped
// while the task is still running in-memory.
func TestCleaner_Cleanup_ClosedInProgressWithActiveExecutionSkipped(t *testing.T) {
	store := createTestStore(t)
	defer func() { _ = store.Close() }()

	if err := store.SaveExecution(&memory.Execution{
		ID:          "exec-2351",
		TaskID:      "GH-2351",
		ProjectPath: "/test/project",
		Status:      "running",
	}); err != nil {
		t.Fatalf("SaveExecution: %v", err)
	}

	closedIssues := []*Issue{
		{Number: 2351, Title: "Closed but still running", Labels: []Label{{Name: LabelInProgress}}, State: StateClosed, UpdatedAt: time.Now()},
	}

	removeLabelHit := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/issues" {
			if r.URL.Query().Get("state") == "closed" {
				_ = json.NewEncoder(w).Encode(closedIssues)
				return
			}
			_ = json.NewEncoder(w).Encode([]*Issue{})
			return
		}
		if r.Method == http.MethodDelete {
			removeLabelHit = true
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	callbackFired := false
	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cleaner, _ := NewCleaner(client, store, "owner/repo", &StaleLabelCleanupConfig{
		Enabled: true, Interval: 30 * time.Minute, Threshold: 1 * time.Hour,
	}, WithOnInProgressCleaned(func(int) { callbackFired = true }))

	if err := cleaner.Cleanup(context.Background()); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}

	if removeLabelHit {
		t.Error("RemoveLabel must NOT be called for closed issue with active execution")
	}
	if callbackFired {
		t.Error("OnInProgressCleaned must NOT fire for closed issue with active execution")
	}
}

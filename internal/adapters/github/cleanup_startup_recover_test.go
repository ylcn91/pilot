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

// TestCleaner_StartupRecover_StuckIssueUnlabeled verifies that an open issue
// carrying pilot-in-progress with no live execution row gets its label stripped.
func TestCleaner_StartupRecover_StuckIssueUnlabeled(t *testing.T) {
	store := createTestStore(t)
	defer func() { _ = store.Close() }()

	// No execution rows — simulates daemon restart after crash.
	issues := []*Issue{
		{
			Number:    2564,
			Title:     "Stuck issue",
			State:     StateOpen,
			Labels:    []Label{{Name: LabelInProgress}},
			UpdatedAt: time.Now().Add(-45 * time.Minute),
		},
	}

	var removeLabelCalled bool
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/issues" {
			_ = json.NewEncoder(w).Encode(issues)
			return
		}
		if r.Method == http.MethodDelete && r.URL.Path == "/repos/owner/repo/issues/2564/labels/"+LabelInProgress {
			removeLabelCalled = true
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	var callbackIssue int
	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cleaner, _ := NewCleaner(client, store, "owner/repo", &StaleLabelCleanupConfig{
		Enabled: true, Interval: 30 * time.Minute, Threshold: 1 * time.Hour,
	}, WithOnStartupRecovered(func(n int) { callbackIssue = n }))

	n, err := cleaner.StartupRecover(context.Background())
	if err != nil {
		t.Fatalf("StartupRecover() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if !removeLabelCalled {
		t.Error("RemoveLabel should have been called for stuck issue")
	}
	if n != 1 {
		t.Errorf("StartupRecover() returned %d, want 1", n)
	}
	if callbackIssue != 2564 {
		t.Errorf("OnStartupRecovered called with %d, want 2564", callbackIssue)
	}
}

// TestCleaner_StartupRecover_LiveExecutionSkipped verifies that an issue with
// a running execution row created < 30 minutes ago is left alone.
func TestCleaner_StartupRecover_LiveExecutionSkipped(t *testing.T) {
	store := createTestStore(t)
	defer func() { _ = store.Close() }()

	// Recent running execution (created_at defaults to now) — daemon may have just restarted.
	if err := store.SaveExecution(&memory.Execution{
		ID:          "exec-live",
		TaskID:      "GH-2589",
		ProjectPath: "/test/project",
		Status:      "running",
	}); err != nil {
		t.Fatalf("SaveExecution: %v", err)
	}

	issues := []*Issue{
		{
			Number:    2589,
			Title:     "In-flight issue",
			State:     StateOpen,
			Labels:    []Label{{Name: LabelInProgress}},
			UpdatedAt: time.Now().Add(-5 * time.Minute),
		},
	}

	removeLabelCalled := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/issues" {
			_ = json.NewEncoder(w).Encode(issues)
			return
		}
		if r.Method == http.MethodDelete {
			removeLabelCalled = true
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cleaner, _ := NewCleaner(client, store, "owner/repo", &StaleLabelCleanupConfig{
		Enabled: true, Interval: 30 * time.Minute, Threshold: 1 * time.Hour,
	})

	n, err := cleaner.StartupRecover(context.Background())
	if err != nil {
		t.Fatalf("StartupRecover() error = %v", err)
	}
	if removeLabelCalled {
		t.Error("RemoveLabel must NOT be called for issue with live execution")
	}
	if n != 0 {
		t.Errorf("StartupRecover() returned %d, want 0", n)
	}
}

// TestCleaner_StartupRecover_StaleExecutionStripped verifies that an issue with
// a running execution row created > 30 minutes ago (orphaned row) is still
// treated as stuck and gets its label stripped.
func TestCleaner_StartupRecover_StaleExecutionStripped(t *testing.T) {
	store := createTestStore(t)
	defer func() { _ = store.Close() }()

	if err := store.SaveExecution(&memory.Execution{
		ID:          "exec-stale",
		TaskID:      "GH-2564",
		ProjectPath: "/test/project",
		Status:      "running",
	}); err != nil {
		t.Fatalf("SaveExecution: %v", err)
	}

	// Backdate created_at so the row appears > 30 minutes old.
	if _, err := store.DB().Exec(
		`UPDATE executions SET created_at = datetime('now', '-3 hours') WHERE id = 'exec-stale'`,
	); err != nil {
		t.Fatalf("backdate created_at: %v", err)
	}

	issues := []*Issue{
		{
			Number:    2564,
			Title:     "Long-stuck issue",
			State:     StateOpen,
			Labels:    []Label{{Name: LabelInProgress}},
			UpdatedAt: time.Now().Add(-3 * time.Hour),
		},
	}

	var removeLabelCalled bool
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/issues" {
			_ = json.NewEncoder(w).Encode(issues)
			return
		}
		if r.Method == http.MethodDelete && r.URL.Path == "/repos/owner/repo/issues/2564/labels/"+LabelInProgress {
			removeLabelCalled = true
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cleaner, _ := NewCleaner(client, store, "owner/repo", &StaleLabelCleanupConfig{
		Enabled: true, Interval: 30 * time.Minute, Threshold: 1 * time.Hour,
	})

	n, err := cleaner.StartupRecover(context.Background())
	if err != nil {
		t.Fatalf("StartupRecover() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if !removeLabelCalled {
		t.Error("RemoveLabel should have been called for stale execution row")
	}
	if n != 1 {
		t.Errorf("StartupRecover() returned %d, want 1", n)
	}
}

// TestCleaner_StartupRecover_NoIssues verifies StartupRecover is a no-op when
// no issues carry the pilot-in-progress label.
func TestCleaner_StartupRecover_NoIssues(t *testing.T) {
	store := createTestStore(t)
	defer func() { _ = store.Close() }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]*Issue{})
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cleaner, _ := NewCleaner(client, store, "owner/repo", &StaleLabelCleanupConfig{
		Enabled: true, Interval: 30 * time.Minute, Threshold: 1 * time.Hour,
	})

	n, err := cleaner.StartupRecover(context.Background())
	if err != nil {
		t.Fatalf("StartupRecover() error = %v", err)
	}
	if n != 0 {
		t.Errorf("StartupRecover() returned %d, want 0", n)
	}
}

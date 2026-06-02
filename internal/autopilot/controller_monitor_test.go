package autopilot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

// TestController_MonitorCompletedOnMerge verifies that when autopilot successfully
// merges a PR, it calls monitor.Complete() to sync dashboard state.
// GH-1336: Dashboard shows stale "failed" status because autopilot didn't update monitor.
func TestController_MonitorCompletedOnMerge(t *testing.T) {
	mergeWasCalled := false
	labelsAdded := []string{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/commits/abc1234/check-runs":
			resp := github.CheckRunsResponse{
				TotalCount: 1,
				CheckRuns: []github.CheckRun{
					{Name: "build", Status: "completed", Conclusion: "success"},
				},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/repos/owner/repo/pulls/42/merge":
			mergeWasCalled = true
			w.WriteHeader(http.StatusOK)
		case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/issues/10/labels") && r.Method == "POST":
			// Track labels added
			var labels []string
			_ = json.NewDecoder(r.Body).Decode(&labels)
			labelsAdded = append(labelsAdded, labels...)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Environment = EnvDev
	cfg.AutoReview = false
	cfg.CIPollInterval = 10 * time.Millisecond
	cfg.DevCITimeout = 1 * time.Second
	cfg.RequiredChecks = []string{"build"}

	// Create controller with mock monitor
	c := NewController(cfg, ghClient, nil, "owner", "repo")
	mockMonitor := newMockTaskMonitor()
	c.SetMonitor(mockMonitor)

	c.OnPRCreated(42, "https://github.com/owner/repo/pull/42", 10, "abc1234", "pilot/GH-10", "")

	ctx := context.Background()

	// Process through the stages: PR created → waiting CI → CI passed → merging → merged
	for i := 0; i < 5; i++ {
		if err := c.ProcessPR(ctx, 42, nil); err != nil {
			t.Fatalf("ProcessPR iteration %d error: %v", i+1, err)
		}
	}

	// Verify merge was called
	if !mergeWasCalled {
		t.Error("merge should have been called")
	}

	// Verify monitor.Complete was called with correct taskID
	expectedTaskID := "GH-10"
	prURL, ok := mockMonitor.completedTasks[expectedTaskID]
	if !ok {
		t.Errorf("monitor.Complete was not called for taskID %s", expectedTaskID)
	}
	if prURL != "https://github.com/owner/repo/pull/42" {
		t.Errorf("monitor.Complete prURL = %s, want https://github.com/owner/repo/pull/42", prURL)
	}
}

// TestController_MonitorNilSafe verifies that nil monitor doesn't cause panic.
func TestController_MonitorNilSafe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/commits/abc1234/check-runs":
			resp := github.CheckRunsResponse{
				TotalCount: 1,
				CheckRuns: []github.CheckRun{
					{Name: "build", Status: "completed", Conclusion: "success"},
				},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		case "/repos/owner/repo/pulls/42/merge":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Environment = EnvDev
	cfg.AutoReview = false
	cfg.CIPollInterval = 10 * time.Millisecond
	cfg.DevCITimeout = 1 * time.Second
	cfg.RequiredChecks = []string{"build"}

	// Create controller WITHOUT setting monitor (nil monitor)
	c := NewController(cfg, ghClient, nil, "owner", "repo")
	// Intentionally NOT calling c.SetMonitor(...)

	c.OnPRCreated(42, "https://github.com/owner/repo/pull/42", 10, "abc1234", "pilot/GH-10", "")

	ctx := context.Background()

	// Process through all stages - should not panic even with nil monitor
	for i := 0; i < 5; i++ {
		if err := c.ProcessPR(ctx, 42, nil); err != nil {
			t.Fatalf("ProcessPR iteration %d error: %v", i+1, err)
		}
	}
	// If we get here without panic, nil safety is verified
}

package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestPoller_CheckForNewIssues(t *testing.T) {
	tests := []struct {
		name               string
		issues             []*Issue
		expectedProcessed  int
		callbackShouldFail bool
	}{
		{
			name: "processes new pilot issues",
			issues: []*Issue{
				{Number: 1, Title: "Issue 1", Labels: []Label{{Name: "pilot"}}},
				{Number: 2, Title: "Issue 2", Labels: []Label{{Name: "pilot"}}},
			},
			expectedProcessed:  2,
			callbackShouldFail: false,
		},
		{
			name: "skips in-progress issues",
			issues: []*Issue{
				{Number: 1, Title: "Issue 1", Labels: []Label{{Name: "pilot"}, {Name: LabelInProgress}}},
				{Number: 2, Title: "Issue 2", Labels: []Label{{Name: "pilot"}}},
			},
			expectedProcessed:  1,
			callbackShouldFail: false,
		},
		{
			name: "skips done issues",
			issues: []*Issue{
				{Number: 1, Title: "Issue 1", Labels: []Label{{Name: "pilot"}, {Name: LabelDone}}},
				{Number: 2, Title: "Issue 2", Labels: []Label{{Name: "pilot"}}},
			},
			expectedProcessed:  1,
			callbackShouldFail: false,
		},
		{
			name:               "handles empty response",
			issues:             []*Issue{},
			expectedProcessed:  0,
			callbackShouldFail: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(tt.issues)
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

			processedIssues := []*Issue{}
			var mu sync.Mutex

			poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
				WithOnIssue(func(ctx context.Context, issue *Issue) error {
					if tt.callbackShouldFail {
						return errors.New("callback error")
					}
					mu.Lock()
					processedIssues = append(processedIssues, issue)
					mu.Unlock()
					return nil
				}),
			)

			// Call checkForNewIssues directly
			poller.checkForNewIssues(context.Background())
			poller.WaitForActive()

			mu.Lock()
			got := len(processedIssues)
			mu.Unlock()
			if got != tt.expectedProcessed {
				t.Errorf("processed %d issues, want %d", got, tt.expectedProcessed)
			}
		})
	}
}

func TestPoller_CheckForNewIssues_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

	callbackCalled := false
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithOnIssue(func(ctx context.Context, issue *Issue) error {
			callbackCalled = true
			return nil
		}),
	)

	// Should not panic and should not call callback
	poller.checkForNewIssues(context.Background())

	if callbackCalled {
		t.Error("callback should not be called on API error")
	}
}

func TestPoller_CheckForNewIssues_CallbackError(t *testing.T) {
	issues := []*Issue{
		{Number: 1, Title: "Issue 1", Labels: []Label{{Name: "pilot"}}},
		{Number: 2, Title: "Issue 2", Labels: []Label{{Name: "pilot"}}},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

	var callCount int32
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithOnIssue(func(ctx context.Context, issue *Issue) error {
			atomic.AddInt32(&callCount, 1)
			return errors.New("callback error")
		}),
	)

	poller.checkForNewIssues(context.Background())
	poller.WaitForActive()

	// Both issues should be attempted (callback is called for both)
	if got := atomic.LoadInt32(&callCount); got != 2 {
		t.Errorf("callback called %d times, want 2", got)
	}

	// GH-2176: When the callback returns an error, the issue is unmarked so it
	// can be retried on the next poll cycle (after pilot-failed label is removed).
	if poller.IsProcessed(1) {
		t.Error("issue 1 should NOT be marked as processed after callback error (GH-2176: retry path)")
	}
	if poller.IsProcessed(2) {
		t.Error("issue 2 should NOT be marked as processed after callback error (GH-2176: retry path)")
	}
}

func TestPoller_CheckForNewIssues_NoCallback(t *testing.T) {
	issues := []*Issue{
		{Number: 1, Title: "Issue 1", Labels: []Label{{Name: "pilot"}}},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

	// Create poller without callback
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second)

	// Should not panic
	poller.checkForNewIssues(context.Background())
	poller.WaitForActive()

	// Issue should be marked as processed even without callback
	if !poller.IsProcessed(1) {
		t.Error("issue should be marked as processed when no callback is set")
	}
}

func TestPoller_CheckForNewIssues_SkipsAlreadyProcessed(t *testing.T) {
	// Issue 1 has pilot-failed and is at max retries — should be skipped
	// Issue 2 has only pilot so should be processed
	issues := []*Issue{
		{Number: 1, Title: "Issue 1", Labels: []Label{{Name: "pilot"}, {Name: "pilot-failed"}}},
		{Number: 2, Title: "Issue 2", Labels: []Label{{Name: "pilot"}}},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

	var callCount int32
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithOnIssue(func(ctx context.Context, issue *Issue) error {
			atomic.AddInt32(&callCount, 1)
			return nil
		}),
		WithMaxFailedRetries(3),
	)

	// GH-2176: Set retry count to max so issue is skipped (not auto-retried)
	poller.mu.Lock()
	poller.failedRetryCount[1] = 3
	poller.mu.Unlock()

	poller.checkForNewIssues(context.Background())
	poller.WaitForActive()

	// Only issue 2 should trigger callback (issue 1 at max retries)
	if got := atomic.LoadInt32(&callCount); got != 1 {
		t.Errorf("callback called %d times, want 1 (issue at retry limit should be skipped)", got)
	}
}

func TestPoller_CheckForNewIssues_AllowsRetryWhenLabelsRemoved(t *testing.T) {
	// Issue was processed before but pilot-failed was removed
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

	// Should retry since labels were removed
	if got := atomic.LoadInt32(&callCount); got != 1 {
		t.Errorf("callback called %d times, want 1 (should retry after labels removed)", got)
	}
}

func TestPoller_Start_CancelsOnContextDone(t *testing.T) {
	client := NewClient(testutil.FakeGitHubToken)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 100*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		poller.Start(ctx)
		close(done)
	}()

	// Let it run briefly
	time.Sleep(50 * time.Millisecond)

	// Cancel context
	cancel()

	// Should exit within reasonable time
	select {
	case <-done:
		// Good - poller stopped
	case <-time.After(1 * time.Second):
		t.Error("poller did not stop within timeout after context cancellation")
	}
}

func TestPoller_Start_InitialCheck(t *testing.T) {
	issues := []*Issue{
		{Number: 1, Title: "Issue 1", Labels: []Label{{Name: "pilot"}}},
	}

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

	callbackCalled := make(chan struct{}, 1)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 1*time.Hour, // Long interval
		WithOnIssue(func(ctx context.Context, issue *Issue) error {
			select {
			case callbackCalled <- struct{}{}:
			default:
			}
			return nil
		}),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go poller.Start(ctx)

	// Should get initial check quickly
	select {
	case <-callbackCalled:
		// Good - initial check happened
	case <-time.After(500 * time.Millisecond):
		t.Error("initial check did not happen quickly")
	}

	cancel()
}

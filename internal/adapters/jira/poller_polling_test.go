package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestPollerCheckForNewIssues(t *testing.T) {
	var requestCount int
	var processedIssue *Issue

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")

		// Handle search request (Cloud uses POST /search/jql since May 2025)
		if r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/search/jql" {
			resp := SearchResponse{
				Issues: []*Issue{
					{
						Key: "TEST-1",
						Fields: Fields{
							Summary:     "First issue",
							Description: "Test description",
							Created:     "2024-01-01T10:00:00.000+0000",
							Labels:      []string{"pilot"},
						},
					},
					{
						Key: "TEST-2",
						Fields: Fields{
							Summary:     "Second issue (in progress)",
							Description: "Already being worked on",
							Created:     "2024-01-02T10:00:00.000+0000",
							Labels:      []string{"pilot", "pilot-in-progress"},
						},
					},
				},
				IsLast: true,
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// Handle label add/remove
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, testutil.FakeJiraUsername, testutil.FakeJiraAPIToken, PlatformCloud)
	config := &Config{PilotLabel: "pilot"}
	poller := NewPoller(client, config, 30*time.Second,
		WithOnJiraIssue(func(ctx context.Context, issue *Issue) (*IssueResult, error) {
			processedIssue = issue
			return &IssueResult{Success: true}, nil
		}),
	)

	ctx := context.Background()
	poller.checkForNewIssues(ctx)
	// GH-1357: Wait for parallel execution to complete
	poller.WaitForActive()

	// Should process TEST-1 but skip TEST-2 (has in-progress label)
	if processedIssue == nil {
		t.Fatal("expected an issue to be processed")
	}

	if processedIssue.Key != "TEST-1" {
		t.Errorf("expected TEST-1 to be processed, got %s", processedIssue.Key)
	}

	// TEST-1 should be marked as processed
	if !poller.IsProcessed("TEST-1") {
		t.Error("expected TEST-1 to be marked as processed")
	}
}

func TestPollerCheckForNewIssues_SkipsAlreadyProcessed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/search/jql" {
			resp := SearchResponse{
				Issues: []*Issue{
					{
						Key: "TEST-1",
						Fields: Fields{
							Summary: "Already processed",
							Labels:  []string{"pilot"},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, testutil.FakeJiraUsername, testutil.FakeJiraAPIToken, PlatformCloud)
	config := &Config{PilotLabel: "pilot"}

	var callCount int
	poller := NewPoller(client, config, 30*time.Second,
		WithOnJiraIssue(func(ctx context.Context, issue *Issue) (*IssueResult, error) {
			callCount++
			return &IssueResult{Success: true}, nil
		}),
	)

	// Mark as already processed
	poller.markProcessed("TEST-1")

	ctx := context.Background()
	poller.checkForNewIssues(ctx)

	if callCount != 0 {
		t.Errorf("expected callback not to be called for already processed issue, got %d calls", callCount)
	}
}

func TestPollerStart_CancelsOnContextDone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := SearchResponse{Issues: []*Issue{}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, testutil.FakeJiraUsername, testutil.FakeJiraAPIToken, PlatformCloud)
	config := &Config{PilotLabel: "pilot"}
	poller := NewPoller(client, config, 100*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- poller.Start(ctx)
	}()

	// Cancel after a short delay
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil error on cancel, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("poller did not stop after context cancellation")
	}
}

//go:build integration

package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// TestPoller_Integration_IssueDiscovery verifies real Poller discovers issues correctly
func TestPoller_Integration_IssueDiscovery(t *testing.T) {
	// Track API calls
	var mu sync.Mutex
	apiCalls := make(map[string]int)

	// Track which issues have been "processed" (would get pilot-done label)
	processedInServer := make(map[int]bool)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		apiCalls[r.URL.Path]++
		mu.Unlock()

		switch {
		case r.URL.Path == "/repos/test/repo/issues":
			// Return issues with pilot label, but mark processed ones as done
			mu.Lock()
			issues := []*Issue{}
			if !processedInServer[1] {
				issues = append(issues, &Issue{
					Number:    1,
					Title:     "Test Issue 1",
					Body:      "Test body 1",
					State:     "open",
					Labels:    []Label{{Name: "pilot"}},
					CreatedAt: time.Now().Add(-2 * time.Hour),
				})
			}
			if !processedInServer[2] {
				issues = append(issues, &Issue{
					Number:    2,
					Title:     "Test Issue 2",
					Body:      "Test body 2",
					State:     "open",
					Labels:    []Label{{Name: "pilot"}},
					CreatedAt: time.Now().Add(-1 * time.Hour),
				})
			}
			mu.Unlock()
			json.NewEncoder(w).Encode(issues)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	client := NewClientWithBaseURL("test-token", server.URL)

	// Track processed issues
	var processedIssues []int
	var processMu sync.Mutex

	poller, err := NewPoller(client, "test/repo", "pilot", 100*time.Millisecond,
		WithOnIssue(func(ctx context.Context, issue *Issue) error {
			processMu.Lock()
			processedIssues = append(processedIssues, issue.Number)
			processMu.Unlock()

			// Mark as processed in mock server
			mu.Lock()
			processedInServer[issue.Number] = true
			mu.Unlock()
			return nil
		}),
		WithMaxConcurrent(1),
	)
	if err != nil {
		t.Fatalf("NewPoller failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start poller in background
	go poller.Start(ctx)

	// Wait for issues to be processed
	time.Sleep(500 * time.Millisecond)
	cancel()

	// Verify issues were discovered
	processMu.Lock()
	count := len(processedIssues)
	processMu.Unlock()

	if count != 2 {
		t.Errorf("Expected 2 issues processed, got %d", count)
	}

	// Verify API was called
	mu.Lock()
	issuesCalls := apiCalls["/repos/test/repo/issues"]
	mu.Unlock()

	if issuesCalls < 1 {
		t.Error("Expected at least 1 issues API call")
	}
}

// TestPoller_Integration_LabelFiltering verifies label-based filtering
func TestPoller_Integration_LabelFiltering(t *testing.T) {
	var mu sync.Mutex
	issue10Processed := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/test/repo/issues":
			mu.Lock()
			issues := []*Issue{
				// Only return issue 10 if not yet processed
				{
					Number: 11,
					Title:  "Has pilot-in-progress label (should skip)",
					State:  "open",
					Labels: []Label{
						{Name: "pilot"},
						{Name: "pilot-in-progress"},
					},
					CreatedAt: time.Now(),
				},
				{
					Number: 12,
					Title:  "Has pilot-done label (should skip)",
					State:  "open",
					Labels: []Label{
						{Name: "pilot"},
						{Name: "pilot-done"},
					},
					CreatedAt: time.Now(),
				},
				{
					// NOTE: pilot-failed is intentionally NOT used here: since GH-2176
					// a pilot-failed issue (without pilot-done) is auto-retried, not
					// skipped. pilot-blocked is a deterministic skip label (GH-2402).
					Number: 13,
					Title:  "Has pilot-blocked label (should skip)",
					State:  "open",
					Labels: []Label{
						{Name: "pilot"},
						{Name: "pilot-blocked"},
					},
					CreatedAt: time.Now(),
				},
			}
			if !issue10Processed {
				issues = append([]*Issue{{
					Number:    10,
					Title:     "Has pilot label",
					State:     "open",
					Labels:    []Label{{Name: "pilot"}},
					CreatedAt: time.Now(),
				}}, issues...)
			}
			mu.Unlock()
			json.NewEncoder(w).Encode(issues)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	client := NewClientWithBaseURL("test-token", server.URL)

	var processedIssues []int
	var processMu sync.Mutex

	poller, err := NewPoller(client, "test/repo", "pilot", 100*time.Millisecond,
		WithOnIssue(func(ctx context.Context, issue *Issue) error {
			processMu.Lock()
			processedIssues = append(processedIssues, issue.Number)
			processMu.Unlock()

			mu.Lock()
			if issue.Number == 10 {
				issue10Processed = true
			}
			mu.Unlock()
			return nil
		}),
		WithMaxConcurrent(1),
	)
	if err != nil {
		t.Fatalf("NewPoller failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	go poller.Start(ctx)
	time.Sleep(500 * time.Millisecond)
	cancel()

	processMu.Lock()
	defer processMu.Unlock()

	// Only issue #10 should be processed (no status labels)
	if len(processedIssues) != 1 {
		t.Errorf("Expected 1 issue processed, got %d", len(processedIssues))
	}

	if len(processedIssues) > 0 && processedIssues[0] != 10 {
		t.Errorf("Expected issue #10 to be processed, got #%d", processedIssues[0])
	}
}

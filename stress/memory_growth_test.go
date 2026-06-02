package stress

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

// TestMemory_ProcessedMapGrowth verifies the processed map doesn't leak.
func TestMemory_ProcessedMapGrowth(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping memory test in short mode")
	}

	const numIssues = 1000

	issues := make([]*github.Issue, numIssues)
	for i := 0; i < numIssues; i++ {
		issues[i] = &github.Issue{
			Number:    i + 1,
			Title:     fmt.Sprintf("Issue %d", i+1),
			State:     "open",
			Labels:    []github.Label{{Name: "pilot"}},
			CreatedAt: time.Now(),
		}
	}

	// After the initial dispatch poll, return issues with pilot-in-progress label
	// so the retry logic doesn't clear them from the processed map on subsequent ticks.
	// recoverOrphanedIssues also makes a ListIssues call, so we track calls and
	// return the dispatchable set only on the second call (first actual poll).
	var pollCount int64
	inProgressIssues := make([]*github.Issue, numIssues)
	for i := 0; i < numIssues; i++ {
		inProgressIssues[i] = &github.Issue{
			Number:    i + 1,
			Title:     fmt.Sprintf("Issue %d", i+1),
			State:     "open",
			Labels:    []github.Label{{Name: "pilot"}, {Name: "pilot-in-progress"}},
			CreatedAt: time.Now(),
		}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Single-issue GET (e.g. /repos/owner/repo/issues/42): return one fresh
		// issue so the poller's label re-check (GH-2341) doesn't decode a
		// 1000-element array per dispatch.
		if strings.Contains(r.URL.Path, "/issues/") {
			parts := strings.Split(r.URL.Path, "/")
			if n, perr := strconv.Atoi(parts[len(parts)-1]); perr == nil && n >= 1 && n <= numIssues {
				_ = json.NewEncoder(w).Encode(issues[n-1])
				return
			}
		}
		// TASK-321 PR-4: parallel checkForNewIssues now runs the fresh-candidate
		// merged-work guard, which probes /search/issues + /pulls per candidate.
		// Answer "no merged work" cheaply so the guard is a fast no-op — otherwise
		// it would decode the 1000-element list per issue and starve the 30s poll.
		if strings.HasPrefix(r.URL.Path, "/search/issues") {
			_, _ = w.Write([]byte(`{"total_count":0,"items":[]}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/pulls") {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		// C6/TASK-346: ListIssues now paginates (per_page=100&page=N). Return the
		// full list only on page 1 and an empty page after, so pagination terminates;
		// otherwise the mock would return 1000 per page up to maxPages (50k items),
		// starving the poll. TASK-353.
		if p := r.URL.Query().Get("page"); p != "" && p != "1" {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		n := atomic.AddInt64(&pollCount, 1)
		if n <= 2 {
			// Call 1: recoverOrphanedIssues, Call 2: first checkForNewIssues
			_ = json.NewEncoder(w).Encode(issues)
		} else {
			_ = json.NewEncoder(w).Encode(inProgressIssues)
		}
	}))
	defer server.Close()

	client := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

	var processedCount int64

	poller, err := github.NewPoller(
		client,
		"owner/repo",
		"pilot",
		10*time.Millisecond,
		github.WithExecutionMode(github.ExecutionModeParallel),
		github.WithMaxConcurrent(20),
		github.WithOnIssue(func(ctx context.Context, issue *github.Issue) error {
			atomic.AddInt64(&processedCount, 1)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("Failed to create poller: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	go poller.Start(ctx)

	// Wait for all to be processed
	waitForProcessed(t, func() int64 { return atomic.LoadInt64(&processedCount) }, int64(numIssues), 25*time.Second)

	cancel()
	poller.WaitForActive()

	// Verify processed count matches
	if count := poller.ProcessedCount(); count != numIssues {
		t.Errorf("ProcessedCount() = %d, want %d", count, numIssues)
	}

	// Reset should clear processed map
	poller.Reset()
	if count := poller.ProcessedCount(); count != 0 {
		t.Errorf("ProcessedCount() after Reset() = %d, want 0", count)
	}
}

// TestMemory_LargePayloads tests handling of issues with large bodies.
func TestMemory_LargePayloads(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping memory test in short mode")
	}

	const (
		numIssues     = 20
		bodySize      = 100 * 1024 // 100KB per issue body
		maxConcurrent = 5
	)

	// Generate large issue bodies
	largeBody := make([]byte, bodySize)
	for i := range largeBody {
		largeBody[i] = byte('a' + i%26)
	}
	bodyStr := string(largeBody)

	issues := make([]*github.Issue, numIssues)
	for i := 0; i < numIssues; i++ {
		issues[i] = &github.Issue{
			Number:    i + 1,
			Title:     fmt.Sprintf("Large Issue %d", i+1),
			Body:      bodyStr,
			State:     "open",
			Labels:    []github.Label{{Name: "pilot"}},
			CreatedAt: time.Now(),
		}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// C6/TASK-346: paginate-aware mock — full list on page 1, empty after, so
		// ListIssues pagination terminates instead of looping to maxPages. TASK-353.
		if p := r.URL.Query().Get("page"); p != "" && p != "1" {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	client := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

	var processedCount int64

	poller, err := github.NewPoller(
		client,
		"owner/repo",
		"pilot",
		100*time.Millisecond,
		github.WithMaxConcurrent(maxConcurrent),
		github.WithOnIssue(func(ctx context.Context, issue *github.Issue) error {
			// Verify body is intact
			if len(issue.Body) != bodySize {
				return fmt.Errorf("body size mismatch: got %d, want %d", len(issue.Body), bodySize)
			}
			time.Sleep(20 * time.Millisecond)
			atomic.AddInt64(&processedCount, 1)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("Failed to create poller: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	go poller.Start(ctx)

	for atomic.LoadInt64(&processedCount) < int64(numIssues) {
		time.Sleep(100 * time.Millisecond)
	}

	cancel()
	poller.WaitForActive()

	processed := atomic.LoadInt64(&processedCount)
	if processed != int64(numIssues) {
		t.Errorf("Processed %d issues, want %d", processed, numIssues)
	}

	t.Logf("Successfully processed %d issues with %dKB payloads each", numIssues, bodySize/1024)
}

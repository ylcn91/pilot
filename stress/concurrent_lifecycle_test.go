package stress

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

// TestStress_NoDeadlock verifies no deadlock occurs under concurrent load.
func TestStress_NoDeadlock(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	const (
		numIssues     = 50
		maxConcurrent = 10
		timeout       = 30 * time.Second
	)

	var (
		processedSet = make(map[int]bool)
		mu           sync.Mutex
	)

	issues := make([]*github.Issue, numIssues)
	for i := 0; i < numIssues; i++ {
		issues[i] = &github.Issue{
			Number:    i + 1,
			Title:     fmt.Sprintf("Issue %d", i+1),
			State:     "open",
			Labels:    []github.Label{{Name: "pilot"}},
			CreatedAt: time.Now().Add(-time.Duration(i) * time.Minute),
		}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Return issues with pilot-done label if already processed
		mu.Lock()
		responseIssues := make([]*github.Issue, 0, numIssues)
		for _, issue := range issues {
			issueCopy := *issue
			if processedSet[issue.Number] {
				issueCopy.Labels = []github.Label{{Name: "pilot"}, {Name: github.LabelDone}}
			}
			responseIssues = append(responseIssues, &issueCopy)
		}
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(responseIssues)
	}))
	defer server.Close()

	client := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

	poller, err := github.NewPoller(
		client,
		"owner/repo",
		"pilot",
		10*time.Millisecond,
		github.WithMaxConcurrent(maxConcurrent),
		github.WithOnIssue(func(ctx context.Context, issue *github.Issue) error {
			// Minimal processing to maximize throughput
			time.Sleep(5 * time.Millisecond)
			mu.Lock()
			processedSet[issue.Number] = true
			mu.Unlock()
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("Failed to create poller: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	completed := make(chan struct{})
	go func() {
		poller.Start(ctx)
		close(completed)
	}()

	// Wait for all unique issues to be processed
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			cancel()
			poller.WaitForActive()
			mu.Lock()
			processed := len(processedSet)
			mu.Unlock()
			if processed < numIssues {
				t.Errorf("Possible deadlock: only processed %d/%d issues in %v",
					processed, numIssues, timeout)
			}
			return
		default:
			mu.Lock()
			processed := len(processedSet)
			mu.Unlock()
			if processed >= numIssues {
				goto done
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

done:
	cancel()
	poller.WaitForActive()

	mu.Lock()
	processed := len(processedSet)
	mu.Unlock()
	if processed != numIssues {
		t.Errorf("Processed %d/%d issues", processed, numIssues)
	}
}

// TestStress_GoroutineStability verifies goroutine count stabilizes after processing.
func TestStress_GoroutineStability(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	// Force GC and get baseline
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	baselineGoroutines := runtime.NumGoroutine()

	const (
		numIssues     = 25
		maxConcurrent = 5
	)

	var processedCount int64

	issues := make([]*github.Issue, numIssues)
	for i := 0; i < numIssues; i++ {
		issues[i] = &github.Issue{
			Number:    i + 1,
			Title:     fmt.Sprintf("Issue %d", i+1),
			State:     "open",
			Labels:    []github.Label{{Name: "pilot"}},
			CreatedAt: time.Now().Add(-time.Duration(i) * time.Minute),
		}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	client := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

	poller, err := github.NewPoller(
		client,
		"owner/repo",
		"pilot",
		50*time.Millisecond,
		github.WithMaxConcurrent(maxConcurrent),
		github.WithOnIssue(func(ctx context.Context, issue *github.Issue) error {
			time.Sleep(10 * time.Millisecond)
			atomic.AddInt64(&processedCount, 1)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("Failed to create poller: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	go poller.Start(ctx)

	// Wait for all issues to process
	for atomic.LoadInt64(&processedCount) < int64(numIssues) {
		time.Sleep(50 * time.Millisecond)
	}

	cancel()
	poller.WaitForActive()

	// Allow goroutines to clean up
	time.Sleep(500 * time.Millisecond)
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	finalGoroutines := runtime.NumGoroutine()

	// Allow some tolerance (test runner goroutines, etc.)
	tolerance := 5
	if finalGoroutines > baselineGoroutines+tolerance {
		t.Errorf("Goroutine leak: baseline=%d, final=%d, leaked=%d",
			baselineGoroutines, finalGoroutines, finalGoroutines-baselineGoroutines)
	}

	t.Logf("Goroutines: baseline=%d, final=%d", baselineGoroutines, finalGoroutines)
}

// TestStress_RapidCancellation tests graceful shutdown under cancellation.
func TestStress_RapidCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	const numIssues = 100

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

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	client := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

	poller, err := github.NewPoller(
		client,
		"owner/repo",
		"pilot",
		10*time.Millisecond,
		github.WithMaxConcurrent(10),
		github.WithOnIssue(func(ctx context.Context, issue *github.Issue) error {
			// Long-running task
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				return nil
			}
		}),
	)
	if err != nil {
		t.Fatalf("Failed to create poller: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		poller.Start(ctx)
		close(done)
	}()

	// Let some issues start processing
	time.Sleep(100 * time.Millisecond)

	// Cancel abruptly
	cancel()

	// Should complete within reasonable time
	select {
	case <-done:
		// Good - stopped gracefully
	case <-time.After(10 * time.Second):
		t.Error("Poller did not stop gracefully after cancellation")
	}

	poller.WaitForActive()
}

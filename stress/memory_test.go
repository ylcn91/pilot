package stress

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

// waitForProcessed busy-waits until get() reaches want, failing fast with a clear
// message when timeout elapses instead of hanging until the whole-package test
// timeout (600s). The unbounded version of this loop made these stress tests red
// CI on unrelated PRs whenever dispatch was slow under load. TASK-353.
func waitForProcessed(t *testing.T, get func() int64, want int64, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for get() < want {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %v waiting for processed count: got %d, want %d", timeout, get(), want)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestMemory_NoUnboundedGrowth verifies memory doesn't grow unbounded during processing.
func TestMemory_NoUnboundedGrowth(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping memory test in short mode")
	}

	const (
		numIssues     = 100
		maxConcurrent = 10
		// Allow 50MB growth max - generous for test overhead
		maxGrowthBytes = 50 * 1024 * 1024
	)

	// Force GC and baseline
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	var initialMem runtime.MemStats
	runtime.ReadMemStats(&initialMem)

	var processedCount int64

	issues := make([]*github.Issue, numIssues)
	for i := 0; i < numIssues; i++ {
		issues[i] = &github.Issue{
			Number:    i + 1,
			Title:     fmt.Sprintf("Memory Test Issue %d", i+1),
			Body:      fmt.Sprintf("Body with some content for issue %d to simulate realistic payload size", i+1),
			State:     "open",
			Labels:    []github.Label{{Name: "pilot"}},
			CreatedAt: time.Now().Add(-time.Duration(numIssues-i) * time.Minute),
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

	poller, err := github.NewPoller(
		client,
		"owner/repo",
		"pilot",
		50*time.Millisecond,
		github.WithMaxConcurrent(maxConcurrent),
		github.WithOnIssue(func(ctx context.Context, issue *github.Issue) error {
			// Simulate some work that allocates memory
			_ = make([]byte, 1024) // 1KB allocation per issue
			time.Sleep(10 * time.Millisecond)
			atomic.AddInt64(&processedCount, 1)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("Failed to create poller: %v", err)
	}

	// Track memory during processing
	metrics := NewMetrics()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	go poller.Start(ctx)

	// Sample memory periodically
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				metrics.SampleMemoryAndGoroutines()
			}
		}
	}()

	// Wait for processing
	waitForProcessed(t, func() int64 { return atomic.LoadInt64(&processedCount) }, int64(numIssues), 25*time.Second)

	close(done)
	cancel()
	poller.WaitForActive()

	// Force GC to get accurate final memory
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	var finalMem runtime.MemStats
	runtime.ReadMemStats(&finalMem)

	memGrowth := int64(finalMem.Alloc) - int64(initialMem.Alloc)

	t.Logf("Memory stats:")
	t.Logf("  Initial: %d MB", initialMem.Alloc/1024/1024)
	t.Logf("  Peak: %d MB", metrics.GetPeakMemory()/1024/1024)
	t.Logf("  Final: %d MB", finalMem.Alloc/1024/1024)
	t.Logf("  Growth: %d bytes", memGrowth)
	t.Logf("  Issues processed: %d", atomic.LoadInt64(&processedCount))

	// Memory growth should be bounded
	if memGrowth > maxGrowthBytes {
		t.Errorf("Memory grew by %d bytes (%.2f MB), exceeds limit of %d bytes",
			memGrowth, float64(memGrowth)/1024/1024, maxGrowthBytes)
	}
}

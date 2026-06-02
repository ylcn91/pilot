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

// TestMemory_RepeatedStartStop tests for memory leaks across multiple start/stop cycles.
func TestMemory_RepeatedStartStop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping memory test in short mode")
	}

	const (
		numCycles      = 10
		issuesPerCycle = 10
	)

	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	var initialMem runtime.MemStats
	runtime.ReadMemStats(&initialMem)

	baseIssues := make([]*github.Issue, issuesPerCycle)
	for i := 0; i < issuesPerCycle; i++ {
		baseIssues[i] = &github.Issue{
			Number:    i + 1,
			Title:     fmt.Sprintf("Issue %d", i+1),
			State:     "open",
			Labels:    []github.Label{{Name: "pilot"}},
			CreatedAt: time.Now(),
		}
	}

	for cycle := 0; cycle < numCycles; cycle++ {
		// Create fresh issues for each cycle
		issues := make([]*github.Issue, issuesPerCycle)
		for i := 0; i < issuesPerCycle; i++ {
			issues[i] = &github.Issue{
				Number:    (cycle * issuesPerCycle) + i + 1,
				Title:     fmt.Sprintf("Cycle %d Issue %d", cycle+1, i+1),
				State:     "open",
				Labels:    []github.Label{{Name: "pilot"}},
				CreatedAt: time.Now(),
			}
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			// C6/TASK-346: paginate-aware mock — full list on page 1, empty after.
			if p := r.URL.Query().Get("page"); p != "" && p != "1" {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			_ = json.NewEncoder(w).Encode(issues)
		}))

		client := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

		var processedCount int64

		poller, err := github.NewPoller(
			client,
			"owner/repo",
			"pilot",
			10*time.Millisecond,
			github.WithMaxConcurrent(5),
			github.WithOnIssue(func(ctx context.Context, issue *github.Issue) error {
				atomic.AddInt64(&processedCount, 1)
				return nil
			}),
		)
		if err != nil {
			t.Fatalf("Cycle %d: failed to create poller: %v", cycle, err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		go poller.Start(ctx)

		// Wait for processing
		deadline := time.After(5 * time.Second)
		for atomic.LoadInt64(&processedCount) < int64(issuesPerCycle) {
			select {
			case <-deadline:
				t.Fatalf("Cycle %d timed out", cycle)
			default:
				time.Sleep(10 * time.Millisecond)
			}
		}

		cancel()
		poller.WaitForActive()
		server.Close()

		// Force GC between cycles
		runtime.GC()
	}

	// Final cleanup
	time.Sleep(500 * time.Millisecond)
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	var finalMem runtime.MemStats
	runtime.ReadMemStats(&finalMem)

	// Allow 10MB growth for test overhead
	maxGrowth := uint64(10 * 1024 * 1024)
	if finalMem.Alloc > initialMem.Alloc+maxGrowth {
		t.Errorf("Memory leak detected: initial=%dMB, final=%dMB, growth=%dMB",
			initialMem.Alloc/1024/1024, finalMem.Alloc/1024/1024,
			(finalMem.Alloc-initialMem.Alloc)/1024/1024)
	}

	t.Logf("Completed %d cycles, memory: initial=%dMB, final=%dMB",
		numCycles, initialMem.Alloc/1024/1024, finalMem.Alloc/1024/1024)
}

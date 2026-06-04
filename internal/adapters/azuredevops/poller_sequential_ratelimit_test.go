package azuredevops

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/skipreason"
	"github.com/ylcn91/pilot/internal/testutil"
)

// TestStartSequential_RateLimited_DoesNotMarkProcessed verifies the GH-3252
// parity fix: a rate-limit error from the executor must NOT mark the work item
// processed (which would drop it until restart). Instead it is left unprocessed
// for the next poll cycle and a task-queued skip metric is recorded.
func TestStartSequential_RateLimited_DoesNotMarkProcessed(t *testing.T) {
	srv := &sequentialServer{
		workItems: []*WorkItem{newSequentialWorkItem(11, "2024-01-01T10:00:00Z")},
	}
	server := httptest.NewServer(srv.handler(t))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeAzureDevOpsPAT, "org", "project", server.URL)

	metrics := newFakePollerMetrics()
	var handlerCalls int32

	poller := NewPoller(client, "pilot", 5*time.Millisecond,
		WithExecutionMode(ExecutionModeSequential),
		WithPollerMetrics(metrics),
		WithOnWorkItemWithResult(func(ctx context.Context, wi *WorkItem) (*WorkItemResult, error) {
			atomic.AddInt32(&handlerCalls, 1)
			// Mimic a Claude Code rate-limit error (executor.IsRateLimitError true).
			return nil, errors.New("Claude usage limit reached. You've hit your limit · resets 6am (UTC)")
		}),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		poller.startSequential(ctx)
		close(done)
	}()

	if !waitUntil(t, 2*time.Second, func() bool { return atomic.LoadInt32(&handlerCalls) >= 1 }) {
		t.Fatal("expected work item handler to be invoked at least once")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("startSequential did not return after ctx cancel")
	}

	if poller.IsProcessed(11) {
		t.Error("rate-limited work item must NOT be marked processed (would be dropped until restart)")
	}

	metrics.mu.Lock()
	queued := metrics.skipped[skipreason.ReasonTaskQueued]
	metrics.mu.Unlock()
	if queued < 1 {
		t.Errorf("expected at least one %q skip metric, got %d", skipreason.ReasonTaskQueued, queued)
	}
}

package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/skipreason"
)

// TestStartSequential_RateLimited_DoesNotMarkProcessed verifies the GH-3252
// parity fix for GitLab: a rate-limit error from the executor must NOT mark the
// issue processed (which would drop it until restart). The issue is left
// unprocessed for the next poll cycle and a task-queued skip metric is recorded.
func TestStartSequential_RateLimited_DoesNotMarkProcessed(t *testing.T) {
	issue := &Issue{
		ID:        1,
		IID:       11,
		Title:     "seq issue",
		State:     StateOpened,
		Labels:    []string{"pilot"},
		CreatedAt: time.Now().Add(-time.Hour),
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// findOldestUnprocessedIssue lists open issues with the pilot label.
		if strings.Contains(r.URL.Path, "/issues") && r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode([]*Issue{issue})
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := NewClientWithBaseURL("test-token", "namespace/project", server.URL)

	metrics := newFakePollerMetrics()
	var handlerCalls int32

	poller := NewPoller(client, "pilot", 5*time.Millisecond,
		WithExecutionMode(ExecutionModeSequential),
		WithPollerMetrics(metrics),
		WithOnIssueWithResult(func(ctx context.Context, issue *Issue) (*IssueResult, error) {
			atomic.AddInt32(&handlerCalls, 1)
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

	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&handlerCalls) < 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if atomic.LoadInt32(&handlerCalls) < 1 {
		t.Fatal("expected issue handler to be invoked at least once")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("startSequential did not return after ctx cancel")
	}

	if poller.IsProcessed(11) {
		t.Error("rate-limited issue must NOT be marked processed (would be dropped until restart)")
	}

	metrics.mu.Lock()
	queued := metrics.skipped[skipreason.ReasonTaskQueued]
	metrics.mu.Unlock()
	if queued < 1 {
		t.Errorf("expected at least one %q skip metric, got %d", skipreason.ReasonTaskQueued, queued)
	}
}

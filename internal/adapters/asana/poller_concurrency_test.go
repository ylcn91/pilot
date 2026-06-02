package asana

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestPollerWithProcessedStore(t *testing.T) {
	client := NewClient(testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	config := &Config{PilotTag: "pilot"}
	store := NewMockProcessedStore()

	// Pre-populate store
	_ = store.Mark("asana", "", "task-1")
	_ = store.Mark("asana", "", "task-2")

	poller := NewPoller(client, config, 30*time.Second,
		WithProcessedStore(store),
	)

	// Should load processed tasks from store
	if !poller.IsProcessed("task-1") {
		t.Error("expected task-1 to be loaded from store")
	}
	if !poller.IsProcessed("task-2") {
		t.Error("expected task-2 to be loaded from store")
	}
	if poller.ProcessedCount() != 2 {
		t.Errorf("expected 2 processed tasks, got %d", poller.ProcessedCount())
	}
}

func TestPollerDrainAndWaitForActive(t *testing.T) {
	client := NewClient(testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	config := &Config{PilotTag: "pilot"}

	taskProcessed := make(chan struct{}, 1)
	poller := NewPoller(client, config, 30*time.Second,
		WithOnAsanaTask(func(ctx context.Context, task *Task) (*TaskResult, error) {
			time.Sleep(50 * time.Millisecond) // simulate work
			taskProcessed <- struct{}{}
			return &TaskResult{Success: true}, nil
		}),
		WithMaxConcurrent(1),
	)

	// Simulate a task being dispatched
	go poller.processTaskAsync(context.Background(), &Task{GID: "test-task"})

	// Test WaitForActive
	done := make(chan struct{})
	go func() {
		poller.WaitForActive()
		close(done)
	}()

	select {
	case <-taskProcessed:
		// Good, task was processed
	case <-time.After(200 * time.Millisecond):
		t.Error("task should have been processed")
	}

	select {
	case <-done:
		// Good, WaitForActive returned
	case <-time.After(200 * time.Millisecond):
		t.Error("WaitForActive should have returned")
	}

	// Reset stopping flag for next test
	poller.stopping.Store(false)

	// Test Drain
	go poller.processTaskAsync(context.Background(), &Task{GID: "test-task-2"})

	drainDone := make(chan struct{})
	go func() {
		poller.Drain()
		close(drainDone)
	}()

	select {
	case <-drainDone:
		// Good, Drain returned
	case <-time.After(200 * time.Millisecond):
		t.Error("Drain should have returned")
	}
}

func TestPollerClearProcessedWithStore(t *testing.T) {
	client := NewClient(testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	config := &Config{PilotTag: "pilot"}
	store := NewMockProcessedStore()

	poller := NewPoller(client, config, 30*time.Second,
		WithProcessedStore(store),
	)

	// Mark task as processed
	poller.markProcessed("task-1")

	// Verify it's in both memory and store
	if !poller.IsProcessed("task-1") {
		t.Error("expected task-1 to be processed in memory")
	}
	processed, _ := store.IsProcessed("asana", "", "task-1")
	if !processed {
		t.Error("expected task-1 to be processed in store")
	}

	// Clear processed
	poller.ClearProcessed("task-1")

	// Verify it's cleared from both memory and store
	if poller.IsProcessed("task-1") {
		t.Error("expected task-1 to be cleared from memory")
	}
	processed, _ = store.IsProcessed("asana", "", "task-1")
	if processed {
		t.Error("expected task-1 to be cleared from store")
	}
}

func TestPollerParallelExecution(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/workspaces/"+testutil.FakeAsanaWorkspaceID+"/tags" {
			resp := PagedResponse[Tag]{
				Data: []Tag{{GID: "tag-pilot", Name: "pilot"}},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		if r.URL.Path == "/tags/tag-pilot/tasks" {
			resp := PagedResponse[Task]{
				Data: []Task{
					{
						GID:       "task-1",
						Name:      "Parallel task 1",
						Completed: false,
						Tags:      []Tag{{Name: "pilot"}},
						CreatedAt: time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
					},
					{
						GID:       "task-2",
						Name:      "Parallel task 2",
						Completed: false,
						Tags:      []Tag{{Name: "pilot"}},
						CreatedAt: time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC),
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	config := &Config{PilotTag: "pilot"}

	var processedTasks []string
	var mu sync.Mutex

	poller := NewPoller(client, config, 30*time.Second,
		WithOnAsanaTask(func(ctx context.Context, task *Task) (*TaskResult, error) {
			time.Sleep(20 * time.Millisecond) // simulate work
			mu.Lock()
			processedTasks = append(processedTasks, task.GID)
			mu.Unlock()
			return &TaskResult{Success: true}, nil
		}),
		WithMaxConcurrent(2),
	)

	ctx := context.Background()

	// Cache tags first
	if err := poller.cacheTagGIDs(ctx); err != nil {
		t.Fatalf("cacheTagGIDs() failed: %v", err)
	}

	// Check for new tasks (should dispatch both in parallel)
	poller.checkForNewTasks(ctx)

	// Wait for all parallel executions to complete
	poller.WaitForActive()

	mu.Lock()
	defer mu.Unlock()

	if len(processedTasks) != 2 {
		t.Errorf("expected 2 processed tasks, got %d", len(processedTasks))
	}

	// Both tasks should be processed
	expectedTasks := map[string]bool{"task-1": false, "task-2": false}
	for _, gid := range processedTasks {
		if _, exists := expectedTasks[gid]; !exists {
			t.Errorf("unexpected processed task: %s", gid)
		}
		expectedTasks[gid] = true
	}

	for gid, processed := range expectedTasks {
		if !processed {
			t.Errorf("task %s was not processed", gid)
		}
	}
}

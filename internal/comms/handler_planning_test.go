package comms

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/executor"
)

// fakePlanExecutor is a minimal taskExecutor stand-in for handlePlanning tests.
// It returns a canned result/error and records the task it was handed.
type fakePlanExecutor struct {
	result   *executor.ExecutionResult
	err      error
	lastTask *executor.Task
	calls    int
}

func (f *fakePlanExecutor) Execute(_ context.Context, task *executor.Task) (*executor.ExecutionResult, error) {
	f.calls++
	f.lastTask = task
	return f.result, f.err
}

func (f *fakePlanExecutor) Config() *executor.BackendConfig                       { return nil }
func (f *fakePlanExecutor) AddProgressCallback(string, executor.ProgressCallback) {}
func (f *fakePlanExecutor) RemoveProgressCallback(string)                         {}

func TestHandlePlanning_StoresPendingTask(t *testing.T) {
	m := &handlerMock{}
	h := newTestHandler(m)
	h.runner = &fakePlanExecutor{
		result: &executor.ExecutionResult{
			Success: true,
			Output:  "1. Modify foo.go\n2. Add bar test",
		},
	}

	h.handlePlanning(context.Background(), "ch1", "thread1", "add a feature")

	h.mu.Lock()
	pending, exists := h.pendingTasks["ch1"]
	h.mu.Unlock()

	if !exists {
		t.Fatal("expected a pending task to be stored after a successful plan")
	}
	if pending.TaskID == "" {
		t.Error("expected pending task to carry a task ID")
	}
	if pending.ContextID != "ch1" {
		t.Errorf("expected ContextID ch1, got %s", pending.ContextID)
	}
	if pending.ThreadID != "thread1" {
		t.Errorf("expected ThreadID thread1, got %s", pending.ThreadID)
	}
	if !strings.Contains(pending.Description, "1. Modify foo.go") {
		t.Errorf("expected plan content in description, got %q", pending.Description)
	}
	if !strings.Contains(pending.Description, "add a feature") {
		t.Errorf("expected original request in description, got %q", pending.Description)
	}

	// A confirmation prompt should have been sent for the plan.
	if len(m.confirms) == 0 {
		t.Error("expected a confirmation to be sent for the plan")
	}
}

func TestHandlePlanning_NoPendingTaskOnExecuteError(t *testing.T) {
	m := &handlerMock{}
	h := newTestHandler(m)
	h.runner = &fakePlanExecutor{err: errors.New("backend exploded")}

	h.handlePlanning(context.Background(), "ch1", "thread1", "add a feature")

	h.mu.Lock()
	_, exists := h.pendingTasks["ch1"]
	h.mu.Unlock()

	if exists {
		t.Error("no pending task should be stored when planning fails")
	}
}

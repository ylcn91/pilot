package executor

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/ylcn91/pilot/internal/executor/workflow"
)

// discardLogger returns a logger that swallows output so a failing hook's
// warn line doesn't pollute test output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestRunWorkflowHook_FailingHookDoesNotAbort verifies that a hook script which
// exits non-zero is logged-and-swallowed: runWorkflowHook returns normally
// (it has no error return), so execution continues. The hook still runs — proven
// by the sentinel file it writes before exiting non-zero.
func TestRunWorkflowHook_FailingHookDoesNotAbort(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "ran")

	scripts := workflow.HookValue{"touch " + sentinel + "; exit 1"}

	// Must not panic / must return — a failing hook is warn-only.
	runWorkflowHook(context.Background(), "before_run", scripts, dir, os.Environ(), discardLogger())

	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("hook did not run before failing: sentinel missing (%v)", err)
	}
}

// TestRunWorkflowHook_SucceedingHookRuns verifies a passing hook executes and its
// side effect is observable, and that an empty hook list is a no-op.
func TestRunWorkflowHook_SucceedingHookRuns(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.txt")

	scripts := workflow.HookValue{"echo ok > " + out}
	runWorkflowHook(context.Background(), "after_run", scripts, dir, os.Environ(), discardLogger())

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read hook output: %v", err)
	}
	if got := string(data); got == "" {
		t.Error("hook output file is empty; hook did not run")
	}

	// Empty hook list is a no-op (no panic, returns immediately).
	runWorkflowHook(context.Background(), "after_run", nil, dir, os.Environ(), discardLogger())
}

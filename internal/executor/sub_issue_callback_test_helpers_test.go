package executor

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
)

// subIssuePRCall records a single invocation of the SubIssuePRCallback.
type subIssuePRCall struct {
	PRNumber    int
	PRURL       string
	IssueNumber int
	CommitSHA   string
	BranchName  string
}

// newTestRunnerWithExecFunc creates a Runner that uses the given function
// instead of r.Execute for sub-issue execution. This avoids the full
// backend/git/webhook stack, making ExecuteSubIssues unit-testable.
func newTestRunnerWithExecFunc(execFn func(ctx context.Context, task *Task) (*ExecutionResult, error)) *Runner {
	return &Runner{
		config: &BackendConfig{
			ClaudeCode: &ClaudeCodeConfig{
				Command: "echo", // unused, but prevents nil panics
			},
		},
		running:           make(map[string]*exec.Cmd),
		progressCallbacks: make(map[string]ProgressCallback),
		tokenCallbacks:    make(map[string]TokenCallback),
		log:               slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		modelRouter:       NewModelRouter(nil, nil),
		executeFunc:       execFn,
		dryRun:            true,
	}
}

package executor

import (
	"context"
	"log/slog"
	"time"

	"github.com/ylcn91/pilot/internal/executor/workflow"
	"github.com/ylcn91/pilot/internal/replay"
)

// executeState threads the locals shared across the phases of
// executeWithOptions. Each field is produced in one phase and consumed in a
// later one; locals confined to a single phase stay local to that phase's
// method. This is a pure structural carrier — it adds no behavior.
type executeState struct {
	// Lifecycle / inputs
	start time.Time
	task  *Task
	ctx   context.Context

	// Worktree + execution path (GH-936)
	executionPath      string
	cleanupWorktree    func()
	beforeRemoveHookFn func()

	// Routing / logging
	complexity     Complexity
	timeout        time.Duration
	cancel         context.CancelFunc
	log            *slog.Logger
	selectedModel  string
	selectedEffort string

	// Git + repo state
	git       *GitOperations
	agentPath string

	// Research + workflow (GH-217, TASK-304/305)
	researchResult   *ResearchResult
	workflowMaxTurns int
	repoWorkflow     *workflow.Workflow
	hookEnv          []string

	// Pipeline plan stage (opt-in): raw spec injected into the execute prompt.
	planOutput string

	// Prompt + progress + recording
	prompt          string
	state           *progressState
	recorder        *replay.Recorder
	hookRestoreFunc func() error

	// Execution + result
	watchdogTimeout time.Duration
	backendResult   *BackendResult
	duration        time.Duration
	result          *ExecutionResult

	// Success-path threading
	qualityGatesPassed bool // GH-1079: quality gates passed → drives self-review decision
}

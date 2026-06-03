package autopilot

import (
	"context"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/memory"
)

// approvalPersister is the subset of memory.Store used for approval persistence
// in the executions table.
type approvalPersister interface {
	SetApprovalRequestID(ctx context.Context, taskID, requestID string) error
	SetApprovalDecision(ctx context.Context, requestID, decision, by string) error
}

// projectBoardSyncer abstracts GitHub Projects V2 board status updates.
// *github.ProjectBoardSync implements this interface; tests substitute a mock.
type projectBoardSyncer interface {
	UpdateProjectItemStatus(ctx context.Context, issueNodeID string, statusName string) error
}

// Notifier sends autopilot notifications for PR lifecycle events.
type Notifier interface {
	// NotifyMerged sends notification when a PR is successfully merged.
	NotifyMerged(ctx context.Context, prState *PRState) error
	// NotifyCIFailed sends notification when CI checks fail.
	NotifyCIFailed(ctx context.Context, prState *PRState, failedChecks []string) error
	// NotifyApprovalRequired sends notification when a PR requires human approval.
	NotifyApprovalRequired(ctx context.Context, prState *PRState) error
	// NotifyFixIssueCreated sends notification when a fix issue is auto-created.
	NotifyFixIssueCreated(ctx context.Context, prState *PRState, issueNumber int) error
	// NotifyReleased sends notification when a release is created.
	NotifyReleased(ctx context.Context, prState *PRState, releaseURL string) error
}

// ReleaseNotifier extends Notifier with release notifications.
type ReleaseNotifier interface {
	Notifier
	// NotifyReleased sends notification when a release is created.
	NotifyReleased(ctx context.Context, prState *PRState, releaseURL string) error
}

// TaskMonitor allows autopilot to update task display state.
// GH-1336: Sync monitor state when autopilot merges PR so dashboard shows correct status.
type TaskMonitor interface {
	Complete(taskID, prURL string)
}

// ExecutionHealer promotes execution rows after externally successful merges.
type ExecutionHealer interface {
	// SelfHealExecutionAfterMerge promotes failed rows to completed and
	// stamps the PR URL after a successful merge. projectPath scopes the update
	// to prevent cross-repo clobbering. GH-2402.
	SelfHealExecutionAfterMerge(taskID, projectPath, prURL string) error
}

// ControllerOption is a functional option for Controller configuration.
type ControllerOption func(*Controller)

// WithProjectBoardSync wires a GitHub Projects V2 board sync into the controller.
// doneStatus: merged PRs; failStatus: CI/exec failures; reviewStatus: PR created (In Progress → Review);
// inProgressStatus: reserved for future use (wired for symmetry, not yet emitted).
func WithProjectBoardSync(bs *github.ProjectBoardSync, doneStatus, failStatus, reviewStatus, inProgressStatus string) ControllerOption {
	return func(c *Controller) {
		c.boardSync = bs
		c.doneStatus = doneStatus
		c.failStatus = failStatus
		c.reviewStatus = reviewStatus
		c.inProgressStatus = inProgressStatus
	}
}

// WithMemoryStore wires an execution-level approval persister so that
// approval_request_id and approval_decision are written to the executions table.
func WithMemoryStore(s *memory.Store) ControllerOption {
	return func(c *Controller) {
		c.memoryStore = s
		c.executionHealer = s
	}
}

// WithProjectPath sets the filesystem project path used to scope execution
// self-heal (SelfHealExecutionAfterMerge) to this project's rows. It MUST match
// the value the executor stored in executions.project_path — an absolute fs path
// (e.g. /Users/me/proj), NOT owner/repo. Empty falls back to task_id-only match.
// TASK-352.
func WithProjectPath(path string) ControllerOption {
	return func(c *Controller) {
		c.projectPath = path
	}
}

// SetNotifier sets the notifier for autopilot events.
// This is optional; if not set, no notifications will be sent.
func (c *Controller) SetNotifier(n Notifier) {
	c.notifier = n
}

// SetMonitor sets the task monitor for dashboard state sync.
// GH-1336: When autopilot merges a PR, it updates monitor state so dashboard
// shows correct "done" status instead of stale "failed" from earlier execution attempts.
func (c *Controller) SetMonitor(m TaskMonitor) {
	c.monitor = m
}

// SetStateStore sets the persistent state store for crash recovery.
// If set, all state transitions are persisted to SQLite.
func (c *Controller) SetStateStore(store *StateStore) {
	c.stateStore = store
}

// SetLearningLoop sets the learning loop for capturing PR review feedback.
// When set, handleMerged will fetch reviews after merge and extract patterns.
func (c *Controller) SetLearningLoop(loop *memory.LearningLoop) {
	c.learningLoop = loop
	// GH-1979: Forward to feedback loop so fix issues can be annotated with known patterns.
	if c.feedbackLoop != nil {
		c.feedbackLoop.SetLearningLoop(loop)
	}
}

// SetExecutionHealer sets the execution healer used after successful merges.
func (c *Controller) SetExecutionHealer(healer ExecutionHealer) {
	c.executionHealer = healer
}

// SetMemoryStore wires an execution-level approval persister so that
// approval_request_id and approval_decision are written to the executions table.
func (c *Controller) SetMemoryStore(s *memory.Store) {
	c.memoryStore = s
	c.executionHealer = s
}

// SetReleaseSummaryGenerator sets the LLM release summary generator.
// When set, handleReleasing will enrich GitHub releases with a human-friendly summary.
func (c *Controller) SetReleaseSummaryGenerator(gen *ReleaseSummaryGenerator) {
	c.releaseSummary = gen
}

// SetOnIssueDone registers a callback invoked after a PR merges and pilot-done
// is applied. The callback receives the issue number and should mark it as
// processed in every active poller so the merge→done window cannot trigger
// phantom re-dispatch. GH-3271.
func (c *Controller) SetOnIssueDone(fn func(issueNumber int)) {
	c.onIssueDone = fn
}

// SetGuardrailsGate wires the per-PR architectural guardrails gate. When set and
// enabled, handleCIPassed runs it as a fail-open, report-only-by-default check
// that posts a pilot/guardrails commit status + PR comment. It never blocks the
// merge path: a nil or disabled gate is a no-op, and any error inside the gate
// is swallowed so guardrails can only add information, never break autopilot.
func (c *Controller) SetGuardrailsGate(g *GuardrailsGate) {
	c.guardrailsGate = g
}

// GuardrailsGate returns the wired guardrails gate, or nil when none is
// attached (the dormant default). It exists so the composition root can verify
// what it injected without reaching into unexported state; callers MUST treat it
// as read-only.
func (c *Controller) GuardrailsGate() *GuardrailsGate {
	return c.guardrailsGate
}

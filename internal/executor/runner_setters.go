package executor

import (
	"context"

	"github.com/ylcn91/pilot/internal/memory"
	"github.com/ylcn91/pilot/internal/webhooks"
)

// SetBackend changes the execution backend.
func (r *Runner) SetBackend(backend Backend) {
	r.backend = backend
}

// GetBackend returns the current execution backend.
func (r *Runner) GetBackend() Backend {
	return r.backend
}

// SetRecordingsPath sets a custom directory path for storing execution recordings.
// If not set, recordings are stored in the default location (~/.pilot/recordings).
func (r *Runner) SetRecordingsPath(path string) {
	r.recordingsPath = path
}

// SetRecordingEnabled enables or disables execution recording.
// When enabled, all Claude Code stream events are captured for replay and debugging.
func (r *Runner) SetRecordingEnabled(enabled bool) {
	r.enableRecording = enabled
}

// SetSkipPreflightChecks disables preflight checks (for testing with mock backends).
func (r *Runner) SetSkipPreflightChecks(skip bool) {
	r.skipPreflightChecks = skip
}

// InitWorktreePool initializes the worktree pool for a given repository.
// Should be called before executing tasks when worktree pooling is enabled.
// GH-1078: Saves 500ms-2s per task by reusing pre-created worktrees.
func (r *Runner) InitWorktreePool(ctx context.Context, repoPath string) error {
	if r.config == nil || r.config.WorktreePoolSize <= 0 {
		return nil // Pooling disabled
	}

	r.worktreeManager = NewWorktreeManagerWithPool(repoPath, r.config.WorktreePoolSize)
	return r.worktreeManager.WarmPool(ctx)
}

// CloseWorktreePool drains and closes the worktree pool.
// Should be called during graceful shutdown.
// GH-1078: Ensures clean shutdown without leaving orphaned worktrees.
func (r *Runner) CloseWorktreePool() {
	if r.worktreeManager != nil {
		r.worktreeManager.Close()
	}
}

// SetAlertProcessor sets the alert processor for emitting task lifecycle events.
// When set, the runner will emit events for task started, completed, and failed.
// The processor interface is satisfied by alerts.Engine.
func (r *Runner) SetAlertProcessor(processor AlertEventProcessor) {
	r.alertProcessor = processor
}

// SetWebhookManager sets the webhook manager for delivering task lifecycle events.
// When set, the runner can dispatch webhook events for task started, progress,
// completed, failed, and PR created events to configured endpoints.
func (r *Runner) SetWebhookManager(mgr *webhooks.Manager) {
	r.webhooks = mgr
}

// SetQualityCheckerFactory sets the factory for creating quality checkers.
// The factory is called with the task ID and project path to create a checker
// that runs quality gates (build, test, lint) before PR creation.
func (r *Runner) SetQualityCheckerFactory(factory QualityCheckerFactory) {
	r.qualityCheckerFactory = factory
}

// SetModelRouter sets the model router for complexity-based model and timeout selection.
func (r *Runner) SetModelRouter(router *ModelRouter) {
	r.modelRouter = router
}

// SetParallelRunner sets the parallel runner for research phase execution (GH-217).
// When set and enabled, medium/complex tasks run parallel research subagents
// before the main implementation to gather codebase context.
func (r *Runner) SetParallelRunner(runner *ParallelRunner) {
	r.parallelRunner = runner
}

// EnableParallelResearch creates and configures a default parallel runner.
// This is a convenience method to enable parallel research with default settings.
func (r *Runner) EnableParallelResearch() {
	r.parallelRunner = NewParallelRunner(DefaultParallelConfig(), r.modelRouter)
	if r.config != nil && r.config.DefaultModel != "" {
		r.parallelRunner.SetDefaultModel(r.config.DefaultModel)
	}
}

// SetTeamChecker sets the team permission checker for RBAC enforcement (GH-634).
// When set, Execute() validates that Task.MemberID has the required permissions
// before proceeding. If not set, all tasks are allowed (backward compatible).
func (r *Runner) SetTeamChecker(tc TeamChecker) {
	r.teamChecker = tc
}

// SetDecomposer sets the task decomposer for auto-splitting complex tasks (GH-218).
// When set and enabled, complex tasks are decomposed into subtasks that run sequentially,
// with only the final subtask creating a PR.
func (r *Runner) SetDecomposer(decomposer *TaskDecomposer) {
	r.decomposer = decomposer
}

// EnableDecomposition creates and configures a default task decomposer.
// This is a convenience method to enable decomposition with default settings.
func (r *Runner) EnableDecomposition(config *DecomposeConfig) {
	if config == nil {
		config = DefaultDecomposeConfig()
		config.Enabled = true // Enable by default when called explicitly
	}
	r.decomposer = NewTaskDecomposer(config)
}

// SetTokenLimitCheck sets the per-task token/duration limit callback (GH-539).
// When set, the callback is invoked on each stream event with cumulative token counts.
// If the callback returns false, the execution context is cancelled and the task
// terminates with a budget-exceeded error.
func (r *Runner) SetTokenLimitCheck(cb TokenLimitCallback) {
	r.tokenLimitCheck = cb
}

// SetOnSubIssuePRCreated sets the callback invoked when a sub-issue PR is created
// during epic execution (GH-588). This allows the autopilot controller to track
// each sub-issue PR individually for CI monitoring and auto-merge.
func (r *Runner) SetOnSubIssuePRCreated(fn SubIssuePRCallback) {
	r.onSubIssuePRCreated = fn
}

// SetSubIssueMergeWait sets the function that blocks between sequential sub-issues until
// the previous sub-issue's PR is merged (GH-2178). When set, ExecuteSubIssues waits for
// each PR to merge before starting the next sub-issue, ensuring ordering is preserved.
func (r *Runner) SetSubIssueMergeWait(fn SubIssueMergeWaitFn) {
	r.subIssueMergeWait = fn
}

// HasSubIssueMergeWait reports whether a merge-wait function is wired.
func (r *Runner) HasSubIssueMergeWait() bool { return r.subIssueMergeWait != nil }

// SetSubIssuePollerSkip wires the callback that marks a newly-created GitHub
// sub-issue as already-processed in the poller so it is not re-dispatched (GH-3240).
func (r *Runner) SetSubIssuePollerSkip(fn SubIssuePollerSkipFn) {
	r.subIssuePollerSkip = fn
}

// SetSubIssueCreator sets the creator for sub-issues in external issue trackers (GH-1471).
// When set and the task's SourceAdapter is non-GitHub, CreateSubIssues will dispatch
// via this interface instead of using the gh CLI.
func (r *Runner) SetSubIssueCreator(creator SubIssueCreator) {
	r.subIssueCreator = creator
}

// SetPRCreator sets the creator for pull/merge requests in external forges.
func (r *Runner) SetPRCreator(creator PRCreator) {
	r.prCreator = creator
}

// SetSubIssueLinker sets the linker for native GitHub sub-issue linking (GH-2211).
// When set, createSubIssuesViaGitHub will call LinkSubIssue after each child issue is
// created to establish the native parent→child relationship. Failures are non-fatal
// (warn-level log only) — the text "Parent: GH-N" body marker remains as fallback.
func (r *Runner) SetSubIssueLinker(linker SubIssueLinker) {
	r.subIssueLinker = linker
}

// SetIntentJudge sets the intent judge for diff-vs-ticket alignment verification (GH-624).
func (r *Runner) SetIntentJudge(judge *IntentJudge) {
	r.intentJudge = judge
}

// SetKnowledgeStore sets the knowledge store for experiential memories (GH-994).
// When set, relevant memories are surfaced in the prompt and decisions are captured post-task.
func (r *Runner) SetKnowledgeStore(k *memory.KnowledgeStore) {
	r.knowledge = k
}

// SetProfileManager sets the profile manager for user preferences (GH-994).
// When set, user preferences (verbosity, code patterns) are applied to prompts.
func (r *Runner) SetProfileManager(pm *memory.ProfileManager) {
	r.profileManager = pm
}

// SetDriftDetector sets the drift detector for collaboration drift (GH-997).
// When set, prompts may include re-anchoring instructions if drift is detected.
func (r *Runner) SetDriftDetector(dd *DriftDetector) {
	r.driftDetector = dd
}

// SetMonitor sets the task monitor for state transitions.
// When set, Runner signals monitor.Start() when execution actually begins,
// enabling accurate queued→running transitions in the dashboard.
func (r *Runner) SetMonitor(m *Monitor) {
	r.monitor = m
}

// SetLogStore sets the memory store used for writing execution milestone log entries (GH-1599).
func (r *Runner) SetLogStore(store *memory.Store) {
	r.logStore = store
}

// SetLearningLoop sets the learning loop for post-execution pattern learning.
func (r *Runner) SetLearningLoop(loop LearningRecorder) {
	r.learningLoop = loop
}

// SetMetricsRecorder wires the Prometheus metrics recorder for token/cost/execution counters (GH-2855).
func (r *Runner) SetMetricsRecorder(rec MetricsRecorder) {
	r.metricsRecorder = rec
}

// SetPatternContext sets the pattern context for pre-execution pattern injection.
func (r *Runner) SetPatternContext(ctx *PatternContext) {
	r.patternContext = ctx
}

// HasLearningLoop reports whether a learning loop is wired.
func (r *Runner) HasLearningLoop() bool { return r.learningLoop != nil }

// HasPatternContext reports whether a pattern context is wired.
func (r *Runner) HasPatternContext() bool { return r.patternContext != nil }

// SetSelfReviewExtractor sets the extractor for self-review pattern learning (GH-1955).
func (r *Runner) SetSelfReviewExtractor(e SelfReviewExtractor) {
	r.selfReviewExtractor = e
}

// SetOutcomeTracker sets the outcome tracker for model escalation (GH-1991).
func (r *Runner) SetOutcomeTracker(t *memory.ModelOutcomeTracker) {
	r.outcomeTracker = t
}

// HasOutcomeTracker reports whether an outcome tracker is wired.
func (r *Runner) HasOutcomeTracker() bool { return r.outcomeTracker != nil }

// SetKnowledgeGraph sets the knowledge graph for execution learning recording (GH-2015).
func (r *Runner) SetKnowledgeGraph(kg KnowledgeGraphRecorder) {
	r.knowledgeGraph = kg
}

// HasKnowledgeGraph reports whether a knowledge graph is wired.
func (r *Runner) HasKnowledgeGraph() bool { return r.knowledgeGraph != nil }

// HasTokenLimitCheck reports whether a token limit check callback is wired.
func (r *Runner) HasTokenLimitCheck() bool { return r.tokenLimitCheck != nil }

// HasKnowledge reports whether a knowledge store is wired.
func (r *Runner) HasKnowledge() bool { return r.knowledge != nil }

// HasLogStore reports whether a log store is wired.
func (r *Runner) HasLogStore() bool { return r.logStore != nil }

// HasTeamChecker reports whether a team checker is wired.
func (r *Runner) HasTeamChecker() bool { return r.teamChecker != nil }

// HasQualityCheckerFactory reports whether a quality checker factory is wired.
func (r *Runner) HasQualityCheckerFactory() bool { return r.qualityCheckerFactory != nil }

// HasOnSubIssuePRCreated reports whether a sub-issue PR callback is wired.
func (r *Runner) HasOnSubIssuePRCreated() bool { return r.onSubIssuePRCreated != nil }

// HasDecomposer reports whether a task decomposer is wired.
func (r *Runner) HasDecomposer() bool { return r.decomposer != nil }

// HasMonitor reports whether a task monitor is wired.
func (r *Runner) HasMonitor() bool { return r.monitor != nil }

// HasAlertProcessor reports whether an alert processor is wired.
func (r *Runner) HasAlertProcessor() bool { return r.alertProcessor != nil }

// HasIntentJudge reports whether an intent judge is wired.
func (r *Runner) HasIntentJudge() bool { return r.intentJudge != nil }

// HasModelRouter reports whether a model router is wired.
func (r *Runner) HasModelRouter() bool { return r.modelRouter != nil }

// ModelRouter returns the model router (may be nil).
func (r *Runner) ModelRouter() *ModelRouter { return r.modelRouter }

// HasDriftDetector reports whether a drift detector is wired.
func (r *Runner) HasDriftDetector() bool { return r.driftDetector != nil }

// HasProfileManager reports whether a profile manager is wired.
func (r *Runner) HasProfileManager() bool { return r.profileManager != nil }

// HasParallelRunner reports whether a parallel runner is wired.
func (r *Runner) HasParallelRunner() bool { return r.parallelRunner != nil }

// HasSubIssueCreator reports whether a sub-issue creator is wired.
func (r *Runner) HasSubIssueCreator() bool { return r.subIssueCreator != nil }

package executor

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/ylcn91/pilot/internal/logging"
	"github.com/ylcn91/pilot/internal/memory"
	"github.com/ylcn91/pilot/internal/webhooks"
)

// Runner executes development tasks using an AI backend (Claude Code, OpenCode, etc.).
// It manages task lifecycle including branch creation, AI invocation,
// progress tracking, PR creation, and execution recording. Runner is safe for
// concurrent use and tracks all running tasks for cancellation support.
type Runner struct {
	backend Backend // AI execution backend
	// Per-stage backends for the optional handoff pipeline. When no pipeline is
	// configured all three point at the single `backend` instance, so behavior
	// is identical to the single-backend path.
	planBackend   Backend
	execBackend   Backend
	reviewBackend Backend
	// Per-role backends for the optional opt-in TDD run mode. When TDD is
	// disabled (or a role's StageConfig is nil) all four point at the single
	// `backend` instance, so behavior is identical to the non-TDD path.
	architectBackend      Backend
	testAuthorBackend     Backend
	implementerBackend    Backend
	qaBackend             Backend
	config                *BackendConfig
	onProgress            ProgressCallback
	progressCallbacks     map[string]ProgressCallback // Named callbacks for multi-listener support
	progressMu            sync.RWMutex                // Protects progressCallbacks
	tokenCallbacks        map[string]TokenCallback    // Named callbacks for token usage updates
	tokenMu               sync.RWMutex                // Protects tokenCallbacks
	mu                    sync.Mutex
	running               map[string]*exec.Cmd
	log                   *slog.Logger
	recordingsPath        string                                                          // Path to recordings directory (empty = default)
	enableRecording       bool                                                            // Whether to record executions
	alertProcessor        AlertEventProcessor                                             // Optional alert processor for event emission
	webhooks              *webhooks.Manager                                               // Optional webhook manager for event delivery
	qualityCheckerFactory QualityCheckerFactory                                           // Optional factory for creating quality checkers
	tddGateCheckerFactory TDDGateCheckerFactory                                           // Optional factory for TDD RED/GREEN single-test gates
	tddGoTestRunner       goTestRunnerFunc                                                // Optional seam for the per-test go-test -json runner (tests inject scripted results)
	modelRouter           *ModelRouter                                                    // Model and timeout routing based on complexity
	parallelRunner        *ParallelRunner                                                 // Optional parallel research runner (GH-217)
	decomposer            *TaskDecomposer                                                 // Optional task decomposer for complex tasks (GH-218)
	subtaskParser         *SubtaskParser                                                  // Haiku-based subtask parser; nil falls back to regex (GH-501)
	suppressProgressLogs  bool                                                            // Suppress slog output for progress (use when visual display is active)
	tokenLimitCheck       TokenLimitCallback                                              // Optional per-task token/duration limit check (GH-539)
	onSubIssuePRCreated   SubIssuePRCallback                                              // Optional callback when a sub-issue PR is created (GH-596)
	subIssueMergeWait     SubIssueMergeWaitFn                                             // Optional fn to block between sub-issues until PR is merged (GH-2178)
	subIssuePollerSkip    SubIssuePollerSkipFn                                            // GH-3240: marks sub-issues in poller so they aren't re-dispatched
	intentJudge           *IntentJudge                                                    // Optional intent judge for diff-vs-ticket alignment (GH-624)
	teamChecker           TeamChecker                                                     // Optional team RBAC checker (GH-633)
	executeFunc           func(ctx context.Context, task *Task) (*ExecutionResult, error) // Internal override for testing
	skipPreflightChecks   bool                                                            // Skip preflight checks (for testing with mock backends)
	retrier               *Retrier                                                        // Optional smart retry handler (GH-920)
	signalParser          *SignalParser                                                   // Structured signal parser v2 for progress extraction (GH-960)
	knowledge             *memory.KnowledgeStore                                          // Optional knowledge store for experiential memories (GH-994)
	profileManager        *memory.ProfileManager                                          // Optional profile manager for user preferences (GH-994)
	driftDetector         *DriftDetector                                                  // Optional drift detector for collaboration drift (GH-997)
	monitor               *Monitor                                                        // Optional monitor for state transitions (queued→running)
	taskProgress          map[string]int                                                  // Per-task progress high-water mark (monotonic enforcement)
	taskProgressMu        sync.RWMutex                                                    // Protects taskProgress
	// GH-1077: AGENTS.md caching
	agentsContent     string       // Cached AGENTS.md content, loaded once per Runner
	agentsProjectPath string       // Project path for agents cache (invalidate on change)
	agentsMu          sync.RWMutex // Protects agents cache
	// GH-1078: Worktree pooling
	worktreeManager *WorktreeManager // Optional worktree manager with pool support
	// GH-1471: SubIssueCreator for non-GitHub adapters
	subIssueCreator SubIssueCreator // Optional creator for sub-issues in external trackers
	prCreator       PRCreator       // Optional creator for MRs/PRs in external forges
	// GH-2211: SubIssueLinker for native GitHub sub-issue API linking
	subIssueLinker SubIssueLinker // Optional linker for native GitHub parent→child wiring
	// GH-1599: Execution log store for milestone entries
	logStore *memory.Store // Optional log store for writing execution milestones
	// GH-1811: Learning system (self-improvement)
	learningLoop        LearningRecorder            // Optional learning loop for pattern extraction + feedback
	patternContext      *PatternContext             // Optional pattern context for prompt injection
	selfReviewExtractor SelfReviewExtractor         // Optional extractor for self-review pattern learning (GH-1955)
	outcomeTracker      *memory.ModelOutcomeTracker // Optional outcome tracker for model escalation (GH-1991)
	// GH-2015: Knowledge graph integration for execution learnings
	knowledgeGraph KnowledgeGraphRecorder // Optional knowledge graph for cross-project learnings
	// GH-2256: Dry-run mode to suppress real gh CLI calls (issue close/comment)
	dryRun bool
	// GH-2363: Track consecutive title-rejection failures per issue so we stop
	// retrying and post a helpful comment after the 2nd identical rejection.
	titleRejections *titleRejectionTracker
	// openSubIssueCheck detects whether recent sub-issues for a parent already exist.
	// Injectable for testing; defaults to queryRecentSubIssues (gh CLI).
	openSubIssueCheck func(ctx context.Context, dir, parentID string) (bool, error)
	// recoverSubIssuesFn reconstructs existing sub-issues when ErrSubIssuesAlreadyExist is hit.
	// Injectable for testing; defaults to recoverExistingSubIssues (gh CLI).
	recoverSubIssuesFn func(ctx context.Context, dir, parentID string) ([]CreatedIssue, error)
	// planEpicFn overrides PlanEpic for testing; nil uses the real PlanEpic implementation.
	planEpicFn func(ctx context.Context, task *Task, executionPath string) (*EpicPlan, error)
	// planPipelineFn overrides the opt-in pipeline plan stage for testing;
	// nil runs the spec through the configured plan backend (r.planBackend.Execute).
	planPipelineFn func() (string, error)
	// GH-2855: Prometheus counters for tokens, cost, and executions.
	metricsRecorder MetricsRecorder
	// GH-3027 / TASK-286: Allowlist used to refuse `gh issue create` calls on
	// repos that are not in the user's configured project list. Set via
	// SetRepoAllowlist at construction time by cmd/pilot. When nil the
	// guardrail logs a one-shot WARN and (without PILOT_ALLOW_UNMANAGED_REPO=1)
	// refuses to create sub-issues — safe default for newly-wired call paths.
	repoAllowlist RepoAllowlist
	// issueCreationEnabled controls whether epic planning may create tracker
	// sub-issues. Default false; wired from executor.create_sub_issues.
	issueCreationEnabled bool
}

// SetRepoAllowlist injects the allowlist used by the sub-issue creation
// guardrail (TASK-286 / GH-3027). cmd/pilot wires this from the top-level
// *config.Config so the executor stays decoupled from concrete config types.
// Passing nil disables the allowlist (the guardrail then refuses unless
// PILOT_ALLOW_UNMANAGED_REPO=1 is set, which logs a WARN).
func (r *Runner) SetRepoAllowlist(allow RepoAllowlist) {
	r.repoAllowlist = allow
}

// SetIssueCreationEnabled controls whether CreateSubIssues may create tracker
// issues. It is disabled by default and wired from executor.create_sub_issues.
func (r *Runner) SetIssueCreationEnabled(enabled bool) {
	r.issueCreationEnabled = enabled
}

// IssueCreationEnabled reports whether tracker issue creation is enabled.
func (r *Runner) IssueCreationEnabled() bool {
	return r.issueCreationEnabled
}

// NewRunner creates a new Runner instance with the default backend.
// The Runner is ready to execute tasks immediately after creation.
func NewRunner() *Runner {
	config := DefaultBackendConfig()
	backend, err := NewBackend(config)
	if err != nil {
		backend = NewCodexExecBackend(nil)
	}
	log := logging.WithComponent("executor")
	return &Runner{
		backend:            backend,
		planBackend:        backend,
		execBackend:        backend,
		reviewBackend:      backend,
		architectBackend:   backend,
		testAuthorBackend:  backend,
		implementerBackend: backend,
		qaBackend:          backend,
		running:            make(map[string]*exec.Cmd),
		progressCallbacks:  make(map[string]ProgressCallback),
		tokenCallbacks:     make(map[string]TokenCallback),
		taskProgress:       make(map[string]int),
		log:                log,
		enableRecording:    true, // Recording enabled by default
		modelRouter:        NewModelRouter(nil, nil),
		signalParser:       NewSignalParser(log),
		titleRejections:    newTitleRejectionTracker(),
	}
}

// NewRunnerWithBackend creates a Runner with a specific backend.
func NewRunnerWithBackend(backend Backend) *Runner {
	if backend == nil {
		backend = NewCodexExecBackend(nil)
	}
	log := logging.WithComponent("executor")
	return &Runner{
		backend:            backend,
		planBackend:        backend,
		execBackend:        backend,
		reviewBackend:      backend,
		architectBackend:   backend,
		testAuthorBackend:  backend,
		implementerBackend: backend,
		qaBackend:          backend,
		running:            make(map[string]*exec.Cmd),
		progressCallbacks:  make(map[string]ProgressCallback),
		tokenCallbacks:     make(map[string]TokenCallback),
		taskProgress:       make(map[string]int),
		log:                log,
		enableRecording:    true,
		modelRouter:        NewModelRouter(nil, nil),
		signalParser:       NewSignalParser(log),
		titleRejections:    newTitleRejectionTracker(),
	}
}

// NewRunnerWithConfig creates a Runner from backend configuration.
func NewRunnerWithConfig(config *BackendConfig) (*Runner, error) {
	// Ensure we have a valid config (GH-956: nil config breaks worktree)
	if config == nil {
		slog.Warn("NewRunnerWithConfig called with nil config, using defaults")
		config = DefaultBackendConfig()
	} else {
		slog.Info("NewRunnerWithConfig",
			slog.Bool("use_worktree", config.UseWorktree),
			slog.String("type", config.Type),
		)
	}
	backend, err := NewBackend(config)
	if err != nil {
		return nil, err
	}
	runner := NewRunnerWithBackend(backend)
	runner.config = config
	runner.issueCreationEnabled = config.CreateSubIssues

	// Resolve per-stage backends for the optional handoff pipeline. With no
	// pipeline (or a nil stage) the field reuses the single `backend` instance,
	// so plan/exec/review are the identical pointer and behavior is unchanged.
	if err := runner.resolveStageBackends(config); err != nil {
		return nil, err
	}

	// Resolve per-role backends for the optional opt-in TDD mode. With TDD
	// disabled (or a nil role) each field reuses the single `backend`, so all
	// four roles are the identical pointer and behavior is unchanged.
	if err := runner.resolveTDDBackends(config); err != nil {
		return nil, err
	}

	// Configure model routing, timeouts, and effort from config
	if config != nil {
		runner.modelRouter = NewModelRouterWithEffort(config.ModelRouting, config.Timeout, config.EffortRouting)

		// GH-727: Attach LLM effort classifier if enabled
		// Uses Claude Code subprocess with Haiku - no ANTHROPIC_API_KEY needed
		if config.EffortClassifier != nil && config.EffortClassifier.Enabled {
			classifier := NewEffortClassifier()
			if config.EffortClassifier.Model != "" {
				classifier.model = config.EffortClassifier.Model
			}
			if config.EffortClassifier.Timeout != "" {
				if d, err := time.ParseDuration(config.EffortClassifier.Timeout); err == nil {
					classifier.timeout = d
				}
			}
			if config.ClaudeCode != nil {
				classifier.SetUseStructuredOutput(config.ClaudeCode.UseStructuredOutput)
			}
			if config.DefaultModel != "" {
				classifier.model = config.DefaultModel
			}
			if config.APIBaseURL != "" {
				classifier.apiURL = config.ResolveAPIBaseURL() + "/v1/messages"
			}
			runner.modelRouter.SetEffortClassifier(classifier)
			runner.log.Info("LLM effort classifier initialized",
				slog.String("model", classifier.model),
				slog.Duration("timeout", classifier.timeout),
			)
		}

		// Configure task decomposition (GH-218)
		if config.Decompose != nil && config.Decompose.Enabled {
			runner.decomposer = NewTaskDecomposer(config.Decompose)

			// GH-727, GH-868: Attach LLM complexity classifier using Claude Code subprocess
			// No ANTHROPIC_API_KEY needed - uses existing Claude Code subscription
			complexityClassifier := NewComplexityClassifier()
			if config.DefaultModel != "" {
				complexityClassifier.model = config.DefaultModel
			}
			if config.ClaudeCode != nil {
				complexityClassifier.SetUseStructuredOutput(config.ClaudeCode.UseStructuredOutput)
			}
			runner.decomposer.SetClassifier(complexityClassifier)
		}
	}

	// Initialize subtask parser using claude subprocess; nil if binary missing (GH-501, GH-2931)
	{
		claudeCmd := ""
		if config != nil && config.ClaudeCode != nil {
			claudeCmd = config.ClaudeCode.Command
		}
		if claudeCmd == "" {
			claudeCmd = "claude"
		}
		runner.subtaskParser = NewSubtaskParser(claudeCmd, runner.log)
		if runner.subtaskParser != nil && config != nil && config.DefaultModel != "" {
			runner.subtaskParser.model = config.DefaultModel
		}
	}

	// Initialize intent judge for diff-vs-ticket alignment (GH-624, GH-2817)
	// Uses Claude Code subprocess — bills to operator's CC subscription, no API key required.
	if config != nil && config.IntentJudge != nil && (config.IntentJudge.Enabled == nil || *config.IntentJudge.Enabled) {
		claudeCmd := ""
		if config.ClaudeCode != nil {
			claudeCmd = config.ClaudeCode.Command
		}
		if claudeCmd == "" {
			claudeCmd = "claude"
		}
		if _, err := exec.LookPath(claudeCmd); err != nil {
			runner.log.Warn("Intent judge disabled: claude binary not found", slog.String("command", claudeCmd))
		} else {
			runner.intentJudge = NewIntentJudge(claudeCmd)
			if config.IntentJudge.Model != "" {
				runner.intentJudge.model = config.IntentJudge.Model
			}
			runner.log.Info("Intent judge initialized", slog.String("model", runner.intentJudge.model))
		}
	} else if config != nil && config.IntentJudge == nil {
		runner.log.Debug("Intent judge disabled: no config")
	}

	// Initialize smart retrier (GH-920)
	if config != nil && config.Retry != nil {
		runner.retrier = NewRetrier(config.Retry)
	}

	// Initialize profile manager and drift detector (GH-1027)
	// Global profile: ~/.pilot/profile.json, Project profile: .agent/.user-profile.json
	homeDir, _ := os.UserHomeDir()
	globalProfilePath := filepath.Join(homeDir, ".pilot", "profile.json")
	// Note: project path will be resolved per-task; using empty default here
	runner.profileManager = memory.NewProfileManager(globalProfilePath, "")

	// Drift detector uses default threshold of 3 corrections within 30-minute window
	runner.driftDetector = NewDriftDetector(3, runner.profileManager)
	runner.log.Debug("Profile manager and drift detector initialized")

	return runner, nil
}

// resolveStageBackends wires planBackend/execBackend/reviewBackend from the
// optional pipeline. Each stage that is configured gets its own Backend built
// via NewStageBackend; every other stage falls back to the run's single
// backend, so a nil pipeline leaves all three equal to r.backend (zero behavior
// change vs. the single-backend path).
func (r *Runner) resolveStageBackends(config *BackendConfig) error {
	r.planBackend = r.backend
	r.execBackend = r.backend
	r.reviewBackend = r.backend
	if config == nil || config.Pipeline == nil {
		return nil
	}
	stages := []struct {
		stage  *StageConfig
		target *Backend
	}{
		{config.Pipeline.Plan, &r.planBackend},
		{config.Pipeline.Execute, &r.execBackend},
		{config.Pipeline.Review, &r.reviewBackend},
	}
	for _, s := range stages {
		if s.stage == nil {
			continue
		}
		b, err := NewStageBackend(s.stage, *config)
		if err != nil {
			return err
		}
		*s.target = b
	}
	return nil
}

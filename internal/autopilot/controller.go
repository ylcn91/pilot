package autopilot

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/approval"
	"github.com/ylcn91/pilot/internal/memory"
)

// prFailureState tracks per-PR circuit breaker state.
// Each PR has independent failure tracking so one bad PR doesn't block others.
type prFailureState struct {
	FailureCount    int       // Number of consecutive failures for this PR
	LastFailureTime time.Time // When the last failure occurred (for timeout reset)
}

// Controller orchestrates the autopilot loop for PR processing.
// It manages the state machine: PR created → CI check → merge → post-merge CI → feedback loop.
type Controller struct {
	config           *Config
	ghClient         *github.Client
	approvalMgr      *approval.Manager
	ciMonitor        *CIMonitor
	autoMerger       *AutoMerger
	feedbackLoop     *FeedbackLoop
	releaser         *Releaser
	notifier         Notifier
	monitor          TaskMonitor // GH-1336: sync dashboard state on merge
	boardSync        projectBoardSyncer
	doneStatus       string
	failStatus       string
	reviewStatus     string // GH-3260: board column for PR-created (In Progress → Review)
	inProgressStatus string // GH-3260: reserved for symmetry; not yet emitted
	log              *slog.Logger

	// State tracking
	activePRs map[int]*PRState
	mu        sync.RWMutex

	// Merge-metric idempotency: tracks PR numbers we've already recorded
	// merge-success metrics for, so handleMerging + ScanRecentlyMergedPRs
	// can both call recordMergeSuccess without double-counting.
	recordedMerges map[int]bool

	// Post-merge reinforcement idempotency: tracks PR numbers we've already
	// reinforced patterns for. Separate from recordedMerges because the metrics
	// flag is set early in handleMerging (before reinforcement runs), so reusing
	// it would skip reinforcement entirely (A4 idempotency bug).
	recordedReinforcements map[int]bool

	// Persistent state store (optional, nil = in-memory only)
	stateStore *StateStore

	// Learning loop for capturing review feedback (optional, nil = learning disabled)
	learningLoop *memory.LearningLoop

	// Execution healer for reconciling execution rows after successful merges.
	executionHealer ExecutionHealer

	// Execution-level approval persistence (optional, nil = audit trail disabled)
	memoryStore approvalPersister

	// Per-PR circuit breaker: each PR has independent failure tracking.
	// A failure on one PR does not block other PRs.
	prFailures map[int]*prFailureState

	// Deadlock detection (GH-849): track last time any PR made progress.
	// If no state transitions occur for 1h, fire a deadlock alert.
	lastProgressAt    time.Time
	deadlockAlertSent bool

	// Release summary generator (optional, nil = no LLM enrichment)
	releaseSummary *ReleaseSummaryGenerator

	// Metrics
	metrics *Metrics

	// Owner and repo for GitHub operations
	owner string
	repo  string

	// projectPath is the absolute filesystem path the executor stored in
	// executions.project_path. Used to scope self-heal to this project's rows.
	// Empty = match by task_id only (single-repo / tests). TASK-352.
	projectPath string

	// GH-3271: called after a PR merges and pilot-done is applied so pollers
	// can immediately re-mark the issue as processed, closing the merge→done
	// race window before label propagation catches up.
	onIssueDone func(issueNumber int)

	// Cached authenticated GitHub login for guarding human recovery PRs.
	cachedBotLogin string

	// guardrailsGate evaluates repo-specific architectural rules per PR and
	// surfaces them as a commit status + PR comment. Optional and fail-open:
	// nil or disabled => handleCIPassed never touches it. It NEVER influences
	// the merge decision (blocking is expressed only through its commit status),
	// so existing behaviour is preserved when it is unset.
	guardrailsGate *GuardrailsGate
}

// NewController creates an autopilot controller with all required components.
func NewController(cfg *Config, ghClient *github.Client, approvalMgr *approval.Manager, owner, repo string, opts ...ControllerOption) *Controller {
	c := &Controller{
		config:                 cfg,
		ghClient:               ghClient,
		approvalMgr:            approvalMgr,
		owner:                  owner,
		repo:                   repo,
		activePRs:              make(map[int]*PRState),
		recordedMerges:         make(map[int]bool),
		recordedReinforcements: make(map[int]bool),
		prFailures:             make(map[int]*prFailureState),
		lastProgressAt:         time.Now(), // Initialize to now to avoid false alarm on startup
		metrics:                NewMetrics(),
		log:                    slog.Default().With("component", "autopilot"),
	}

	c.ciMonitor = NewCIMonitor(ghClient, owner, repo, cfg)
	c.autoMerger = NewAutoMerger(ghClient, approvalMgr, c.ciMonitor, owner, repo, cfg)
	c.feedbackLoop = NewFeedbackLoop(ghClient, owner, repo, cfg)

	// Initialize releaser from resolved release config (env-scoped wins over global).
	// Mirrors resolvedRelease() so construction matches runtime decision path.
	if relCfg := resolveRelease(cfg); relCfg != nil && relCfg.Enabled {
		c.releaser = NewReleaser(ghClient, owner, repo, relCfg)
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// OnPRCreated registers a new PR for autopilot processing.
func (c *Controller) OnPRCreated(prNumber int, prURL string, issueNumber int, headSHA string, branchName string, issueNodeID string) {
	c.mu.Lock()
	prState := &PRState{
		PRNumber:        prNumber,
		PRURL:           prURL,
		IssueNumber:     issueNumber,
		BranchName:      branchName,
		HeadSHA:         headSHA,
		Stage:           StagePRCreated,
		CIStatus:        CIPending,
		CreatedAt:       time.Now(),
		EnvironmentName: c.config.EnvironmentName(),
		IssueNodeID:     issueNodeID,
	}
	c.activePRs[prNumber] = prState
	c.mu.Unlock()

	// Persist to SQLite (idempotent, safe outside lock).
	// TASK-324: prState is now published in activePRs, so a concurrent ProcessPR or
	// webhook could already hold a reference. Take prState.mu for the persist to honor
	// the persistPRState contract (caller holds prState.mu). c.mu is already released,
	// so the prState.mu→c.mu ordering invariant holds.
	prState.mu.Lock()
	c.persistPRState(prState)
	prState.mu.Unlock()

	c.log.Info("PR registered for autopilot",
		"pr", prNumber,
		"url", prURL,
		"issue", issueNumber,
		"branch", branchName,
		"sha", ShortSHA(headSHA),
		"stage", StagePRCreated,
		"env", c.config.EnvironmentName(),
	)

	// GH-3260: Sync board card to "In Review" column when PR is created (In Progress → Review).
	// Board sync is a non-critical side-effect; failure is logged but does not block registration.
	if c.boardSync != nil && issueNodeID != "" && c.reviewStatus != "" {
		if err := c.boardSync.UpdateProjectItemStatus(context.Background(), issueNodeID, c.reviewStatus); err != nil {
			c.log.Warn("board sync on PR created failed", "pr", prNumber, "error", err)
		}
	}
}

// OnReviewRequested handles PR review events from GitHub webhooks.
// For changes_requested reviews on tracked PRs, it transitions the PR to StageReviewRequested
// so the next processAllPRs tick will create a revision issue.
func (c *Controller) OnReviewRequested(prNumber int, action, state, reviewer string) {
	c.mu.RLock()
	prState, tracked := c.activePRs[prNumber]
	c.mu.RUnlock()

	c.log.Info("PR review received",
		"pr", prNumber,
		"action", action,
		"state", state,
		"reviewer", reviewer,
		"tracked", tracked,
	)

	if !tracked {
		return
	}

	// Only act on changes_requested reviews
	if state != "changes_requested" {
		return
	}

	// Check if review feedback handling is enabled
	if c.config.ReviewFeedback == nil || !c.config.ReviewFeedback.Enabled {
		c.log.Info("review feedback handling disabled, ignoring changes_requested",
			"pr", prNumber,
			"reviewer", reviewer,
		)
		return
	}

	// TASK-324: guard the read of prState.Stage (for the log), the Stage write, and
	// the persist under the per-PR mutex. The pointer was fetched under c.mu above and
	// c.mu has since been released, so taking prState.mu here keeps the no-deadlock
	// invariant (prState.mu before c.mu, never the reverse).
	prState.mu.Lock()
	c.log.Warn("Changes requested on PR, transitioning to review_requested stage",
		"pr", prNumber,
		"reviewer", reviewer,
		"current_stage", prState.Stage,
	)
	prState.Stage = StageReviewRequested
	c.persistPRState(prState)
	prState.mu.Unlock()
}

// ProcessPR processes a single PR through the state machine.
// Returns error if processing fails; caller should retry based on error type.
// Accepts optional cached ghPR to avoid redundant API calls.
func (c *Controller) ProcessPR(ctx context.Context, prNumber int, ghPR *github.PullRequest) error {
	c.mu.RLock()
	prState, ok := c.activePRs[prNumber]
	c.mu.RUnlock()

	if !ok {
		return fmt.Errorf("PR %d not tracked", prNumber)
	}

	// TASK-324: hold the per-PR mutex for the entire processing body. This single
	// lock covers all 11 handleX(prState) handlers, the inline PRTitle/TargetBranch/
	// Error writes, and the persistPRState call, serialising the main loop against
	// webhook writers (OnReviewRequested, SetApprovalDecision) on the same PR.
	// Lock ordering: we hold prState.mu and may take c.mu below (isPRCircuitOpen,
	// recordPRFailure/resetPRFailures, the lastProgressAt update, removePR via
	// handlers). Never the reverse — see the no-deadlock invariant on PRState.
	prState.mu.Lock()
	defer prState.mu.Unlock()

	// Per-PR circuit breaker check
	if c.isPRCircuitOpen(prNumber) {
		c.log.Warn("per-PR circuit breaker open", "pr", prNumber)
		c.metrics.RecordCircuitBreakerTrip()
		return fmt.Errorf("circuit breaker: PR %d has too many consecutive failures", prNumber)
	}

	// Populate PR metadata from GitHub response when available
	if ghPR != nil {
		if prState.PRTitle == "" && ghPR.Title != "" {
			prState.PRTitle = ghPR.Title
		}
		if prState.TargetBranch == "" && ghPR.Base.Ref != "" {
			prState.TargetBranch = ghPR.Base.Ref
		}
	}

	previousStage := prState.Stage
	var err error

	switch prState.Stage {
	case StagePRCreated:
		err = c.handlePRCreated(ctx, prState, ghPR)
	case StageWaitingCI:
		err = c.handleWaitingCI(ctx, prState, ghPR)
	case StageCIPassed:
		err = c.handleCIPassed(ctx, prState)
	case StageCIFailed:
		err = c.handleCIFailed(ctx, prState)
	case StageAwaitApproval:
		err = c.handleAwaitApproval(ctx, prState)
	case StageMerging:
		err = c.handleMerging(ctx, prState)
	case StageMerged:
		err = c.handleMerged(ctx, prState)
	case StagePostMergeCI:
		err = c.handlePostMergeCI(ctx, prState)
	case StageReviewRequested:
		err = c.handleReviewRequested(ctx, prState)
	case StageReleasing:
		err = c.handleReleasing(ctx, prState)
	case StageFailed:
		// Terminal state - no processing
		return nil
	}

	// Log stage transitions and update progress timestamp for deadlock detection
	if prState.Stage != previousStage {
		c.log.Info("PR stage transition",
			"pr", prNumber,
			"from", previousStage,
			"to", prState.Stage,
			"env", c.config.EnvironmentName(),
		)

		// GH-849: Update lastProgressAt and reset deadlock alert flag
		c.mu.Lock()
		c.lastProgressAt = time.Now()
		c.deadlockAlertSent = false
		c.mu.Unlock()
	}

	if err != nil {
		c.recordPRFailure(prNumber)
		prState.Error = err.Error()
		c.log.Error("autopilot stage failed", "pr", prNumber, "stage", prState.Stage, "error", err)
	} else {
		c.resetPRFailures(prNumber)
	}

	// Persist state after every processing cycle (covers transitions and updated fields)
	c.persistPRState(prState)

	return err
}

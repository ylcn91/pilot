package github

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/skipreason"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/logging"
)

// ExecutionMode determines how issues are processed
type ExecutionMode string

const (
	// ExecutionModeSequential processes one issue at a time, waiting for PR merge
	ExecutionModeSequential ExecutionMode = "sequential"
	// ExecutionModeParallel processes issues concurrently (legacy behavior)
	ExecutionModeParallel ExecutionMode = "parallel"
	// ExecutionModeAuto uses parallel dispatch with scope-overlap guard:
	// non-overlapping issues run concurrently; overlapping groups run oldest-first.
	ExecutionModeAuto ExecutionMode = "auto"
)

// ProcessedStore persists which issues have been processed across restarts.
// Implemented by autopilot.StateStore to avoid circular imports.
type ProcessedStore interface {
	Mark(source, repo, issueID string) error
	Unmark(source, repo, issueID string) error
	IsProcessed(source, repo, issueID string) (bool, error)
	Load(source, repo string) (map[string]time.Time, error)
}

// TaskChecker checks whether a task is currently queued or in-progress.
// Used during retry grace-period evaluation to avoid re-dispatching issues
// that are still being executed.
type TaskChecker interface {
	IsTaskQueued(taskID string) bool
}

// ExecutionChecker verifies whether a completed execution exists for a task.
// GH-2242: Prevents re-dispatch of completed tasks when pilot-done label is missing.
type ExecutionChecker interface {
	HasCompletedExecution(taskID, projectPath string) (bool, error)
}

// Verdict is the poller-side result of a pre-flight judgment.
// Mirrors executor.PreFlightVerdict but keeps the poller decoupled from the executor
// package to avoid import cycles.
type Verdict struct {
	Accepted   bool
	Decision   string
	Reason     string
	Confidence float64
}

// PreFlightJudger evaluates issues before dispatch to avoid burning worker slots on
// vague, ambiguous, or otherwise unactionable issues (GH-2802).
// Implemented by a shim in cmd/pilot/main.go that wraps *executor.IntentJudge.
type PreFlightJudger interface {
	JudgeIssue(ctx context.Context, title, body, repoContext string) (Verdict, error)
}

// ExecutionSaver persists pre-flight rejection records for observability.
type ExecutionSaver interface {
	SaveDeclinedExecution(taskID, projectPath, status, reason string) error
}

// IssueMetricsRecorder records issue processing outcomes.
// Implemented by *autopilot.Metrics; kept as an interface here to avoid circular imports.
type IssueMetricsRecorder interface {
	RecordIssueProcessed(result string)
}

// DispatchPolicy groups the knobs that govern whether and when an issue is
// dispatched: retry strategy (failed + retry-ready budgets, grace period,
// task-queued guard), the pre-flight quality judge, and the completed-execution
// guard. Extracted from the Poller god struct; behavior is unchanged — these
// were previously flat fields on Poller.
type DispatchPolicy struct {
	// GH-2201: Retry grace period prevents rapid re-dispatch of recently-processed
	// issues. When a processed issue's status labels are removed, the poller waits
	// this duration before allowing retry. Default: 5 minutes.
	retryGracePeriod time.Duration

	// GH-2201: taskChecker verifies whether an issue is still queued/in-progress
	// before allowing retry after the grace period expires.
	taskChecker TaskChecker

	// GH-2176: Auto-retry issues stuck with pilot-failed from execution failures.
	// failedRetryCount tracks how many times each issue has been retried after
	// pilot-failed; maxFailedRetries caps it (default: 3).
	failedRetryCount map[int]int
	maxFailedRetries int

	// GH-2276/GH-2432: Auto-retry issues with pilot-retry-ready (PR closed without
	// merge). The budget is tracked via GitHub labels (pilot-retry-1/2/exhausted);
	// maxRetryReadyRetries records the configured cap (default: 3).
	maxRetryReadyRetries int

	// GH-2802: Pre-flight judge evaluates issues before dispatch. nil means
	// disabled (config flag executor.pre_flight_judge.enabled=false). execSaver
	// persists pre-flight rejection records for observability.
	preFlightJudge PreFlightJudger
	execSaver      ExecutionSaver

	// GH-2242: execChecker prevents re-dispatch of completed tasks when the
	// pilot-done label failed to apply. projectPath scopes the lookup.
	execChecker ExecutionChecker
	projectPath string
}

// BoardConfig groups the Projects V2 board integration knobs: the candidate
// source column (GH-3228) and the in-progress write-back on dispatch (GH-3252).
// Extracted from the Poller god struct; behavior is unchanged.
type BoardConfig struct {
	// projectBoardSource sources candidates from a Projects V2 board column
	// (GH-3228). When non-nil, replaces label-based ListIssues in
	// findOldestUnprocessedIssue.
	projectBoardSource *ProjectBoardSource

	// boardSync moves the issue card to inProgressStatus on confirmed dispatch
	// (GH-3252). nil or empty inProgressStatus disables the write-back, keeping
	// label-mode identical.
	boardSync        *ProjectBoardSync
	inProgressStatus string
}

// IssueResult is returned by the issue handler with PR information
type IssueResult struct {
	Success    bool
	PRNumber   int    // PR number if created
	PRURL      string // PR URL if created
	HeadSHA    string // Head commit SHA of the PR
	BranchName string // Head branch name (e.g. "pilot/GH-123")
	Error      error
}

// Poller polls GitHub for issues with a specific label
type Poller struct {
	client    *Client
	owner     string
	repo      string
	label     string
	interval  time.Duration
	processed map[int]time.Time
	mu        sync.RWMutex
	onIssue   func(ctx context.Context, issue *Issue) error
	// onIssueWithResult is called for sequential mode, returns PR info
	onIssueWithResult func(ctx context.Context, issue *Issue) (*IssueResult, error)
	// OnPRCreated is called when a PR is created after issue processing
	// Parameters: prNumber, prURL, issueNumber, headSHA, branchName, issueNodeID
	OnPRCreated func(prNumber int, prURL string, issueNumber int, headSHA string, branchName string, issueNodeID string)
	logger      *slog.Logger

	// Sequential mode configuration
	executionMode  ExecutionMode
	mergeWaiter    *MergeWaiter
	waitForMerge   bool
	prTimeout      time.Duration
	prPollInterval time.Duration

	// Rate limit retry scheduler
	scheduler *executor.Scheduler

	// Parallel mode configuration
	maxConcurrent int
	semaphore     chan struct{}
	activeWg      sync.WaitGroup
	stopping      atomic.Bool
	wgMu          sync.Mutex // protects stopping + activeWg Add/Wait coordination

	// Persistent processed store (optional)
	processedStore ProcessedStore

	// dispatch groups the retry strategy, pre-flight judge, and completed-execution
	// guard (GH-2176/2201/2242/2276/2432/2802).
	dispatch DispatchPolicy

	// metricsRecorder records issue processing outcomes (optional).
	metricsRecorder IssueMetricsRecorder

	// pollerMetrics records per-repo dispatch/skip counters (TASK-293, GH-3064).
	pollerMetrics skipreason.PollerMetricsRecorder

	// board groups the Projects V2 candidate source and in-progress write-back
	// (GH-3228/GH-3252).
	board BoardConfig
}

// NewPoller creates a new GitHub issue poller
func NewPoller(client *Client, repo string, label string, interval time.Duration, opts ...PollerOption) (*Poller, error) {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repo format, expected owner/repo: %s", repo)
	}

	p := &Poller{
		client:         client,
		owner:          parts[0],
		repo:           parts[1],
		label:          label,
		interval:       interval,
		processed:      make(map[int]time.Time),
		logger:         logging.WithComponent("github-poller"),
		executionMode:  ExecutionModeAuto, // Default matches config.DefaultExecutionConfig()
		waitForMerge:   true,
		prPollInterval: 30 * time.Second,
		prTimeout:      1 * time.Hour,
		dispatch: DispatchPolicy{
			retryGracePeriod:     5 * time.Minute, // GH-2201: default grace period
			failedRetryCount:     make(map[int]int),
			maxFailedRetries:     3, // GH-2176: default max retries for pilot-failed issues
			maxRetryReadyRetries: 3, // GH-2276: default max retries for pilot-retry-ready issues
		},
	}

	for _, opt := range opts {
		opt(p)
	}

	// Create merge waiter if in sequential mode
	if p.executionMode == ExecutionModeSequential && p.waitForMerge {
		p.mergeWaiter = NewMergeWaiter(client, p.owner, p.repo, &MergeWaiterConfig{
			PollInterval: p.prPollInterval,
			Timeout:      p.prTimeout,
		})
	}

	// Load processed issues from persistent store if available
	if p.processedStore != nil {
		loaded, err := p.processedStore.Load("github", p.repoKey())
		if err != nil {
			p.logger.Warn("Failed to load processed issues from store", slog.Any("error", err))
		} else if len(loaded) > 0 {
			p.mu.Lock()
			for idStr, t := range loaded {
				if num, parseErr := strconv.Atoi(idStr); parseErr == nil {
					p.processed[num] = t
				}
			}
			p.mu.Unlock()
			p.logger.Info("Loaded processed issues from store", slog.Int("count", len(loaded)))
		}
	}

	// Initialize parallel semaphore
	if p.maxConcurrent < 1 {
		p.maxConcurrent = 2 // default
	}
	p.semaphore = make(chan struct{}, p.maxConcurrent)

	return p, nil
}

// Start begins polling for issues
func (p *Poller) Start(ctx context.Context) {
	p.logger.Info("Starting GitHub poller",
		slog.String("repo", p.owner+"/"+p.repo),
		slog.String("label", p.label),
		slog.Duration("interval", p.interval),
		slog.String("mode", string(p.executionMode)),
	)

	// GH-1355: Recover orphaned in-progress issues from previous run before starting poll loop
	p.recoverOrphanedIssues(ctx)

	if p.executionMode == ExecutionModeSequential {
		p.startSequential(ctx)
	} else {
		// Both parallel and auto modes use startParallel; auto additionally
		// applies the scope-overlap guard (groupByOverlappingScope) which is
		// already built into checkForNewIssues.
		p.startParallel(ctx)
	}
}

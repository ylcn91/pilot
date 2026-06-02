package azuredevops

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/skipreason"
	"github.com/ylcn91/pilot/internal/logging"
)

// ExecutionMode determines how work items are processed
type ExecutionMode string

const (
	// ExecutionModeSequential processes one work item at a time, waiting for PR merge
	ExecutionModeSequential ExecutionMode = "sequential"
	// ExecutionModeParallel processes work items concurrently (legacy behavior)
	ExecutionModeParallel ExecutionMode = "parallel"
)

// WorkItemResult is returned by the work item handler with PR information
type WorkItemResult struct {
	Success    bool
	PRNumber   int    // PR ID if created
	PRURL      string // PR URL if created
	HeadSHA    string // Head commit SHA of the PR
	BranchName string // Head branch name (e.g. "pilot/GH-123")
	Error      error
}

// ProcessedStore persists which Azure DevOps work items have been processed across restarts.
type ProcessedStore interface {
	Mark(source, repo, issueID string) error
	Unmark(source, repo, issueID string) error
	IsProcessed(source, repo, issueID string) (bool, error)
	Load(source, repo string) (map[string]time.Time, error)
}

// Poller polls Azure DevOps for work items with a specific tag
type Poller struct {
	client     *Client
	tag        string
	interval   time.Duration
	processed  map[int]bool
	mu         sync.RWMutex
	onWorkItem func(ctx context.Context, wi *WorkItem) error
	// onWorkItemWithResult is called for sequential mode, returns PR info
	onWorkItemWithResult func(ctx context.Context, wi *WorkItem) (*WorkItemResult, error)
	// OnPRCreated is called when a PR is created after work item processing
	// Parameters: prID, prURL, workItemID, headSHA, branchName
	OnPRCreated func(prID int, prURL string, workItemID int, headSHA string, branchName string)
	logger      *slog.Logger

	// Work item filtering
	workItemTypes []string

	// Sequential mode configuration
	executionMode  ExecutionMode
	mergeWaiter    *MergeWaiter
	waitForMerge   bool
	prTimeout      time.Duration
	prPollInterval time.Duration

	// GH-1358: Persistent processed store (optional)
	processedStore ProcessedStore

	// GH-1358: Parallel execution configuration
	maxConcurrent int
	semaphore     chan struct{}
	activeWg      sync.WaitGroup
	stopping      atomic.Bool
	wgMu          sync.Mutex // protects stopping + activeWg Add/Wait coordination

	// pollerMetrics records per-repo dispatch/skip counters (TASK-293, GH-3064).
	pollerMetrics skipreason.PollerMetricsRecorder
	repoKey       string // label value for the `repo` dimension in poller metrics
}

// PollerOption configures a Poller
type PollerOption func(*Poller)

// WithPollerLogger sets the logger for the poller
func WithPollerLogger(logger *slog.Logger) PollerOption {
	return func(p *Poller) {
		p.logger = logger
	}
}

// WithOnWorkItem sets the callback for new work items (parallel mode)
func WithOnWorkItem(fn func(ctx context.Context, wi *WorkItem) error) PollerOption {
	return func(p *Poller) {
		p.onWorkItem = fn
	}
}

// WithOnWorkItemWithResult sets the callback for new work items that returns PR info (sequential mode)
func WithOnWorkItemWithResult(fn func(ctx context.Context, wi *WorkItem) (*WorkItemResult, error)) PollerOption {
	return func(p *Poller) {
		p.onWorkItemWithResult = fn
	}
}

// WithExecutionMode sets the execution mode (sequential or parallel)
func WithExecutionMode(mode ExecutionMode) PollerOption {
	return func(p *Poller) {
		p.executionMode = mode
	}
}

// WithSequentialConfig configures sequential execution settings
func WithSequentialConfig(waitForMerge bool, pollInterval, timeout time.Duration) PollerOption {
	return func(p *Poller) {
		p.waitForMerge = waitForMerge
		p.prPollInterval = pollInterval
		p.prTimeout = timeout
	}
}

// WithOnPRCreated sets the callback for PR creation events
func WithOnPRCreated(fn func(prID int, prURL string, workItemID int, headSHA string, branchName string)) PollerOption {
	return func(p *Poller) {
		p.OnPRCreated = fn
	}
}

// WithWorkItemTypes sets the work item types to filter
func WithWorkItemTypes(types []string) PollerOption {
	return func(p *Poller) {
		p.workItemTypes = types
	}
}

// WithProcessedStore sets the persistent store for processed work item tracking.
// GH-1358: On startup, processed work items are loaded from the store to prevent re-processing after hot upgrade.
func WithProcessedStore(store ProcessedStore) PollerOption {
	return func(p *Poller) {
		p.processedStore = store
	}
}

// WithMaxConcurrent sets the maximum number of parallel work item executions.
// GH-1358: Ported from GitHub poller parallel execution pattern.
func WithMaxConcurrent(n int) PollerOption {
	return func(p *Poller) {
		if n < 1 {
			n = 1
		}
		p.maxConcurrent = n
	}
}

// WithPollerMetrics sets the recorder for per-repo dispatch/skip counters (TASK-293).
func WithPollerMetrics(rec skipreason.PollerMetricsRecorder) PollerOption {
	return func(p *Poller) {
		p.pollerMetrics = rec
	}
}

// WithPollerRepoKey sets the `repo` label value used in poller metrics.
func WithPollerRepoKey(key string) PollerOption {
	return func(p *Poller) {
		p.repoKey = key
	}
}

// NewPoller creates a new Azure DevOps work item poller
func NewPoller(client *Client, tag string, interval time.Duration, opts ...PollerOption) *Poller {
	p := &Poller{
		client:         client,
		tag:            tag,
		interval:       interval,
		processed:      make(map[int]bool),
		logger:         logging.WithComponent("azuredevops-poller"),
		executionMode:  ExecutionModeParallel, // Default for backward compatibility
		waitForMerge:   true,
		prPollInterval: 30 * time.Second,
		prTimeout:      1 * time.Hour,
		workItemTypes:  []string{"Bug", "Task", "User Story"},
	}

	for _, opt := range opts {
		opt(p)
	}

	// GH-1358: Load processed work items from persistent store if available
	if p.processedStore != nil {
		loaded, err := p.processedStore.Load("azuredevops", p.repoKey)
		if err != nil {
			p.logger.Warn("Failed to load processed work items from store", slog.Any("error", err))
		} else if len(loaded) > 0 {
			p.mu.Lock()
			for idStr := range loaded {
				if id, parseErr := strconv.Atoi(idStr); parseErr == nil {
					p.processed[id] = true
				}
			}
			p.mu.Unlock()
			p.logger.Info("Loaded processed work items from store", slog.Int("count", len(loaded)))
		}
	}

	// GH-1358: Initialize parallel semaphore
	if p.maxConcurrent < 1 {
		p.maxConcurrent = 2 // default
	}
	p.semaphore = make(chan struct{}, p.maxConcurrent)

	// Create merge waiter if in sequential mode
	if p.executionMode == ExecutionModeSequential && p.waitForMerge {
		p.mergeWaiter = NewMergeWaiter(client, &MergeWaiterConfig{
			PollInterval: p.prPollInterval,
			Timeout:      p.prTimeout,
		})
	}

	return p
}

// Start begins polling for work items
func (p *Poller) Start(ctx context.Context) {
	p.logger.Info("Starting Azure DevOps poller",
		slog.String("tag", p.tag),
		slog.Duration("interval", p.interval),
		slog.String("mode", string(p.executionMode)),
		slog.Int("max_concurrent", p.maxConcurrent),
	)

	// GH-1355: Recover orphaned in-progress work items from previous run before starting poll loop
	p.recoverOrphanedWorkItems(ctx)

	if p.executionMode == ExecutionModeSequential {
		p.startSequential(ctx)
	} else {
		p.startParallel(ctx)
	}
}

package gitlab

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

// ExecutionMode determines how issues are processed
type ExecutionMode string

const (
	// ExecutionModeSequential processes one issue at a time, waiting for MR merge
	ExecutionModeSequential ExecutionMode = "sequential"
	// ExecutionModeParallel processes issues concurrently (legacy behavior)
	ExecutionModeParallel ExecutionMode = "parallel"
)

// IssueResult is returned by the issue handler with MR information
type IssueResult struct {
	Success    bool
	MRNumber   int    // MR IID if created
	MRURL      string // MR URL if created
	HeadSHA    string // Head commit SHA of the MR
	BranchName string // Head branch name (e.g. "pilot/GH-123")
	Error      error
}

// ProcessedStore persists which GitLab issues have been processed across restarts.
type ProcessedStore interface {
	Mark(source, repo, issueID string) error
	Unmark(source, repo, issueID string) error
	IsProcessed(source, repo, issueID string) (bool, error)
	Load(source, repo string) (map[string]time.Time, error)
}

// Poller polls GitLab for issues with a specific label
type Poller struct {
	client    *Client
	label     string
	interval  time.Duration
	processed map[int]bool
	mu        sync.RWMutex
	onIssue   func(ctx context.Context, issue *Issue) error
	// onIssueWithResult is called for sequential mode, returns MR info
	onIssueWithResult func(ctx context.Context, issue *Issue) (*IssueResult, error)
	// OnMRCreated is called when an MR is created after issue processing
	// Parameters: mrIID, mrURL, issueIID, headSHA, branchName
	OnMRCreated func(mrIID int, mrURL string, issueIID int, headSHA string, branchName string)
	logger      *slog.Logger

	// Sequential mode configuration
	executionMode  ExecutionMode
	mergeWaiter    *MergeWaiter
	waitForMerge   bool
	mrTimeout      time.Duration
	mrPollInterval time.Duration

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

// NewPoller creates a new GitLab issue poller
func NewPoller(client *Client, label string, interval time.Duration, opts ...PollerOption) *Poller {
	p := &Poller{
		client:         client,
		label:          label,
		interval:       interval,
		processed:      make(map[int]bool),
		logger:         logging.WithComponent("gitlab-poller"),
		executionMode:  ExecutionModeParallel, // Default for backward compatibility
		waitForMerge:   true,
		mrPollInterval: 30 * time.Second,
		mrTimeout:      1 * time.Hour,
	}

	for _, opt := range opts {
		opt(p)
	}

	// GH-1358: Load processed issues from persistent store if available
	if p.processedStore != nil {
		loaded, err := p.processedStore.Load("gitlab", p.repoKey)
		if err != nil {
			p.logger.Warn("Failed to load processed issues from store", slog.Any("error", err))
		} else if len(loaded) > 0 {
			p.mu.Lock()
			for idStr := range loaded {
				if id, parseErr := strconv.Atoi(idStr); parseErr == nil {
					p.processed[id] = true
				}
			}
			p.mu.Unlock()
			p.logger.Info("Loaded processed issues from store", slog.Int("count", len(loaded)))
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
			PollInterval: p.mrPollInterval,
			Timeout:      p.mrTimeout,
		})
	}

	return p
}

// Start begins polling for issues
func (p *Poller) Start(ctx context.Context) {
	p.logger.Info("Starting GitLab poller",
		slog.String("label", p.label),
		slog.Duration("interval", p.interval),
		slog.String("mode", string(p.executionMode)),
		slog.Int("max_concurrent", p.maxConcurrent),
	)

	// GH-1355: Recover orphaned in-progress issues from previous run before starting poll loop
	p.recoverOrphanedIssues(ctx)

	if p.executionMode == ExecutionModeSequential {
		p.startSequential(ctx)
	} else {
		p.startParallel(ctx)
	}
}

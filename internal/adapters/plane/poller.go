package plane

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ylcn91/pilot/internal/logging"
)

// Status labels for tracking work item progress.
const (
	LabelPilot      = "pilot"
	LabelInProgress = "pilot-in-progress"
	LabelDone       = "pilot-done"
	LabelFailed     = "pilot-failed"
)

// IssueResult is returned by the work item handler.
type IssueResult struct {
	Success    bool
	PRNumber   int
	PRURL      string
	HeadSHA    string // Head commit SHA of the PR (for autopilot wiring)
	BranchName string // Head branch name e.g. "pilot/PLANE-123"
	Error      error
}

// ProcessedStore persists which Plane work items have been processed across restarts.
type ProcessedStore interface {
	Mark(source, repo, issueID string) error
	Unmark(source, repo, issueID string) error
	IsProcessed(source, repo, issueID string) (bool, error)
	Load(source, repo string) (map[string]time.Time, error)
}

// Poller polls Plane.so for work items with the pilot label.
type Poller struct {
	client   *Client
	config   *Config
	interval time.Duration

	processed map[string]bool // Work item UUID → processed
	mu        sync.RWMutex

	onIssue     func(ctx context.Context, issue *WorkItem) (*IssueResult, error)
	onPRCreated func(prNumber int, prURL, issueID, headSHA, branchName string)
	logger      *slog.Logger

	// Label UUID cache (resolved on startup by name).
	// Plane labels are per-project, so UUIDs are keyed by project ID:
	// the same label name (e.g. "pilot") has a distinct UUID in each project.
	pilotLabelIDs      map[string]string
	inProgressLabelIDs map[string]string
	doneLabelIDs       map[string]string
	failedLabelIDs     map[string]string

	// GH-1830: Persistent processed store (optional)
	processedStore ProcessedStore

	// GH-1832: State UUID cache (resolved on startup by group)
	// Maps project ID → state UUID for started/completed groups.
	startedStateIDs   map[string]string
	completedStateIDs map[string]string

	// GH-1830: Parallel execution configuration
	maxConcurrent int
	semaphore     chan struct{}
	activeWg      sync.WaitGroup
	stopping      atomic.Bool
	wgMu          sync.Mutex // protects stopping + activeWg Add/Wait coordination
}

// PollerOption configures a Poller.
type PollerOption func(*Poller)

// WithOnIssue sets the callback for new work items.
func WithOnIssue(fn func(ctx context.Context, issue *WorkItem) (*IssueResult, error)) PollerOption {
	return func(p *Poller) {
		p.onIssue = fn
	}
}

// WithPollerLogger sets the logger for the poller.
func WithPollerLogger(logger *slog.Logger) PollerOption {
	return func(p *Poller) {
		p.logger = logger
	}
}

// WithProcessedStore sets the persistent store for processed work item tracking.
// GH-1830: On startup, processed items are loaded from the store to prevent re-processing after hot upgrade.
func WithProcessedStore(store ProcessedStore) PollerOption {
	return func(p *Poller) {
		p.processedStore = store
	}
}

// WithMaxConcurrent sets the maximum number of parallel work item executions.
// GH-1830: Ported parallel execution pattern from other adapters.
func WithMaxConcurrent(n int) PollerOption {
	return func(p *Poller) {
		if n < 1 {
			n = 1
		}
		p.maxConcurrent = n
	}
}

// WithOnPRCreated sets the callback for when a PR is created for a work item.
func WithOnPRCreated(fn func(prNumber int, prURL, issueID, headSHA, branchName string)) PollerOption {
	return func(p *Poller) {
		p.onPRCreated = fn
	}
}

// NewPoller creates a new Plane work item poller.
func NewPoller(client *Client, config *Config, interval time.Duration, opts ...PollerOption) *Poller {
	p := &Poller{
		client:    client,
		config:    config,
		interval:  interval,
		processed: make(map[string]bool),
		logger:    logging.WithComponent("plane-poller"),
	}

	for _, opt := range opts {
		opt(p)
	}

	// GH-1830: Load processed items from persistent store if available
	if p.processedStore != nil {
		loaded, err := p.processedStore.Load("plane", "")
		if err != nil {
			p.logger.Warn("Failed to load processed issues from store", slog.Any("error", err))
		} else if len(loaded) > 0 {
			p.mu.Lock()
			for id := range loaded {
				p.processed[id] = true
			}
			p.mu.Unlock()
			p.logger.Info("Loaded processed issues from store", slog.Int("count", len(loaded)))
		}
	}

	// GH-1830: Initialize parallel semaphore
	if p.maxConcurrent < 1 {
		p.maxConcurrent = 2 // default
	}
	p.semaphore = make(chan struct{}, p.maxConcurrent)

	return p
}

// Start begins polling for work items.
func (p *Poller) Start(ctx context.Context) error {
	// Cache label UUIDs on startup
	if err := p.cacheLabelIDs(ctx); err != nil {
		return fmt.Errorf("failed to cache label IDs: %w", err)
	}

	// GH-1832: Cache state UUIDs for state transitions
	p.cacheStateIDs(ctx)

	p.logger.Info("Starting Plane poller",
		slog.String("workspace", p.config.WorkspaceSlug),
		slog.Int("projects", len(p.config.ProjectIDs)),
		slog.Duration("interval", p.interval),
		slog.Int("max_concurrent", p.maxConcurrent),
	)

	// GH-1830: Recover orphaned in-progress items from previous run
	p.recoverOrphanedIssues(ctx)

	// Initial check
	p.checkForNewIssues(ctx)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("Plane poller stopping, waiting for active tasks...")
			p.wgMu.Lock()
			p.stopping.Store(true)
			p.wgMu.Unlock()
			p.activeWg.Wait()
			p.logger.Info("Plane poller stopped")
			return nil
		case <-ticker.C:
			p.checkForNewIssues(ctx)
		}
	}
}

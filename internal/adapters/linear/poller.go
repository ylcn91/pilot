package linear

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ylcn91/pilot/internal/adapters"
	"github.com/ylcn91/pilot/internal/logging"
)

// IssueResult is returned by the issue handler
type IssueResult struct {
	Success    bool
	PRNumber   int
	PRURL      string
	HeadSHA    string // Head commit SHA of the PR (GH-1361: for autopilot wiring)
	BranchName string // Head branch name e.g. "pilot/APP-123" (GH-1361: for autopilot wiring)
	Error      error
}

// ProcessedStore persists which Linear issues have been processed across
// restarts. It aliases the canonical adapters.ProcessedStore so a method
// addition there propagates to every adapter without per-package edits.
type ProcessedStore = adapters.ProcessedStore

// Poller polls Linear for issues with a specific label
type Poller struct {
	client    *Client
	config    *WorkspaceConfig
	interval  time.Duration
	processed map[string]bool // Linear uses string IDs
	mu        sync.RWMutex
	onIssue   func(ctx context.Context, issue *Issue) (*IssueResult, error)
	logger    *slog.Logger

	// Labels cache
	pilotLabelID      string
	inProgressLabelID string
	doneLabelID       string
	failedLabelID     string

	// GH-1351: Persistent processed store (optional)
	processedStore ProcessedStore

	// GH-1700: OnPRCreated is called when a PR is created after issue processing
	OnPRCreated func(prNumber int, prURL string, issueNumber int, headSHA string, branchName string, issueNodeID string)

	// GH-1357: Parallel execution configuration
	maxConcurrent int
	semaphore     chan struct{}
	activeWg      sync.WaitGroup
	stopping      atomic.Bool
	wgMu          sync.Mutex // protects stopping + activeWg Add/Wait coordination
}

// PollerOption configures a Poller
type PollerOption func(*Poller)

// WithOnLinearIssue sets the callback for new issues
func WithOnLinearIssue(fn func(ctx context.Context, issue *Issue) (*IssueResult, error)) PollerOption {
	return func(p *Poller) {
		p.onIssue = fn
	}
}

// WithOnPRCreated sets the callback for PR creation events.
// GH-1700: Mirrors the GitHub poller pattern for autopilot wiring.
func WithOnPRCreated(fn func(prNumber int, prURL string, issueNumber int, headSHA string, branchName string, issueNodeID string)) PollerOption {
	return func(p *Poller) {
		p.OnPRCreated = fn
	}
}

// WithPollerLogger sets the logger for the poller
func WithPollerLogger(logger *slog.Logger) PollerOption {
	return func(p *Poller) {
		p.logger = logger
	}
}

// WithProcessedStore sets the persistent store for processed issue tracking.
// GH-1351: On startup, processed issues are loaded from the store to prevent re-processing after hot upgrade.
func WithProcessedStore(store ProcessedStore) PollerOption {
	return func(p *Poller) {
		p.processedStore = store
	}
}

// WithMaxConcurrent sets the maximum number of parallel issue executions.
// GH-1357: Ported from GitHub poller parallel execution pattern.
func WithMaxConcurrent(n int) PollerOption {
	return func(p *Poller) {
		if n < 1 {
			n = 1
		}
		p.maxConcurrent = n
	}
}

// NewPoller creates a new Linear issue poller
func NewPoller(client *Client, config *WorkspaceConfig, interval time.Duration, opts ...PollerOption) *Poller {
	p := &Poller{
		client:    client,
		config:    config,
		interval:  interval,
		processed: make(map[string]bool),
		logger:    logging.WithComponent("linear-poller"),
	}

	for _, opt := range opts {
		opt(p)
	}

	// GH-1351: Load processed issues from persistent store if available
	if p.processedStore != nil {
		loaded, err := p.processedStore.Load("linear", "")
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

	// GH-1357: Initialize parallel semaphore
	if p.maxConcurrent < 1 {
		p.maxConcurrent = 2 // default
	}
	p.semaphore = make(chan struct{}, p.maxConcurrent)

	return p
}

// Start begins polling for issues
func (p *Poller) Start(ctx context.Context) error {
	// Cache label IDs on startup
	if err := p.cacheLabelIDs(ctx); err != nil {
		return fmt.Errorf("failed to cache label IDs: %w", err)
	}

	p.logger.Info("Starting Linear poller",
		slog.String("team", p.config.TeamID),
		slog.String("label", p.config.PilotLabel),
		slog.Duration("interval", p.interval),
		slog.Int("max_concurrent", p.maxConcurrent),
	)

	// GH-1355: Recover orphaned in-progress issues from previous run before starting poll loop
	p.recoverOrphanedIssues(ctx)

	// Initial check
	p.checkForNewIssues(ctx)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("Linear poller stopping, waiting for active tasks...")
			p.wgMu.Lock()
			p.stopping.Store(true)
			p.wgMu.Unlock()
			p.activeWg.Wait()
			p.logger.Info("Linear poller stopped")
			return nil
		case <-ticker.C:
			p.checkForNewIssues(ctx)
		}
	}
}

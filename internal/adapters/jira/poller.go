package jira

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ylcn91/pilot/internal/logging"
)

// Status labels for tracking issue progress
const (
	LabelInProgress = "pilot-in-progress"
	LabelDone       = "pilot-done"
	LabelFailed     = "pilot-failed"
)

// IssueResult is returned by the issue handler
type IssueResult struct {
	Success    bool
	PRNumber   int
	PRURL      string
	HeadSHA    string // Head commit SHA of the PR (GH-1398: for autopilot wiring)
	BranchName string // Head branch name e.g. "pilot/PROJ-123" (GH-1398: for autopilot wiring)
	Error      error
}

// ProcessedStore persists which Jira issues have been processed across restarts.
type ProcessedStore interface {
	Mark(source, repo, issueID string) error
	Unmark(source, repo, issueID string) error
	IsProcessed(source, repo, issueID string) (bool, error)
	Load(source, repo string) (map[string]time.Time, error)
}

// Poller polls Jira for issues with the pilot label
type Poller struct {
	client     *Client
	config     *Config
	interval   time.Duration
	processed  map[string]bool // Jira uses string IDs (issue keys like PROJ-123)
	mu         sync.RWMutex
	onIssue    func(ctx context.Context, issue *Issue) (*IssueResult, error)
	logger     *slog.Logger
	pilotLabel string

	// GH-1357: Persistent processed store (optional)
	processedStore ProcessedStore

	// GH-1357: Parallel execution configuration
	maxConcurrent int
	semaphore     chan struct{}
	activeWg      sync.WaitGroup
	stopping      atomic.Bool
	wgMu          sync.Mutex // protects stopping + activeWg Add/Wait coordination
}

// PollerOption configures a Poller
type PollerOption func(*Poller)

// WithOnJiraIssue sets the callback for new issues
func WithOnJiraIssue(fn func(ctx context.Context, issue *Issue) (*IssueResult, error)) PollerOption {
	return func(p *Poller) {
		p.onIssue = fn
	}
}

// WithJiraPollerLogger sets the logger for the poller
func WithJiraPollerLogger(logger *slog.Logger) PollerOption {
	return func(p *Poller) {
		p.logger = logger
	}
}

// WithProcessedStore sets the persistent store for processed issue tracking.
// GH-1357: On startup, processed issues are loaded from the store to prevent re-processing after hot upgrade.
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

// NewPoller creates a new Jira issue poller
func NewPoller(client *Client, config *Config, interval time.Duration, opts ...PollerOption) *Poller {
	pilotLabel := config.PilotLabel
	if pilotLabel == "" {
		pilotLabel = "pilot"
	}

	p := &Poller{
		client:     client,
		config:     config,
		interval:   interval,
		processed:  make(map[string]bool),
		logger:     logging.WithComponent("jira-poller"),
		pilotLabel: pilotLabel,
	}

	for _, opt := range opts {
		opt(p)
	}

	// GH-1357: Load processed issues from persistent store if available
	if p.processedStore != nil {
		loaded, err := p.processedStore.Load("jira", "")
		if err != nil {
			p.logger.Warn("Failed to load processed issues from store", slog.Any("error", err))
		} else if len(loaded) > 0 {
			p.mu.Lock()
			for key := range loaded {
				p.processed[key] = true
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

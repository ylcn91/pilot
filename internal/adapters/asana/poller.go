package asana

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

// Status tags for tracking task progress
const (
	TagInProgress = "pilot-in-progress"
	TagDone       = "pilot-done"
	TagFailed     = "pilot-failed"
)

// TaskResult is returned by the task handler. It aliases the canonical
// adapters.IssueResult so the result shape stays in sync across adapters.
type TaskResult = adapters.IssueResult

// ProcessedStore persists which Asana tasks have been processed across
// restarts. It aliases the canonical adapters.ProcessedStore so a method
// addition there propagates to every adapter without per-package edits.
type ProcessedStore = adapters.ProcessedStore

// Poller polls Asana for tasks with the pilot tag
type Poller struct {
	client    *Client
	config    *Config
	interval  time.Duration
	processed map[string]bool // Asana uses string GIDs
	mu        sync.RWMutex
	onTask    func(ctx context.Context, task *Task) (*TaskResult, error)
	logger    *slog.Logger

	// Tag GID cache
	pilotTagGID      string
	inProgressTagGID string
	doneTagGID       string
	failedTagGID     string

	// GH-1359: Persistent processed store (optional)
	processedStore ProcessedStore

	// GH-1359: Parallel execution configuration
	maxConcurrent int
	semaphore     chan struct{}
	activeWg      sync.WaitGroup
	stopping      atomic.Bool
	wgMu          sync.Mutex // protects stopping + activeWg Add/Wait coordination
}

// PollerOption configures a Poller
type PollerOption func(*Poller)

// WithOnAsanaTask sets the callback for new tasks
func WithOnAsanaTask(fn func(ctx context.Context, task *Task) (*TaskResult, error)) PollerOption {
	return func(p *Poller) {
		p.onTask = fn
	}
}

// WithAsanaPollerLogger sets the logger for the poller
func WithAsanaPollerLogger(logger *slog.Logger) PollerOption {
	return func(p *Poller) {
		p.logger = logger
	}
}

// WithProcessedStore sets the persistent store for processed task tracking.
// GH-1359: On startup, processed tasks are loaded from the store to prevent re-processing after hot upgrade.
func WithProcessedStore(store ProcessedStore) PollerOption {
	return func(p *Poller) {
		p.processedStore = store
	}
}

// WithMaxConcurrent sets the maximum number of parallel task executions.
// GH-1359: Ported parallel execution pattern from other adapters.
func WithMaxConcurrent(n int) PollerOption {
	return func(p *Poller) {
		if n < 1 {
			n = 1
		}
		p.maxConcurrent = n
	}
}

// NewPoller creates a new Asana task poller
func NewPoller(client *Client, config *Config, interval time.Duration, opts ...PollerOption) *Poller {
	p := &Poller{
		client:    client,
		config:    config,
		interval:  interval,
		processed: make(map[string]bool),
		logger:    logging.WithComponent("asana-poller"),
	}

	for _, opt := range opts {
		opt(p)
	}

	// GH-1359: Load processed tasks from persistent store if available
	if p.processedStore != nil {
		loaded, err := p.processedStore.Load("asana", "")
		if err != nil {
			p.logger.Warn("Failed to load processed tasks from store", slog.Any("error", err))
		} else if len(loaded) > 0 {
			p.mu.Lock()
			for gid := range loaded {
				p.processed[gid] = true
			}
			p.mu.Unlock()
			p.logger.Info("Loaded processed tasks from store", slog.Int("count", len(loaded)))
		}
	}

	// GH-1359: Initialize parallel semaphore
	if p.maxConcurrent < 1 {
		p.maxConcurrent = 2 // default
	}
	p.semaphore = make(chan struct{}, p.maxConcurrent)

	return p
}

// Start begins polling for tasks
func (p *Poller) Start(ctx context.Context) error {
	// Cache tag GIDs on startup
	if err := p.cacheTagGIDs(ctx); err != nil {
		return fmt.Errorf("failed to cache tag GIDs: %w", err)
	}

	p.logger.Info("Starting Asana poller",
		slog.String("workspace", p.client.workspaceID),
		slog.String("tag", p.config.PilotTag),
		slog.Duration("interval", p.interval),
		slog.Int("max_concurrent", p.maxConcurrent),
	)

	// GH-1355: Recover orphaned in-progress tasks from previous run before starting poll loop
	p.recoverOrphanedTasks(ctx)

	// Initial check
	p.checkForNewTasks(ctx)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("Asana poller stopping, waiting for active tasks...")
			p.wgMu.Lock()
			p.stopping.Store(true)
			p.wgMu.Unlock()
			p.activeWg.Wait()
			p.logger.Info("Asana poller stopped")
			return nil
		case <-ticker.C:
			p.checkForNewTasks(ctx)
		}
	}
}

// cacheTagGIDs fetches and caches the GIDs for pilot-related tags
func (p *Poller) cacheTagGIDs(ctx context.Context) error {
	pilotTag := p.config.PilotTag
	if pilotTag == "" {
		pilotTag = "pilot"
	}

	// Find or create pilot tag
	tag, err := p.client.FindTagByName(ctx, pilotTag)
	if err != nil {
		return fmt.Errorf("failed to find pilot tag: %w", err)
	}
	if tag == nil {
		return fmt.Errorf("pilot tag %q not found in workspace", pilotTag)
	}
	p.pilotTagGID = tag.GID

	// Find status tags (optional - don't fail if not found)
	if tag, _ := p.client.FindTagByName(ctx, TagInProgress); tag != nil {
		p.inProgressTagGID = tag.GID
	}
	if tag, _ := p.client.FindTagByName(ctx, TagDone); tag != nil {
		p.doneTagGID = tag.GID
	}
	if tag, _ := p.client.FindTagByName(ctx, TagFailed); tag != nil {
		p.failedTagGID = tag.GID
	}

	p.logger.Debug("Cached tag GIDs",
		slog.String("pilot", p.pilotTagGID),
		slog.String("in_progress", p.inProgressTagGID),
		slog.String("done", p.doneTagGID),
		slog.String("failed", p.failedTagGID),
	)

	return nil
}

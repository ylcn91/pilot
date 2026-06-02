package github

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ylcn91/pilot/internal/logging"
	"github.com/ylcn91/pilot/internal/memory"
)

// Cleaner handles automatic cleanup of stale pilot labels (pilot-in-progress and pilot-failed).
// When Pilot crashes or is killed, labels remain on issues. This cleaner
// periodically checks for such orphaned labels and removes them.
type Cleaner struct {
	client          *Client
	store           *memory.Store
	owner           string
	repo            string
	interval        time.Duration
	threshold       time.Duration
	failedThreshold time.Duration
	logger          *slog.Logger

	// OnFailedCleaned is called when a pilot-failed label is removed.
	// Used to clear the issue from the poller's processed map.
	OnFailedCleaned func(issueNumber int)

	// OnInProgressCleaned is called when a pilot-in-progress label is removed
	// from a closed issue. Used to prune the dashboard monitor so the task
	// stops appearing in the queue view (GH-2354).
	OnInProgressCleaned func(issueNumber int)

	// OnBlockedCleaned is called when a pilot-blocked label is removed.
	// Used to clear the issue from the poller's processed map so it can be
	// re-dispatched after a human resolves the blocking condition (GH-2402).
	OnBlockedCleaned func(issueNumber int)

	// OnStartupRecovered is called for each issue that has its pilot-in-progress
	// label stripped during daemon startup recovery (GH-2589). Used to clear the
	// issue from the poller's persistent processed store so it can be re-dispatched.
	OnStartupRecovered func(issueNumber int)

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
}

// CleanerOption configures a Cleaner
type CleanerOption func(*Cleaner)

// WithCleanerLogger sets the logger for the cleaner
func WithCleanerLogger(logger *slog.Logger) CleanerOption {
	return func(c *Cleaner) {
		c.logger = logger
	}
}

// WithOnFailedCleaned sets the callback for when a pilot-failed label is removed.
// The callback receives the issue number and should clear it from the poller's processed map.
func WithOnFailedCleaned(fn func(issueNumber int)) CleanerOption {
	return func(c *Cleaner) {
		c.OnFailedCleaned = fn
	}
}

// WithOnInProgressCleaned sets the callback for when a pilot-in-progress label
// is removed from a closed issue. The callback receives the issue number and
// should remove the task from the dashboard monitor (GH-2354).
func WithOnInProgressCleaned(fn func(issueNumber int)) CleanerOption {
	return func(c *Cleaner) {
		c.OnInProgressCleaned = fn
	}
}

// WithOnBlockedCleaned sets the callback for when a pilot-blocked label is
// detected as removed. The callback receives the issue number and should
// clear it from the poller's processed map so the next poll can re-dispatch.
// GH-2402.
func WithOnBlockedCleaned(fn func(issueNumber int)) CleanerOption {
	return func(c *Cleaner) {
		c.OnBlockedCleaned = fn
	}
}

// WithOnStartupRecovered sets the callback invoked for each issue that has its
// pilot-in-progress label stripped by StartupRecover (GH-2589). The callback
// receives the issue number and should clear it from the poller's persistent
// processed store so the issue can be re-dispatched on the next poll cycle.
func WithOnStartupRecovered(fn func(issueNumber int)) CleanerOption {
	return func(c *Cleaner) {
		c.OnStartupRecovered = fn
	}
}

// NewCleaner creates a new stale label cleaner.
// The repo parameter should be in "owner/repo" format.
func NewCleaner(client *Client, store *memory.Store, repo string, config *StaleLabelCleanupConfig, opts ...CleanerOption) (*Cleaner, error) {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repo format, expected owner/repo: %s", repo)
	}

	interval := config.Interval
	if interval == 0 {
		interval = 30 * time.Minute
	}

	threshold := config.Threshold
	if threshold == 0 {
		threshold = 1 * time.Hour
	}

	failedThreshold := config.FailedThreshold
	if failedThreshold == 0 {
		failedThreshold = 24 * time.Hour
	}

	c := &Cleaner{
		client:          client,
		store:           store,
		owner:           parts[0],
		repo:            parts[1],
		interval:        interval,
		threshold:       threshold,
		failedThreshold: failedThreshold,
		logger:          logging.WithComponent("github-cleanup"),
		stopCh:          make(chan struct{}),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c, nil
}

// Start begins the periodic cleanup loop.
// It runs in the background and can be stopped with Stop().
func (c *Cleaner) Start(ctx context.Context) {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.running = true
	c.stopCh = make(chan struct{})
	c.mu.Unlock()

	c.logger.Info("Starting stale label cleaner",
		slog.String("repo", c.owner+"/"+c.repo),
		slog.Duration("interval", c.interval),
		slog.Duration("in_progress_threshold", c.threshold),
		slog.Duration("failed_threshold", c.failedThreshold),
	)

	// Run initial cleanup
	if err := c.Cleanup(ctx); err != nil {
		c.logger.Warn("Initial cleanup failed", slog.Any("error", err))
	}

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("Stale label cleaner stopped (context cancelled)")
			return
		case <-c.stopCh:
			c.logger.Info("Stale label cleaner stopped")
			return
		case <-ticker.C:
			if err := c.Cleanup(ctx); err != nil {
				c.logger.Warn("Cleanup failed", slog.Any("error", err))
			}
		}
	}
}

// Stop stops the periodic cleanup loop
func (c *Cleaner) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running {
		close(c.stopCh)
		c.running = false
	}
}

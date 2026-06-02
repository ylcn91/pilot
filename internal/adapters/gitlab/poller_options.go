package gitlab

import (
	"context"
	"log/slog"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/skipreason"
)

// PollerOption configures a Poller
type PollerOption func(*Poller)

// WithPollerLogger sets the logger for the poller
func WithPollerLogger(logger *slog.Logger) PollerOption {
	return func(p *Poller) {
		p.logger = logger
	}
}

// WithOnIssue sets the callback for new issues (parallel mode)
func WithOnIssue(fn func(ctx context.Context, issue *Issue) error) PollerOption {
	return func(p *Poller) {
		p.onIssue = fn
	}
}

// WithOnIssueWithResult sets the callback for new issues that returns MR info (sequential mode)
func WithOnIssueWithResult(fn func(ctx context.Context, issue *Issue) (*IssueResult, error)) PollerOption {
	return func(p *Poller) {
		p.onIssueWithResult = fn
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
		p.mrPollInterval = pollInterval
		p.mrTimeout = timeout
	}
}

// WithOnMRCreated sets the callback for MR creation events
func WithOnMRCreated(fn func(mrIID int, mrURL string, issueIID int, headSHA string, branchName string)) PollerOption {
	return func(p *Poller) {
		p.OnMRCreated = fn
	}
}

// WithProcessedStore sets the persistent store for processed issue tracking.
// GH-1358: On startup, processed issues are loaded from the store to prevent re-processing after hot upgrade.
func WithProcessedStore(store ProcessedStore) PollerOption {
	return func(p *Poller) {
		p.processedStore = store
	}
}

// WithMaxConcurrent sets the maximum number of parallel issue executions.
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

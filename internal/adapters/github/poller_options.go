package github

import (
	"context"
	"log/slog"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/skipreason"
	"github.com/ylcn91/pilot/internal/executor"
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

// WithOnIssueWithResult sets the callback for new issues that returns PR info (sequential mode)
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
		p.prPollInterval = pollInterval
		p.prTimeout = timeout
	}
}

// WithOnPRCreated sets the callback for PR creation events.
// The callback receives prNumber, prURL, issueNumber, headSHA, branchName, issueNodeID.
// The callback is invoked after a PR is successfully created for an issue
func WithOnPRCreated(fn func(prNumber int, prURL string, issueNumber int, headSHA string, branchName string, issueNodeID string)) PollerOption {
	return func(p *Poller) {
		p.OnPRCreated = fn
	}
}

// WithScheduler sets the rate limit retry scheduler
func WithScheduler(s *executor.Scheduler) PollerOption {
	return func(p *Poller) {
		p.scheduler = s
	}
}

// WithProcessedStore sets the persistent store for processed issue tracking.
// On startup, processed issues are loaded from the store to prevent re-processing.
func WithProcessedStore(store ProcessedStore) PollerOption {
	return func(p *Poller) {
		p.processedStore = store
	}
}

// WithRetryGracePeriod sets the minimum time that must elapse after an issue is
// marked processed before the retry path will allow re-dispatch. Default: 5 minutes.
func WithRetryGracePeriod(d time.Duration) PollerOption {
	return func(p *Poller) {
		p.retryGracePeriod = d
	}
}

// WithTaskChecker sets the task checker used to verify whether an issue is still
// queued or in-progress before allowing retry after the grace period expires.
func WithTaskChecker(tc TaskChecker) PollerOption {
	return func(p *Poller) {
		p.taskChecker = tc
	}
}

// WithExecutionChecker sets the execution checker used to prevent re-dispatch
// of tasks that already have a completed execution in the database (GH-2242).
func WithExecutionChecker(ec ExecutionChecker, projectPath string) PollerOption {
	return func(p *Poller) {
		p.execChecker = ec
		p.projectPath = projectPath
	}
}

// WithMaxFailedRetries sets the maximum number of auto-retries for issues
// that are stuck with pilot-failed label from execution failures. Default: 3.
func WithMaxFailedRetries(n int) PollerOption {
	return func(p *Poller) {
		if n < 0 {
			n = 0
		}
		p.maxFailedRetries = n
	}
}

// WithMaxRetryReadyRetries sets the maximum number of auto-retries for issues
// with pilot-retry-ready label (PR closed without merge). Default: 3.
func WithMaxRetryReadyRetries(n int) PollerOption {
	return func(p *Poller) {
		if n < 0 {
			n = 0
		}
		p.maxRetryReadyRetries = n
	}
}

// WithMaxConcurrent sets the maximum number of parallel issue executions
func WithMaxConcurrent(n int) PollerOption {
	return func(p *Poller) {
		if n < 1 {
			n = 1
		}
		p.maxConcurrent = n
	}
}

// WithPreFlightJudge sets the pre-flight issue quality judge (GH-2802).
// Pass nil to disable (same as not calling this option).
func WithPreFlightJudge(judge PreFlightJudger) PollerOption {
	return func(p *Poller) {
		p.preFlightJudge = judge
	}
}

// WithExecutionSaver sets the store used to persist pre-flight rejection records.
func WithExecutionSaver(saver ExecutionSaver) PollerOption {
	return func(p *Poller) {
		p.execSaver = saver
	}
}

// WithIssueMetricsRecorder sets the recorder for issue processing outcomes.
// Pass nil to disable (same as not calling this option).
func WithIssueMetricsRecorder(rec IssueMetricsRecorder) PollerOption {
	return func(p *Poller) {
		p.metricsRecorder = rec
	}
}

// WithPollerMetrics sets the recorder for per-repo dispatch/skip counters (TASK-293).
func WithPollerMetrics(rec skipreason.PollerMetricsRecorder) PollerOption {
	return func(p *Poller) {
		p.pollerMetrics = rec
	}
}

// WithProjectBoardSource configures the poller to source candidates from a Projects V2
// board column instead of by label. When set, FindIssuesFromProject replaces ListIssues
// as the candidate fetch in findOldestUnprocessedIssue; all downstream filters are unchanged.
func WithProjectBoardSource(src *ProjectBoardSource) PollerOption {
	return func(p *Poller) {
		p.projectBoardSource = src
	}
}

// WithBoardSync configures the poller to move the issue card to inProgressStatus on the
// Projects V2 board after confirmed dispatch. No-op when bs is nil or inProgressStatus is "".
func WithBoardSync(bs *ProjectBoardSync, inProgressStatus string) PollerOption {
	return func(p *Poller) {
		p.boardSync = bs
		p.inProgressStatus = inProgressStatus
	}
}

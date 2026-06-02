package autopilot

import (
	"context"
	"time"
)

// Run starts the autopilot processing loop.
// It continuously processes all active PRs until context is cancelled.
func (c *Controller) Run(ctx context.Context) error {
	c.log.Info("autopilot controller started",
		"env", c.config.EnvironmentName(),
		"poll_interval", c.config.CIPollInterval,
		"ci_timeout", c.config.CIWaitTimeout,
		"auto_merge", c.config.AutoMerge,
		"release_enabled", c.resolvedRelease() != nil && c.resolvedRelease().Enabled,
	)

	// Dynamic poll interval settings
	basePollInterval := c.config.CIPollInterval
	fastPollInterval := 10 * time.Second
	idlePollInterval := 60 * time.Second
	currentInterval := basePollInterval

	// GH-3113: Periodic reconciliation loop — registers orphan PRs that OnPRCreated missed.
	go c.startReconciler(ctx)

	// GH-2251: Periodic scan for externally-merged PRs.
	// Use half the scan window as the interval so merges are detected well within the window.
	mergedScanInterval := c.config.MergedPRScanWindow / 2
	if mergedScanInterval < 5*time.Minute {
		mergedScanInterval = 5 * time.Minute
	}
	mergedScanTicker := time.NewTicker(mergedScanInterval)
	defer mergedScanTicker.Stop()

	ticker := time.NewTicker(currentInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.log.Info("autopilot controller stopping")
			return ctx.Err()
		case <-mergedScanTicker.C:
			// GH-2251: Periodically scan for externally-merged PRs that
			// were never tracked by autopilot (e.g. merged via gh pr merge).
			if err := c.ScanRecentlyMergedPRs(ctx); err != nil {
				c.log.Warn("periodic merged PR scan failed", "error", err)
			}
		case <-ticker.C:
			c.processAllPRs(ctx)

			// Adjust interval based on active PR states
			newInterval := idlePollInterval
			activePRs := c.GetActivePRs()
			for _, pr := range activePRs {
				if pr.Stage == StageWaitingCI || pr.Stage == StagePRCreated {
					newInterval = fastPollInterval
					break
				}
			}

			// Update ticker interval if it changed
			if newInterval != currentInterval {
				c.log.Debug("adjusting poll interval",
					"old_interval", currentInterval,
					"new_interval", newInterval,
					"active_prs", len(activePRs),
				)
				ticker.Reset(newInterval)
				currentInterval = newInterval
			}
		}
	}
}

// processAllPRs processes all active PRs in one iteration.
func (c *Controller) processAllPRs(ctx context.Context) {
	prs := c.GetActivePRs()

	// Update active PR gauges every tick
	c.metrics.UpdateActivePRs(prs)

	if len(prs) == 0 {
		return
	}

	c.log.Info("processing active PRs", "count", len(prs))

	for _, snap := range prs {
		select {
		case <-ctx.Done():
			return
		default:
			c.log.Debug("checking PR",
				"pr", snap.PRNumber,
				"stage", snap.Stage,
				"ci_status", snap.CIStatus,
			)

			// TASK-324: `snap` is a detached snapshot from GetActivePRs. Re-fetch the
			// LIVE pointer by number so the pre-ProcessPR mutations below (and
			// checkExternalMergeOrClose) operate on the shared state under its mutex.
			c.mu.RLock()
			pr, ok := c.activePRs[snap.PRNumber]
			c.mu.RUnlock()
			if !ok {
				// PR was removed between snapshot and now — skip.
				continue
			}

			// Fetch PR once, use twice - cache to avoid redundant API calls
			ghPR, err := c.ghClient.GetPullRequest(ctx, c.owner, c.repo, pr.PRNumber)
			if err != nil {
				c.log.Warn("failed to fetch PR", "pr", pr.PRNumber, "error", err)
				continue
			}

			// TASK-324: hold pr.mu around the external-merge/close check and the
			// polling-mode changes-requested read-modify-write + persist. Release it
			// BEFORE calling ProcessPR, which re-acquires pr.mu for its whole body
			// (Go's sync.Mutex is non-reentrant). Lock ordering preserved: pr.mu is
			// taken before any c.mu that checkExternalMergeOrClose→removePR acquires.
			pr.mu.Lock()
			externallyResolved := c.checkExternalMergeOrClose(ctx, pr, ghPR)
			if externallyResolved {
				pr.mu.Unlock()
				continue
			}

			// Detect changes_requested reviews in polling mode (webhook mode uses OnReviewRequested).
			// Only check PRs that haven't already been transitioned to review_requested.
			if pr.Stage != StageReviewRequested && pr.Stage != StageFailed &&
				c.config.ReviewFeedback != nil && c.config.ReviewFeedback.Enabled {
				if c.hasChangesRequested(ctx, pr) {
					c.log.Info("detected changes_requested review in polling mode",
						"pr", pr.PRNumber,
						"stage", pr.Stage,
					)
					pr.Stage = StageReviewRequested
					c.persistPRState(pr)
				}
			}
			pr.mu.Unlock()

			if err := c.ProcessPR(ctx, pr.PRNumber, ghPR); err != nil {
				// Error already logged in ProcessPR
				continue
			}
		}
	}
}

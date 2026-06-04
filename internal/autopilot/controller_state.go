package autopilot

import (
	"context"
	"strings"
	"time"

	"github.com/ylcn91/pilot/internal/logging"
)

// removePR removes PR from tracking and cleans up the remote branch.
func (c *Controller) removePR(prNumber int) {
	c.mu.Lock()
	prState, ok := c.activePRs[prNumber]
	var branchName string
	if ok {
		branchName = prState.BranchName
		// GH-862: Clean up discovery state for this PR's SHA
		if prState.HeadSHA != "" {
			c.ciMonitor.ClearDiscovery(prState.HeadSHA)
		}
		delete(c.activePRs, prNumber)
	}
	delete(c.prFailures, prNumber)
	c.mu.Unlock()

	// Clean up remote branch for closed/failed PRs (merged PRs already handled in handleMerging)
	if branchName != "" && c.ghClient != nil {
		if err := c.ghClient.DeleteBranch(context.Background(), c.owner, c.repo, branchName); err != nil {
			c.log.Debug("branch cleanup on PR removal", "branch", branchName, "pr", prNumber, "error", err)
		} else {
			c.log.Info("deleted branch on PR removal", "branch", branchName, "pr", prNumber)
		}
	}

	c.persistRemovePR(prNumber)
	c.removePRFailures(prNumber)
	c.log.Info("PR removed from tracking", "pr", prNumber)
}

// GetActivePRs returns detached snapshots of all tracked PRs.
//
// TASK-324: each returned *PRState is a field-by-field copy taken under that PR's
// own mu (via snapshot()), so every read-only consumer (metrics.UpdateActivePRs,
// metrics_alerter, dashboard/tui, gateway/server, cmd/pilot/adapters) is race-free
// for free and can never observe a torn write. The returned pointers are NOT the
// live map entries; callers that must mutate state (e.g. processAllPRs) re-fetch the
// live pointer by PRNumber under c.mu and take that pr.mu themselves.
//
// Lock ordering: we collect the live pointers under c.mu.RLock, RELEASE c.mu, then
// take each pr.mu to snapshot. This preserves the no-deadlock invariant (never hold
// c.mu while acquiring a prState.mu).
func (c *Controller) GetActivePRs() []*PRState {
	c.mu.RLock()
	live := make([]*PRState, 0, len(c.activePRs))
	for _, pr := range c.activePRs {
		live = append(live, pr)
	}
	c.mu.RUnlock()

	prs := make([]*PRState, 0, len(live))
	for _, pr := range live {
		pr.mu.Lock()
		prs = append(prs, pr.snapshot())
		pr.mu.Unlock()
	}
	return prs
}

// GetPRState returns the state of a specific PR.
func (c *Controller) GetPRState(prNumber int) (*PRState, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	pr, ok := c.activePRs[prNumber]
	return pr, ok
}

// isPRCircuitOpen checks if the per-PR circuit breaker is open.
// A PR's circuit breaker opens when it has >= MaxFailures consecutive failures.
// The counter auto-resets after FailureResetTimeout since the last failure.
func (c *Controller) isPRCircuitOpen(prNumber int) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	state, ok := c.prFailures[prNumber]
	if !ok {
		return false
	}

	// Auto-reset after timeout
	resetTimeout := c.config.FailureResetTimeout
	if resetTimeout == 0 {
		resetTimeout = 30 * time.Minute // Default fallback
	}
	if time.Since(state.LastFailureTime) > resetTimeout {
		return false
	}

	return state.FailureCount >= c.config.MaxFailures
}

// recordPRFailure increments the failure counter for a specific PR.
func (c *Controller) recordPRFailure(prNumber int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	state, ok := c.prFailures[prNumber]
	if !ok {
		state = &prFailureState{}
		c.prFailures[prNumber] = state
	}

	// Check if we should reset due to timeout before incrementing
	resetTimeout := c.config.FailureResetTimeout
	if resetTimeout == 0 {
		resetTimeout = 30 * time.Minute
	}
	if !state.LastFailureTime.IsZero() && time.Since(state.LastFailureTime) > resetTimeout {
		state.FailureCount = 0
	}

	state.FailureCount++
	state.LastFailureTime = time.Now()

	c.log.Debug("recorded PR failure",
		"pr", prNumber,
		"failures", state.FailureCount,
		"max", c.config.MaxFailures,
	)

	// Persist outside lock
	logging.SafeGo("autopilot.persistPRFailures", func() {
		c.persistPRFailures(prNumber, state)
	})
}

// resetPRFailures clears the failure counter for a specific PR after success.
func (c *Controller) resetPRFailures(prNumber int) {
	c.mu.Lock()
	state, hadFailures := c.prFailures[prNumber]
	if hadFailures && state.FailureCount > 0 {
		delete(c.prFailures, prNumber)
	}
	c.mu.Unlock()

	if hadFailures && state.FailureCount > 0 {
		c.log.Debug("reset PR failure counter after success", "pr", prNumber)
		c.removePRFailures(prNumber)
	}
}

// ResetCircuitBreaker resets the failure counter for all PRs.
// Call this after manual intervention or system recovery.
func (c *Controller) ResetCircuitBreaker() {
	c.mu.Lock()
	prNumbers := make([]int, 0, len(c.prFailures))
	for prNum := range c.prFailures {
		prNumbers = append(prNumbers, prNum)
	}
	c.prFailures = make(map[int]*prFailureState)
	c.mu.Unlock()

	// Persist removal of all failures
	for _, prNum := range prNumbers {
		c.removePRFailures(prNum)
	}
	c.log.Info("circuit breaker reset for all PRs", "count", len(prNumbers))
}

// ResetPRCircuitBreaker resets the failure counter for a specific PR.
// Use this when manually recovering a single PR.
func (c *Controller) ResetPRCircuitBreaker(prNumber int) {
	c.mu.Lock()
	_, hadFailures := c.prFailures[prNumber]
	delete(c.prFailures, prNumber)
	c.mu.Unlock()

	if hadFailures {
		c.removePRFailures(prNumber)
		c.log.Info("circuit breaker reset for PR", "pr", prNumber)
	}
}

// IsCircuitOpen returns true if any PR has an open circuit breaker.
// For per-PR tracking, this checks if any PR is blocked.
func (c *Controller) IsCircuitOpen() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	resetTimeout := c.config.FailureResetTimeout
	if resetTimeout == 0 {
		resetTimeout = 30 * time.Minute
	}

	for _, state := range c.prFailures {
		// Skip if timeout has passed
		if time.Since(state.LastFailureTime) > resetTimeout {
			continue
		}
		if state.FailureCount >= c.config.MaxFailures {
			return true
		}
	}
	return false
}

// IsPRCircuitOpen returns true if a specific PR's circuit breaker is open.
func (c *Controller) IsPRCircuitOpen(prNumber int) bool {
	return c.isPRCircuitOpen(prNumber)
}

// Config returns the autopilot configuration.
func (c *Controller) Config() *Config {
	return c.config
}

// GetPRFailures returns the current failure count for a specific PR.
func (c *Controller) GetPRFailures(prNumber int) int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	state, ok := c.prFailures[prNumber]
	if !ok {
		return 0
	}
	return state.FailureCount
}

// TotalFailures returns the sum of all active per-PR failure counts.
// Used for dashboard display. Only counts failures within the reset timeout.
func (c *Controller) TotalFailures() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	resetTimeout := c.config.FailureResetTimeout
	if resetTimeout == 0 {
		resetTimeout = 30 * time.Minute
	}

	total := 0
	for _, state := range c.prFailures {
		// Skip expired failures
		if time.Since(state.LastFailureTime) > resetTimeout {
			continue
		}
		total += state.FailureCount
	}
	return total
}

// Metrics returns the autopilot metrics collector.
func (c *Controller) Metrics() *Metrics {
	return c.metrics
}

// recordMergeSuccess fires the three merge-success metrics counters exactly
// once per PR number per daemon lifetime. Safe to call from any path that
// observes a Pilot PR transitioning to merged (handleMerging for
// autopilot-driven merges, ScanRecentlyMergedPRs for externally-merged PRs).
// Skips the time-to-merge histogram if prState.CreatedAt is zero (defensive).
func (c *Controller) recordMergeSuccess(prState *PRState) {
	c.mu.Lock()
	if c.recordedMerges[prState.PRNumber] {
		c.mu.Unlock()
		return
	}
	c.recordedMerges[prState.PRNumber] = true
	c.mu.Unlock()

	c.metrics.RecordPRMerged()
	c.metrics.RecordIssueProcessed("success")
	if !prState.CreatedAt.IsZero() {
		c.metrics.RecordPRTimeToMerge(time.Since(prState.CreatedAt))
	}
}

// reinforceMergedPatterns credits the merged PR's project patterns with a
// success outcome (A4). It runs at most once per PR number per daemon lifetime,
// guarded independently of recordMergeSuccess so the early metrics flag doesn't
// suppress it. No-op when learning is disabled.
func (c *Controller) reinforceMergedPatterns(prState *PRState) {
	if c.learningLoop == nil {
		return
	}

	c.mu.Lock()
	if c.recordedReinforcements[prState.PRNumber] {
		c.mu.Unlock()
		return
	}
	c.recordedReinforcements[prState.PRNumber] = true
	c.mu.Unlock()

	project := c.owner + "/" + c.repo
	taskType := inferTaskTypeFromTitle(prState.PRTitle)
	if err := c.learningLoop.RecordMergeOutcome(project, taskType, ""); err != nil {
		c.log.Warn("failed to reinforce patterns on merge",
			"pr", prState.PRNumber, "project", project, "error", err)
	}
}

// inferTaskTypeFromTitle derives a conventional-commit task type from a PR/issue
// title. Mirrors the executor's inferTaskType keyword logic without importing
// the executor package. Defaults to "feat".
func inferTaskTypeFromTitle(title string) string {
	t := strings.ToLower(title)
	for _, prefix := range []string{"feat", "fix", "refactor", "test", "docs", "chore"} {
		if strings.HasPrefix(t, prefix) {
			return prefix
		}
	}
	return "feat"
}

// GetLastProgressAt returns the timestamp of the last PR state transition.
// Used by MetricsAlerter for deadlock detection (GH-849).
func (c *Controller) GetLastProgressAt() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastProgressAt
}

// IsDeadlockAlertSent returns whether a deadlock alert has been sent since the last progress.
// Used by MetricsAlerter to avoid alert spam (GH-849).
func (c *Controller) IsDeadlockAlertSent() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.deadlockAlertSent
}

// MarkDeadlockAlertSent marks that a deadlock alert has been sent.
// Called by MetricsAlerter after firing a deadlock alert (GH-849).
func (c *Controller) MarkDeadlockAlertSent() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deadlockAlertSent = true
}

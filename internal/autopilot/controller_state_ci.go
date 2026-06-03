package autopilot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
)

// handlePRCreated starts CI monitoring for all environments.
// Also checks for merge conflicts immediately (race condition with concurrent merges).
// Accepts optional cached ghPR to avoid redundant API calls.
func (c *Controller) handlePRCreated(ctx context.Context, prState *PRState, ghPR *github.PullRequest) error {
	c.log.Debug("handlePRCreated: starting CI monitoring",
		"pr", prState.PRNumber,
		"sha", ShortSHA(prState.HeadSHA),
	)

	// GH-724: Check for merge conflicts immediately after PR creation.
	// Concurrent merges can make a PR conflicting before CI even starts.
	// Use cached ghPR if provided to avoid redundant API call.
	if ghPR != nil {
		if c.isMergeConflict(ghPR) {
			return c.handleMergeConflict(ctx, prState)
		}
	} else {
		// Fallback: fetch PR if not provided (for backward compatibility)
		fetchedPR, err := c.ghClient.GetPullRequest(ctx, c.owner, c.repo, prState.PRNumber)
		if err != nil {
			c.log.Warn("failed to check PR mergeable state on creation", "pr", prState.PRNumber, "error", err)
			// Non-fatal: proceed to CI wait, conflict will be caught there
		} else if c.isMergeConflict(fetchedPR) {
			return c.handleMergeConflict(ctx, prState)
		}
	}

	// All environments wait for CI - no skipping
	prState.Stage = StageWaitingCI
	prState.CIWaitStartedAt = time.Now()
	return nil
}

// handleWaitingCI checks CI status once (non-blocking) and updates state.
// Uses CheckCI instead of WaitForCI to prevent blocking the processing loop.
// Accepts optional cached ghPR to avoid redundant API calls.
func (c *Controller) handleWaitingCI(ctx context.Context, prState *PRState, ghPR *github.PullRequest) error {
	// Initialize CIWaitStartedAt if not set (backwards compatibility)
	if prState.CIWaitStartedAt.IsZero() {
		prState.CIWaitStartedAt = time.Now()
	}

	// Check for CI timeout: use the minimum of CIWaitTimeout and the environment's CITimeout.
	// This respects explicit user overrides (e.g. short timeouts in tests) while defaulting
	// to the environment-specific timeout when no override is set.
	ciTimeout := c.config.CIWaitTimeout
	envCITimeout := c.config.ResolvedEnv().CITimeout
	if envCITimeout > 0 && (ciTimeout == 0 || envCITimeout < ciTimeout) {
		ciTimeout = envCITimeout
	}

	if time.Since(prState.CIWaitStartedAt) > ciTimeout {
		c.log.Warn("CI timeout", "pr", prState.PRNumber, "waited", time.Since(prState.CIWaitStartedAt))
		prState.Stage = StageFailed
		prState.Error = fmt.Sprintf("CI timeout after %v", ciTimeout)
		return nil
	}

	// GH-419, GH-457: Always refresh HeadSHA from GitHub before checking CI.
	// Self-review or other post-creation commits can change the HEAD,
	// and OnPRCreated may have been called with an empty or stale CommitSHA.
	// The previous fix (GH-419) only handled empty SHA; stale non-empty SHAs
	// caused autopilot to query CI for the wrong commit indefinitely.
	sha := prState.HeadSHA

	// Use cached ghPR if provided, otherwise fetch it
	if ghPR == nil {
		var err error
		ghPR, err = c.ghClient.GetPullRequest(ctx, c.owner, c.repo, prState.PRNumber)
		if err != nil {
			c.log.Warn("failed to fetch PR head SHA", "pr", prState.PRNumber, "error", err)
			if sha == "" {
				return nil // Can't check CI without SHA, retry next cycle
			}
			// Fall through with existing SHA if we have one
		}
	}

	if ghPR != nil && ghPR.Head.SHA != "" {
		if sha != "" && sha != ghPR.Head.SHA {
			c.log.Info("refreshed stale HeadSHA from GitHub",
				"pr", prState.PRNumber,
				"old", ShortSHA(sha),
				"new", ShortSHA(ghPR.Head.SHA),
			)
		} else if sha == "" {
			c.log.Info("refreshed empty HeadSHA from GitHub",
				"pr", prState.PRNumber,
				"sha", ShortSHA(ghPR.Head.SHA),
			)
		}
		prState.HeadSHA = ghPR.Head.SHA
		sha = ghPR.Head.SHA
	} else if sha == "" {
		c.log.Warn("GitHub returned empty SHA for PR", "pr", prState.PRNumber)
		return nil // Retry next cycle
	}

	// GH-724: Check for merge conflicts before waiting for CI.
	// Conflicting PRs will never have CI run, so waiting is pointless.
	if ghPR != nil && c.isMergeConflict(ghPR) {
		return c.handleMergeConflict(ctx, prState)
	}

	// Non-blocking CI status check
	status, err := c.ciMonitor.CheckCI(ctx, sha)
	if err != nil {
		prState.ConsecutiveAPIFailures++
		c.log.Warn("CI status check failed",
			"pr", prState.PRNumber,
			"sha", ShortSHA(sha),
			"consecutive_failures", prState.ConsecutiveAPIFailures,
			"error", err)

		// If we've had 5 consecutive failures, transition to failed stage
		if prState.ConsecutiveAPIFailures >= 5 {
			prState.Stage = StageFailed
			prState.Error = fmt.Sprintf("CI check API failed %d consecutive times: %v",
				prState.ConsecutiveAPIFailures, err)
			c.log.Error("PR transitioned to failed due to consecutive API failures",
				"pr", prState.PRNumber,
				"consecutive_failures", prState.ConsecutiveAPIFailures)
			return nil
		}

		// Don't fail the PR on transient errors, will retry next poll cycle
		return nil
	}

	// Reset failure counter on successful API call
	prState.ConsecutiveAPIFailures = 0

	// GH-862: Capture discovered checks for PR state (only once, when first seen)
	if discovered := c.ciMonitor.GetDiscoveredChecks(sha); len(discovered) > 0 && len(prState.DiscoveredChecks) == 0 {
		prState.DiscoveredChecks = discovered
		c.log.Info("CI checks discovered", "pr", prState.PRNumber, "checks", discovered)
	}

	prState.CIStatus = status
	prState.LastChecked = time.Now()

	c.log.Debug("CI status check result",
		"pr", prState.PRNumber,
		"sha", ShortSHA(sha),
		"status", status,
	)

	switch status {
	case CISuccess:
		c.log.Info("CI passed", "pr", prState.PRNumber, "sha", ShortSHA(sha))
		prState.Stage = StageCIPassed
		if !prState.CIWaitStartedAt.IsZero() {
			c.metrics.RecordCIWaitDuration(time.Since(prState.CIWaitStartedAt))
		}
	case CIFailure:
		c.log.Warn("CI failed", "pr", prState.PRNumber, "sha", ShortSHA(sha))
		prState.Stage = StageCIFailed
		if !prState.CIWaitStartedAt.IsZero() {
			c.metrics.RecordCIWaitDuration(time.Since(prState.CIWaitStartedAt))
		}
	case CIPending, CIRunning:
		// Stay in StageWaitingCI, will be checked next poll cycle
		c.log.Debug("CI still running", "pr", prState.PRNumber, "status", status)
	}

	return nil
}

// handleCIPassed proceeds to merge (with approval if required by environment config
// or by the scope-drift / size-floor defense-in-depth rails).
func (c *Controller) handleCIPassed(ctx context.Context, prState *PRState) error {
	c.log.Info("handleCIPassed: CI passed, determining next stage",
		"pr", prState.PRNumber,
		"env", c.config.EnvironmentName(),
		"auto_merge", c.config.AutoMerge,
	)

	// Defense-in-depth: scope-drift and size-floor gates escalate to human approval
	// regardless of env RequireApproval config. Born from OAuth cascade #2
	// (#2572/#2584/#2585): a runaway executor must not land oversized or scope-drifting
	// code unsupervised even when env config drops require_approval.
	var escalateReason string
	files, listErr := c.ghClient.ListPullRequestFiles(ctx, c.owner, c.repo, prState.PRNumber)
	if listErr != nil {
		c.log.Warn("handleCIPassed: ListPullRequestFiles failed, skipping size-floor gate (fail-open)",
			"pr", prState.PRNumber, "error", listErr)
	} else if reason := SizeFloorReason(files); reason != "" {
		escalateReason = reason
	}

	// Architectural guardrails gate (report-only by default): evaluate repo
	// rules over this PR's changed files and surface them as a commit status +
	// PR comment. Reuses the files already fetched above when available. This is
	// FAIL-OPEN and NEVER blocks the merge — blocking, when configured, is
	// expressed solely via the pilot/guardrails commit status, so the merge path
	// below is unaffected. A nil/disabled gate is a no-op.
	c.runGuardrailsGate(ctx, prState, files)

	if escalateReason == "" && prState.IssueNumber > 0 {
		issue, issueErr := c.ghClient.GetIssue(ctx, c.owner, c.repo, prState.IssueNumber)
		if issueErr != nil {
			c.log.Warn("handleCIPassed: GetIssue failed, skipping scope-drift gate (fail-open)",
				"pr", prState.PRNumber, "issue", prState.IssueNumber, "error", issueErr)
		} else if reason := ScopeDriftReason(prState.PRTitle, issue.Title); reason != "" {
			escalateReason = reason
		}
	}

	if escalateReason != "" {
		c.log.Warn("merge gate escalated: requiring human approval",
			"pr", prState.PRNumber, "reason", escalateReason)
		prState.Stage = StageAwaitApproval
		if c.notifier != nil {
			if err := c.notifier.NotifyApprovalRequired(ctx, prState); err != nil {
				c.log.Warn("failed to send approval notification", "error", err)
			}
		}
		return nil
	}

	if c.config.ResolvedEnv().RequireApproval {
		c.log.Info("awaiting approval before merge", "pr", prState.PRNumber)
		prState.Stage = StageAwaitApproval

		// Notify approval required
		if c.notifier != nil {
			if err := c.notifier.NotifyApprovalRequired(ctx, prState); err != nil {
				c.log.Warn("failed to send approval notification", "error", err)
			}
		}
	} else {
		c.log.Info("proceeding to merge",
			"pr", prState.PRNumber,
			"env", c.config.EnvironmentName(),
		)
		prState.Stage = StageMerging
	}
	return nil
}

// handleCIFailed creates fix issue via feedback loop.
// GH-1566: Tracks CI fix iteration depth to prevent infinite cascade.
// Each fix issue embeds an iteration counter in autopilot-meta; when the
// counter reaches MaxCIFixIterations the PR transitions to StageFailed
// instead of spawning another fix issue.
func (c *Controller) handleCIFailed(ctx context.Context, prState *PRState) error {
	failedChecks, err := c.ciMonitor.GetFailedChecks(ctx, prState.HeadSHA)
	if err != nil {
		c.log.Warn("failed to get failed checks", "error", err)
		// Continue with empty list
	}

	// Notify CI failure
	if c.notifier != nil {
		if err := c.notifier.NotifyCIFailed(ctx, prState, failedChecks); err != nil {
			c.log.Warn("failed to send CI failure notification", "error", err)
		}
	}

	// GH-1566: Check CI fix iteration depth from the originating issue.
	// If this PR was created from an autopilot-fix issue, that issue's body
	// contains an iteration counter. Stop the cascade when limit is reached.
	iteration := 0
	if prState.IssueNumber > 0 && c.config.MaxCIFixIterations > 0 {
		issue, err := c.ghClient.GetIssue(ctx, c.owner, c.repo, prState.IssueNumber)
		if err != nil {
			c.log.Warn("failed to fetch issue for iteration check", "issue", prState.IssueNumber, "error", err)
			// Continue with iteration=0 (safe: won't block on transient error)
		} else {
			iteration = parseAutopilotIteration(issue.Body)
		}

		if iteration >= c.config.MaxCIFixIterations {
			c.log.Warn("CI fix iteration limit reached, stopping cascade",
				"pr", prState.PRNumber,
				"issue", prState.IssueNumber,
				"iteration", iteration,
				"max", c.config.MaxCIFixIterations,
			)

			// Close the failed PR so the sequential poller can unblock
			if err := c.ghClient.ClosePullRequest(ctx, c.owner, c.repo, prState.PRNumber); err != nil {
				c.log.Warn("failed to close failed PR", "pr", prState.PRNumber, "error", err)
			}

			// GH-3260: Sync board card to "Blocked/Failed" column on execution failure (iteration limit).
			if c.boardSync != nil && prState.IssueNodeID != "" && c.failStatus != "" {
				if err := c.boardSync.UpdateProjectItemStatus(ctx, prState.IssueNodeID, c.failStatus); err != nil {
					c.log.Warn("board sync on exec failure (iteration limit) failed", "pr", prState.PRNumber, "error", err)
				}
			}

			prState.Stage = StageFailed
			prState.Error = fmt.Sprintf("CI fix iteration limit reached (%d/%d): stopping cascade to prevent infinite loop", iteration, c.config.MaxCIFixIterations)
			c.metrics.RecordPRFailed()
			c.metrics.RecordIssueProcessed("failed")
			return nil
		}
	}

	// GH-2588: Cascade-2 size-guard — if the failing PR already exceeds the size floor,
	// it is a likely contamination cascade. Refuse to spawn another fix(ci) issue.
	if c.config.MaxCIFixPRSize > 0 {
		files, err := c.ghClient.ListPullRequestFiles(ctx, c.owner, c.repo, prState.PRNumber)
		if err != nil {
			c.log.Warn("CI fix size guard: ListPullRequestFiles failed, skipping guard (fail-open)",
				"pr", prState.PRNumber, "error", err)
			// Fall through — belt-and-suspenders: merge-time SizeFloor gate in handleCIPassed catches it.
		} else {
			netAdditions := 0
			for _, f := range files {
				netAdditions += f.Additions
			}
			if netAdditions > c.config.MaxCIFixPRSize {
				c.log.Warn("CI fix size guard fired — failing PR exceeds size floor, refusing to spawn fix issue",
					"pr", prState.PRNumber, "additions", netAdditions, "limit", c.config.MaxCIFixPRSize)
				if err := c.ghClient.ClosePullRequest(ctx, c.owner, c.repo, prState.PRNumber); err != nil {
					c.log.Warn("CI fix size guard: failed to close oversized failed PR",
						"pr", prState.PRNumber, "error", err)
				}
				// GH-3260: Sync board card to "Blocked/Failed" column on execution failure (size guard).
				if c.boardSync != nil && prState.IssueNodeID != "" && c.failStatus != "" {
					if err := c.boardSync.UpdateProjectItemStatus(ctx, prState.IssueNodeID, c.failStatus); err != nil {
						c.log.Warn("board sync on exec failure (size guard) failed", "pr", prState.PRNumber, "error", err)
					}
				}
				prState.Stage = StageFailed
				prState.Error = fmt.Sprintf("CI fix size guard: PR has %d additions, over limit %d (likely cascade contamination — escalate to human)", netAdditions, c.config.MaxCIFixPRSize)
				c.metrics.RecordPRFailed()
				c.metrics.RecordIssueProcessed("failed")
				return nil
			}
		}
	}

	// GH-1567: Fetch actual CI error logs to include in fix issues.
	// This prevents Pilot from having to rediscover errors by running linter/tests itself.
	ciLogs := c.ciMonitor.GetFailedCheckLogs(ctx, prState.HeadSHA, 2000)

	issueNum, err := c.feedbackLoop.CreateFailureIssue(ctx, prState, FailureCIPreMerge, failedChecks, ciLogs, iteration+1)
	if err != nil {
		return fmt.Errorf("failed to create fix issue: %w", err)
	}

	// GH-1964/GH-1979: Learn from CI failure patterns (self-improvement).
	// Guard: skip learning when CI logs are empty/whitespace (nothing to extract).
	if c.learningLoop != nil && strings.TrimSpace(ciLogs) != "" {
		projectPath := c.owner + "/" + c.repo
		if learnErr := c.learningLoop.LearnFromCIFailure(ctx, projectPath, ciLogs, failedChecks); learnErr != nil {
			c.log.Warn("Failed to learn from CI failure", slog.Any("error", learnErr))
		}
	}

	// Notify fix issue created
	if c.notifier != nil {
		if err := c.notifier.NotifyFixIssueCreated(ctx, prState, issueNum); err != nil {
			c.log.Warn("failed to send fix issue notification", "error", err)
		}
	}

	c.log.Info("created fix issue for CI failure", "pr", prState.PRNumber, "issue", issueNum)

	// Close the failed PR on GitHub so the sequential poller's merge waiter
	// can unblock and pick up the fix issue. Without this, the poller stays
	// blocked in WaitWithCallback() waiting for a PR that will never merge.
	if err := c.ghClient.ClosePullRequest(ctx, c.owner, c.repo, prState.PRNumber); err != nil {
		c.log.Warn("failed to close failed PR", "pr", prState.PRNumber, "error", err)
		// Non-fatal: merge waiter will eventually timeout
	} else {
		c.log.Info("closed failed PR", "pr", prState.PRNumber, "fix_issue", issueNum)
	}

	// GH-1870: Sync board card to "Failed" column on CI failure
	if c.boardSync != nil && prState.IssueNodeID != "" && c.failStatus != "" {
		if err := c.boardSync.UpdateProjectItemStatus(ctx, prState.IssueNodeID, c.failStatus); err != nil {
			c.log.Warn("board sync on CI fail failed", "pr", prState.PRNumber, "error", err)
		}
	}

	prState.Stage = StageFailed
	c.metrics.RecordPRFailed()
	return nil
}

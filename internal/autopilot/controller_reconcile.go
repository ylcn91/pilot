package autopilot

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// parentIssueRe extracts a parent issue number from a sub-issue body line like
// "Parent: GH-3344" — the convention epic.go writes when decomposing an issue
// into sub-issues. TASK-352.
var parentIssueRe = regexp.MustCompile(`(?i)Parent:\s*GH-(\d+)`)

// selfHealForPR promotes any prior "failed" execution rows for the merged PR's
// issue — and its parent epic, if it is a sub-issue — to "completed", stamping the
// PR URL so the dashboard reflects the merged outcome. Safe to call from any merge
// path (controller-driven handleMerging or the externally-merged scan). No-op when
// the execution healer is unset or issueNum is zero. TASK-352.
func (c *Controller) selfHealForPR(ctx context.Context, issueNum int, prURL string) {
	if c.executionHealer == nil || issueNum == 0 {
		return
	}
	c.selfHealTask(fmt.Sprintf("GH-%d", issueNum), prURL)
	// Pilot decomposes a parent issue into sub-issues; only the sub-issue's PR
	// merges, so the parent's no-op "failed" row would never heal otherwise.
	if parent := c.resolveParentIssue(ctx, issueNum); parent != 0 && parent != issueNum {
		c.selfHealTask(fmt.Sprintf("GH-%d", parent), prURL)
	}
}

// selfHealTask runs SelfHealExecutionAfterMerge for one task ID, scoped to this
// controller's project path (empty = task_id-only match). TASK-352.
func (c *Controller) selfHealTask(taskID, prURL string) {
	if err := c.executionHealer.SelfHealExecutionAfterMerge(taskID, c.projectPath, prURL); err != nil {
		c.log.Warn("failed to self-heal execution on merge", "task_id", taskID, "error", err)
	}
}

// resolveParentIssue returns the parent issue number for a sub-issue by parsing
// the "Parent: GH-N" line epic.go writes into sub-issue bodies, or 0 if the issue
// has no parent or cannot be fetched (best-effort, fail-open). TASK-352.
func (c *Controller) resolveParentIssue(ctx context.Context, issueNum int) int {
	issue, err := c.ghClient.GetIssue(ctx, c.owner, c.repo, issueNum)
	if err != nil || issue == nil {
		return 0
	}
	if m := parentIssueRe.FindStringSubmatch(issue.Body); len(m) == 2 {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

// ScanExistingPRs scans for open PRs created by Pilot and restores their state.
// This should be called on startup to track PRs that were created before the current session.
func (c *Controller) ScanExistingPRs(ctx context.Context) error {
	c.log.Info("scanning for existing Pilot PRs",
		"owner", c.owner,
		"repo", c.repo,
	)

	prs, err := c.ghClient.ListPullRequests(ctx, c.owner, c.repo, "open")
	if err != nil {
		return fmt.Errorf("failed to list PRs: %w", err)
	}

	c.log.Debug("found open PRs", "total", len(prs))

	restored := 0
	for _, pr := range prs {
		// Filter for Pilot branches (pilot/GH-*)
		if !strings.HasPrefix(pr.Head.Ref, "pilot/GH-") {
			c.log.Debug("skipping non-Pilot PR",
				"pr", pr.Number,
				"branch", pr.Head.Ref,
			)
			continue
		}

		// Extract issue number from branch name
		var issueNum int
		if _, err := fmt.Sscanf(pr.Head.Ref, "pilot/GH-%d", &issueNum); err != nil {
			c.log.Warn("failed to parse branch name", "branch", pr.Head.Ref, "error", err)
			continue
		}

		// Skip PRs already tracked via RestoreState — OnPRCreated would clobber
		// their persisted stage (e.g. StageWaitingCI) back to StagePRCreated and
		// reset CIWaitStartedAt, making CI timers restart from zero after every
		// Pilot restart. RestoreState is authoritative for PRs in SQLite; this
		// scan only registers genuine orphans (PRs created while Pilot was down).
		c.mu.RLock()
		_, alreadyTracked := c.activePRs[pr.Number]
		c.mu.RUnlock()
		if alreadyTracked {
			c.log.Debug("skipping already-tracked PR in scan", "pr", pr.Number, "branch", pr.Head.Ref)
			continue
		}

		c.log.Info("restoring Pilot PR for tracking",
			"pr", pr.Number,
			"branch", pr.Head.Ref,
			"sha", ShortSHA(pr.Head.SHA),
			"issue", issueNum,
		)

		// Register PR via existing mechanism
		c.OnPRCreated(pr.Number, pr.HTMLURL, issueNum, pr.Head.SHA, pr.Head.Ref, "")
		c.metrics.RecordOrphanPRRegistered("startup_scan")
		restored++
	}

	c.log.Info("completed PR scan", "restored", restored, "env", c.config.EnvironmentName())
	return nil
}

// startReconciler runs a periodic loop that calls reconcileOrphanPRs once per
// minute. It is launched as a goroutine by Run and exits when ctx is cancelled.
func (c *Controller) startReconciler(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.reconcileOrphanPRs(ctx)
		}
	}
}

// reconcileOrphanPRs lists all open pilot/ PRs and registers any that are not
// currently tracked in activePRs. A PR is orphaned when OnPRCreated was never
// fired — e.g. the executor returned pr_url="" or the poller gate filtered it.
// The function is idempotent and safe to call concurrently with processAllPRs.
func (c *Controller) reconcileOrphanPRs(ctx context.Context) {
	prs, err := c.ghClient.ListPullRequests(ctx, c.owner, c.repo, "open")
	if err != nil {
		c.log.Warn("reconciler: failed to list open PRs", "error", err)
		return
	}

	for _, pr := range prs {
		if !strings.HasPrefix(pr.Head.Ref, "pilot/GH-") {
			continue
		}

		c.mu.RLock()
		_, tracked := c.activePRs[pr.Number]
		c.mu.RUnlock()
		if tracked {
			continue
		}

		var issueNum int
		if _, err := fmt.Sscanf(pr.Head.Ref, "pilot/GH-%d", &issueNum); err != nil {
			c.log.Warn("reconciler: failed to parse branch name", "branch", pr.Head.Ref, "error", err)
			continue
		}

		c.log.Warn("reconciler: registering orphan PR",
			"pr", pr.Number,
			"branch", pr.Head.Ref,
			"issue", issueNum,
		)
		c.OnPRCreated(pr.Number, pr.HTMLURL, issueNum, pr.Head.SHA, pr.Head.Ref, "")
		c.metrics.RecordOrphanPRRegistered("reconciler")
	}
}

// ScanRecentlyMergedPRs scans for Pilot PRs that were merged externally.
// This catches PRs that need release triggering but were merged outside of
// autopilot (e.g. via `gh pr merge` or the GitHub UI).
// Called on startup and periodically from the Run loop.
func (c *Controller) ScanRecentlyMergedPRs(ctx context.Context) error {
	// TASK-356 #2 (decouple): the scan reconciles externally-merged Pilot PRs —
	// release triggering, merge metrics, execution-row self-heal, AND board
	// write-back. Run it whenever EITHER auto-release OR board sync is enabled, so
	// a board-sourced (non-releasing) setup still gets its cards moved to Done on a
	// manual merge. The release-triggering tail below is separately gated on
	// releaseEnabled so nothing tries to tag a release when release is off.
	releaseEnabled := c.shouldTriggerRelease()
	boardEnabled := c.boardSync != nil && c.doneStatus != ""
	if !releaseEnabled && !boardEnabled {
		c.log.Debug("skipping merged PR scan: neither auto-release nor board sync enabled")
		return nil
	}

	scanWindow := c.config.MergedPRScanWindow
	if scanWindow == 0 {
		scanWindow = 30 * time.Minute // Default fallback
	}

	c.log.Info("scanning for recently merged Pilot PRs",
		"owner", c.owner,
		"repo", c.repo,
		"window", scanWindow,
	)

	// List closed PRs
	prs, err := c.ghClient.ListPullRequests(ctx, c.owner, c.repo, "closed")
	if err != nil {
		return fmt.Errorf("failed to list closed PRs: %w", err)
	}

	c.log.Debug("found closed PRs", "total", len(prs))

	cutoff := time.Now().Add(-scanWindow)
	triggered := 0

	for _, pr := range prs {
		// Filter for Pilot branches (pilot/GH-* or pilot/*)
		if !strings.HasPrefix(pr.Head.Ref, "pilot/") {
			continue
		}

		// Must be merged (not just closed)
		if !pr.Merged {
			continue
		}

		// Check if merged within scan window
		// MergedAt is RFC3339 format string
		if pr.MergedAt == "" {
			continue
		}
		mergedAt, err := time.Parse(time.RFC3339, pr.MergedAt)
		if err != nil {
			c.log.Warn("failed to parse MergedAt", "pr", pr.Number, "merged_at", pr.MergedAt, "error", err)
			continue
		}
		if mergedAt.Before(cutoff) {
			continue
		}

		// Extract issue number from branch name (optional)
		var issueNum int
		if strings.HasPrefix(pr.Head.Ref, "pilot/GH-") {
			_, _ = fmt.Sscanf(pr.Head.Ref, "pilot/GH-%d", &issueNum)
		}

		// Record merge metrics BEFORE the activePRs/release-exists skip gates
		// below — those gates exist to avoid duplicate release triggering, but
		// the metric must fire on every discovered merged Pilot PR regardless
		// of whether a release tag already exists or whether autopilot tracked
		// the PR through creation. recordMergeSuccess is idempotent via
		// recordedMerges so handleMerging + scanner can both call it.
		// Use pr.CreatedAt for a meaningful time-to-merge sample; fall back to
		// mergedAt so the histogram still records on PRs missing CreatedAt.
		createdAt, _ := time.Parse(time.RFC3339, pr.CreatedAt)
		if createdAt.IsZero() {
			createdAt = mergedAt
		}
		c.recordMergeSuccess(&PRState{PRNumber: pr.Number, CreatedAt: createdAt})

		// TASK-352: Self-heal execution records for externally-merged PRs (gh pr
		// merge / GitHub UI). These never pass through handleMerging, so their
		// "failed" rows would otherwise never flip to "completed". Like
		// recordMergeSuccess above, this fires before the release-tag/activePRs skip
		// gates because the heal must happen on every discovered merged Pilot PR.
		c.selfHealForPR(ctx, issueNum, pr.HTMLURL)

		// TASK-356 #2: board write-back for externally-merged PRs. Large PRs that
		// hit the stage approval-misconfig (require_approval=true + approval disabled)
		// are merged manually (`gh pr merge` / GitHub UI) and never pass through
		// handleMerging, so their board card stays stuck "In Review". Move it to Done
		// here, mirroring the on-merge write-back in handleMerging. Like
		// recordMergeSuccess/selfHealForPR above, this fires on every discovered merged
		// Pilot PR (before the release-tag/activePRs skip gates) and is independent of
		// whether release is enabled. UpdateProjectItemStatus is idempotent and silently
		// skips issues that aren't on the board.
		if boardEnabled && issueNum > 0 {
			if nodeID, nodeErr := c.ghClient.GetIssueNodeID(ctx, c.owner, c.repo, issueNum); nodeErr != nil {
				c.log.Warn("board sync on external merge: failed to resolve issue node id",
					"pr", pr.Number, "issue", issueNum, "error", nodeErr)
			} else if err := c.boardSync.UpdateProjectItemStatus(ctx, nodeID, c.doneStatus); err != nil {
				c.log.Warn("board sync on external merge failed",
					"pr", pr.Number, "issue", issueNum, "error", err)
			}
		}

		// Everything below is release-triggering machinery — skip it entirely when
		// release is disabled (the scan may be running for board sync alone).
		if !releaseEnabled {
			continue
		}

		// Skip if already tracked in activePRs (avoid duplicate processing)
		c.mu.RLock()
		_, alreadyTracked := c.activePRs[pr.Number]
		c.mu.RUnlock()
		if alreadyTracked {
			continue
		}

		// B3 (TASK-309): activePRs is in-memory only. After a daemon restart a PR
		// can be persisted at stage='releasing' yet be absent from activePRs, so the
		// in-memory gate above would re-register and re-trigger the release on every
		// scan. Consult the persistent state: if a recent 'releasing' row exists, the
		// release is already in flight — skip it. Stale rows (age past
		// releasingStaleThreshold) are intentionally NOT skipped so a genuinely
		// wedged release can be re-driven.
		if c.stateStore != nil {
			if age, found, err := c.stateStore.PersistedReleasingAge(pr.Number); err != nil {
				c.log.Warn("failed to check persisted releasing state, will track to be safe",
					"pr", pr.Number,
					"error", err,
				)
			} else if found && age < releasingStaleThreshold {
				c.log.Debug("skipping PR: release already in flight (persisted at releasing)",
					"pr", pr.Number,
					"age", age,
				)
				continue
			}
		}

		// Skip if this merge commit already has a release tag.
		// GitHub releases set target_commitish to the branch ref ("main"), not the merge
		// SHA, so the former map-based check was unreliable. GetTagForSHA (same primitive
		// handleReleasing uses) is the reliable check.
		if pr.MergeCommitSHA != "" {
			existingTag, tagErr := c.ghClient.GetTagForSHA(ctx, c.owner, c.repo, pr.MergeCommitSHA)
			if tagErr != nil {
				c.log.Warn("failed to check existing tag for PR, will track to be safe",
					"pr", pr.Number,
					"merge_sha", ShortSHA(pr.MergeCommitSHA),
					"error", tagErr,
				)
			} else if existingTag != "" {
				c.log.Debug("skipping PR: merge commit already tagged",
					"pr", pr.Number,
					"merge_sha", ShortSHA(pr.MergeCommitSHA),
					"tag", existingTag,
				)
				continue
			}
		}

		c.log.Info("found merged Pilot PR needing release",
			"pr", pr.Number,
			"branch", pr.Head.Ref,
			"merged_at", mergedAt,
			"merge_sha", ShortSHA(pr.MergeCommitSHA),
		)

		// Create PR state and trigger release
		prState := &PRState{
			PRNumber:        pr.Number,
			PRURL:           pr.HTMLURL,
			IssueNumber:     issueNum,
			BranchName:      pr.Head.Ref,
			HeadSHA:         pr.MergeCommitSHA,
			Stage:           StageReleasing,
			CIStatus:        CISuccess, // Assume CI passed if merged
			CreatedAt:       time.Now(),
			EnvironmentName: c.config.EnvironmentName(),
			PRTitle:         pr.Title,
			TargetBranch:    pr.Base.Ref,
		}

		// Register and trigger release
		c.mu.Lock()
		c.activePRs[pr.Number] = prState
		c.mu.Unlock()
		// prState is now published in activePRs, so a concurrent ProcessPR or
		// webhook could already hold the pointer — persist under prState.mu per
		// the caller-holds-the-lock contract (mirrors OnPRCreated).
		prState.mu.Lock()
		c.persistPRState(prState)
		prState.mu.Unlock()

		triggered++
	}

	c.log.Info("completed merged PR scan",
		"triggered", triggered,
		"window", scanWindow,
	)

	return nil
}

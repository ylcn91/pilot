package autopilot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ylcn91/pilot/internal/logging"
	"github.com/ylcn91/pilot/internal/memory"
)

// handleMerged checks post-merge CI based on environment config.
func (c *Controller) handleMerged(ctx context.Context, prState *PRState) error {
	c.log.Info("handleMerged: PR merged, checking next steps",
		"pr", prState.PRNumber,
		"env", c.config.EnvironmentName(),
		"should_release", c.shouldTriggerRelease(),
	)

	// GH-1823: Learn from PR reviews (self-improvement).
	// Fetch reviews and line-level comments after merge, when the review cycle is complete.
	if c.learningLoop != nil {
		reviews, err := c.ghClient.ListPullRequestReviews(ctx, c.owner, c.repo, prState.PRNumber)
		if err != nil {
			c.log.Warn("Failed to fetch reviews for learning", slog.Any("error", err))
		} else if len(reviews) > 0 {
			var reviewData []*memory.ReviewData
			for _, r := range reviews {
				if r.Body == "" {
					continue // Skip click-only approvals
				}
				reviewData = append(reviewData, &memory.ReviewData{
					Body:     r.Body,
					State:    r.State,
					Reviewer: r.User.Login,
				})
			}

			// Also fetch line-level comments for richer signal
			comments, err := c.ghClient.GetPullRequestComments(ctx, c.owner, c.repo, prState.PRNumber)
			if err == nil {
				for _, comment := range comments {
					reviewData = append(reviewData, &memory.ReviewData{
						Body:     comment.Body,
						State:    "COMMENTED",
						Reviewer: comment.User.Login,
					})
				}
			}

			if len(reviewData) > 0 {
				projectPath := "" // resolved from prState if project path is available
				if learnErr := c.learningLoop.LearnFromReview(ctx, projectPath, reviewData, prState.PRURL); learnErr != nil {
					c.log.Warn("Failed to learn from reviews", slog.Any("error", learnErr))
				} else {
					c.log.Info("Learned from PR reviews",
						slog.Int("pr", prState.PRNumber),
						slog.Int("reviews", len(reviewData)),
					)
				}
			}
		}
	}

	// GH-2086: Close parent issue when all sub-issues are done.
	c.maybeCloseParentIssue(ctx, prState)

	if c.config.ResolvedEnv().SkipPostMergeCI {
		// Fast path: skip post-merge CI, check if we should release immediately
		if c.shouldTriggerRelease() && !c.resolvedRelease().RequireCI {
			c.log.Info("skipping post-merge CI: proceeding to release",
				"pr", prState.PRNumber,
			)
			prState.Stage = StageReleasing
			return nil
		}
		c.log.Info("skipping post-merge CI: PR complete", "pr", prState.PRNumber)
		c.removePR(prState.PRNumber)
		return nil
	}

	// Wait for post-merge CI
	c.log.Info("waiting for post-merge CI",
		"pr", prState.PRNumber,
		"env", c.config.EnvironmentName(),
	)
	prState.Stage = StagePostMergeCI
	return nil
}

// handlePostMergeCI monitors post-merge checks (non-blocking).
// Each tick calls CheckCI once and either advances the stage or returns to wait
// for the next tick, mirroring the pattern used by handleWaitingCI.
func (c *Controller) handlePostMergeCI(ctx context.Context, prState *PRState) error {
	// Capture main branch SHA on first entry; persisted so daemon restarts resume
	// monitoring the same commit rather than picking up a newer one.
	if prState.PostMergeSHA == "" {
		sha, err := c.getMainBranchSHA(ctx)
		if err != nil {
			c.log.Warn("failed to get main branch SHA, using head SHA", "error", err)
			sha = prState.HeadSHA
		}
		prState.PostMergeSHA = sha
	}

	// Start the CI timer on first tick.
	if prState.PostMergeCIStartedAt.IsZero() {
		prState.PostMergeCIStartedAt = time.Now()
	}

	// Enforce timeout using same logic as handleWaitingCI.
	ciTimeout := c.config.CIWaitTimeout
	envCITimeout := c.config.ResolvedEnv().CITimeout
	if envCITimeout > 0 && (ciTimeout == 0 || envCITimeout < ciTimeout) {
		ciTimeout = envCITimeout
	}
	if time.Since(prState.PostMergeCIStartedAt) > ciTimeout {
		c.log.Warn("post-merge CI timeout", "pr", prState.PRNumber, "waited", time.Since(prState.PostMergeCIStartedAt))
		prState.Stage = StageFailed
		prState.Error = fmt.Sprintf("post-merge CI timeout after %v", ciTimeout)
		return nil
	}

	mainSHA := prState.PostMergeSHA
	status, err := c.ciMonitor.CheckCI(ctx, mainSHA)
	if err != nil {
		// Transient API error — log and retry next tick without failing the PR.
		c.log.Warn("post-merge CI status check failed", "pr", prState.PRNumber, "sha", ShortSHA(mainSHA), "error", err)
		return nil
	}

	prState.CIStatus = status
	prState.LastChecked = time.Now()

	switch status {
	case CISuccess:
		c.log.Info("post-merge CI passed", "pr", prState.PRNumber, "sha", ShortSHA(mainSHA))
		if c.shouldTriggerRelease() {
			prState.Stage = StageReleasing
			return nil
		}
		c.removePR(prState.PRNumber)

	case CIFailure:
		c.log.Warn("post-merge CI failed", "pr", prState.PRNumber, "sha", ShortSHA(mainSHA))
		failedChecks, _ := c.ciMonitor.GetFailedChecks(ctx, mainSHA)
		// GH-1567: Fetch CI error logs for post-merge failures too.
		ciLogs := c.ciMonitor.GetFailedCheckLogs(ctx, mainSHA, 2000)
		// Post-merge failures start a new lineage (iteration 1), not part of pre-merge cascade.
		issueNum, issueErr := c.feedbackLoop.CreateFailureIssue(ctx, prState, FailureCIPostMerge, failedChecks, ciLogs, 1)
		if issueErr != nil {
			c.log.Error("failed to create post-merge fix issue", "error", issueErr)
		} else {
			c.log.Info("created fix issue for post-merge CI failure", "pr", prState.PRNumber, "issue", issueNum)
		}
		// GH-1964/GH-1979: Learn from post-merge CI failure patterns (self-improvement).
		// Guard: skip learning when CI logs are empty/whitespace (nothing to extract).
		if c.learningLoop != nil && strings.TrimSpace(ciLogs) != "" {
			projectPath := c.owner + "/" + c.repo
			if learnErr := c.learningLoop.LearnFromCIFailure(ctx, projectPath, ciLogs, failedChecks); learnErr != nil {
				c.log.Warn("Failed to learn from post-merge CI failure", slog.Any("error", learnErr))
			}
		}
		c.removePR(prState.PRNumber)

	default:
		// CIPending or CIRunning — stay in StagePostMergeCI and wait for next tick.
		c.log.Debug("post-merge CI still running", "pr", prState.PRNumber, "sha", ShortSHA(mainSHA), "status", status)
	}

	return nil
}

// getMainBranchSHA returns the current SHA of the main branch.
//
// TASK-291: previously hardcoded "main" — this silently broke post-merge CI
// monitoring on repos defaulting to develop/master/trunk (releases could fire
// before main-branch CI completed). Now reads ResolvedEnv().Branch and falls
// back to literal "main" with a WARN log when no environment branch is set.
func (c *Controller) getMainBranchSHA(ctx context.Context) (string, error) {
	branchName := c.resolveMainBranchName()
	branch, err := c.ghClient.GetBranch(ctx, c.owner, c.repo, branchName)
	if err != nil {
		return "", err
	}
	return branch.SHA(), nil
}

// resolveMainBranchName returns the branch name post-merge CI should track.
// Preference order:
//  1. c.config.ResolvedEnv().Branch — the per-environment branch (prod=main, stage=develop, etc.)
//  2. Literal "main" with a WARN log — last-resort fallback so we never block a release on an empty branch name.
//
// A broader fallback through ProjectConfig (BranchFrom/DefaultBranch) would
// require wiring the pilot global Config into autopilot.Controller — deferred
// to a follow-up; not needed for the workshop-scope incident this fix targets.
func (c *Controller) resolveMainBranchName() string {
	if env := c.config.ResolvedEnv(); env != nil && env.Branch != "" {
		return env.Branch
	}
	c.log.Warn("resolveMainBranchName: no environment branch configured, falling back to literal \"main\" — set environments.<env>.branch to silence this warning",
		"owner", c.owner,
		"repo", c.repo,
	)
	return "main"
}

// resolveRelease is a package-level helper used during construction (before Controller
// exists) and by the resolvedRelease method below. Env-scoped config wins over global.
func resolveRelease(cfg *Config) *ReleaseConfig {
	if env := cfg.ResolvedEnv(); env != nil && env.Release != nil {
		return env.Release
	}
	return cfg.Release
}

// resolvedRelease returns the effective release config, preferring per-environment
// config over global. Returns nil if neither is set.
func (c *Controller) resolvedRelease() *ReleaseConfig {
	return resolveRelease(c.config)
}

// shouldTriggerRelease returns true if auto-release is configured.
func (c *Controller) shouldTriggerRelease() bool {
	rel := c.resolvedRelease()
	return rel != nil && rel.Enabled && rel.Trigger == "on_merge"
}

// detectBumpFromPRLabels fetches the PR's labels (PRs are issues in the GitHub
// API, so the issues endpoint returns their labels) and maps them to a bump type
// via DetectBumpFromLabels. Used by the "pr_labels" version strategy.
func (c *Controller) detectBumpFromPRLabels(ctx context.Context, owner, repo string, prNumber int) (BumpType, error) {
	issue, err := c.ghClient.GetIssue(ctx, owner, repo, prNumber)
	if err != nil {
		return BumpNone, err
	}
	return DetectBumpFromLabels(issue.Labels), nil
}

// handleReleasing creates a release after successful merge and CI.
func (c *Controller) handleReleasing(ctx context.Context, prState *PRState) error {
	if c.releaser == nil {
		c.log.Debug("releaser not configured, skipping release", "pr", prState.PRNumber)
		c.removePR(prState.PRNumber)
		return nil
	}

	// Resolve the actual repo owner/name from the PR URL.
	// Cross-repo PRs (e.g. auth-service) have a PRURL pointing to a different repo
	// than c.owner/c.repo (the pilot repo). All release API calls must target the
	// correct repo to avoid stuck-forever releasing state.
	owner, repo := prState.RepoOwnerAndName(c.owner, c.repo)

	// Race condition guard: Check if this commit already has a tag.
	// When multiple PRs merge rapidly, each triggers handleReleasing but only
	// the first should create a tag. Subsequent PRs will see their merge commit
	// is already tagged (by an earlier release) and skip.
	existingTag, err := c.ghClient.GetTagForSHA(ctx, owner, repo, prState.HeadSHA)
	if err != nil {
		// Transient lookup failure: do NOT fall through to CreateTagForRepo. If a
		// tag already exists but we couldn't see it, the create call fails with
		// "Reference already exists", returns an error, and the PR stays in
		// StageReleasing forever (re-tried every poll). Return the error so this
		// PR is retried cleanly on the next poll once the lookup recovers. (TASK-316)
		return fmt.Errorf("failed to check existing tags for PR #%d: %w", prState.PRNumber, err)
	}
	if existingTag != "" {
		c.log.Info("commit already tagged, skipping release",
			"pr", prState.PRNumber,
			"sha", ShortSHA(prState.HeadSHA),
			"tag", existingTag,
		)
		c.removePR(prState.PRNumber)
		return nil
	}

	// Get current version from the target repo
	currentVersion, err := c.releaser.GetCurrentVersionForRepo(ctx, owner, repo)
	if err != nil {
		c.log.Warn("failed to get current version, defaulting to 0.0.0", "error", err)
		currentVersion = SemVer{}
	}

	// Get PR commits for bump detection (and for the release summary enrichment below).
	commits, err := c.ghClient.GetPRCommits(ctx, owner, repo, prState.PRNumber)
	if err != nil {
		return fmt.Errorf("failed to get PR commits: %w", err)
	}

	// Detect bump type using the configured version strategy. "pr_labels" derives
	// the bump from the PR's semver:* / breaking / feature / fix labels; the default
	// "conventional_commits" derives it from the commit messages.
	var bumpType BumpType
	if c.resolvedRelease().VersionStrategy == "pr_labels" {
		bumpType, err = c.detectBumpFromPRLabels(ctx, owner, repo, prState.PRNumber)
		if err != nil {
			return fmt.Errorf("failed to detect bump from PR labels: %w", err)
		}
	} else {
		bumpType = DetectBumpType(commits)
	}
	prState.ReleaseBumpType = bumpType

	if !c.releaser.ShouldRelease(bumpType) {
		c.log.Info("no release needed", "pr", prState.PRNumber, "bump", bumpType)
		c.removePR(prState.PRNumber)
		return nil
	}

	// Calculate new version
	newVersion := currentVersion.Bump(bumpType)
	rel := c.resolvedRelease()
	prState.ReleaseVersion = newVersion.String(rel.TagPrefix)

	c.log.Info("creating release",
		"pr", prState.PRNumber,
		"current", currentVersion.String(rel.TagPrefix),
		"new", prState.ReleaseVersion,
		"bump", bumpType,
	)

	// Create git tag in the correct repo
	tagName, err := c.releaser.CreateTagForRepo(ctx, owner, repo, prState, newVersion)
	if err != nil {
		// A duplicate-tag error means the commit is already released (e.g. a
		// racing PR tagged it, or our GetTagForSHA check raced the create).
		// Treat it as success so the PR drains from activePRs instead of
		// looping forever on a tag it can never re-create. (TASK-316)
		if isDuplicateTagError(err) {
			c.log.Info("tag already exists at HEAD SHA — treating as released",
				"pr", prState.PRNumber,
				"sha", ShortSHA(prState.HeadSHA),
			)
			c.removePR(prState.PRNumber)
			return nil
		}
		return fmt.Errorf("failed to create tag: %w", err)
	}

	releaseURL := fmt.Sprintf("https://github.com/%s/%s/releases/tag/%s", owner, repo, tagName)
	c.log.Info("tag created (GoReleaser will create release)",
		"pr", prState.PRNumber,
		"version", prState.ReleaseVersion,
		"tag", tagName,
	)

	// Enrich release with LLM-generated summary (best-effort, non-blocking).
	// Runs in a goroutine because it polls for GoReleaser to publish the release
	// (up to 5 min) and we don't want to block the notification or PR cleanup.
	if c.releaseSummary != nil && rel.GenerateSummary {
		logging.SafeGo("autopilot.enrichRelease", func() {
			enrichCtx, cancel := context.WithTimeout(context.Background(), releasePollTimeout+releaseSummaryTimeout)
			defer cancel()
			if err := c.releaseSummary.EnrichRelease(enrichCtx, owner, repo, tagName, commits); err != nil {
				c.log.Warn("failed to enrich release notes", "tag", tagName, "error", err)
			}
		})
	}

	// Send notification
	if rel.NotifyOnRelease && c.notifier != nil {
		if n, ok := c.notifier.(ReleaseNotifier); ok {
			if err := n.NotifyReleased(ctx, prState, releaseURL); err != nil {
				c.log.Warn("failed to send release notification", "error", err)
			}
		}
	}

	c.removePR(prState.PRNumber)
	return nil
}

// isDuplicateTagError reports whether err indicates the git tag already exists.
// GitHub returns HTTP 422 with body {"message":"Reference already exists"} when
// POSTing /git/refs for a ref that is already present. The predicate is kept
// deliberately narrow — it matches the "already exists" signal, not generic 422s
// (e.g. validation failures), so we never swallow a real release failure. (TASK-316)
func isDuplicateTagError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "already exists")
}

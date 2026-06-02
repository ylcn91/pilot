package autopilot

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ylcn91/pilot/internal/approval"
	"github.com/ylcn91/pilot/internal/memory"
)

// handleReviewRequested processes a PR that received "changes requested" review feedback.
// It fetches reviews and comments, checks iteration limits, creates a revision issue,
// learns from the review, then closes the PR and deletes the branch.
func (c *Controller) handleReviewRequested(ctx context.Context, prState *PRState) error {
	c.log.Info("handleReviewRequested: processing review feedback",
		"pr", prState.PRNumber,
	)

	// Fetch reviews and comments
	reviews, err := c.ghClient.ListPullRequestReviews(ctx, c.owner, c.repo, prState.PRNumber)
	if err != nil {
		return fmt.Errorf("failed to fetch reviews: %w", err)
	}

	comments, err := c.ghClient.GetPullRequestComments(ctx, c.owner, c.repo, prState.PRNumber)
	if err != nil {
		c.log.Warn("failed to fetch review comments", "pr", prState.PRNumber, "error", err)
		// Non-fatal: proceed with reviews only
	}

	// Check iteration limit
	iteration := 0
	if prState.IssueNumber > 0 && c.config.ReviewFeedback != nil && c.config.ReviewFeedback.MaxIterations > 0 {
		issue, err := c.ghClient.GetIssue(ctx, c.owner, c.repo, prState.IssueNumber)
		if err != nil {
			c.log.Warn("failed to fetch issue for iteration check", "issue", prState.IssueNumber, "error", err)
		} else {
			iteration = parseAutopilotIteration(issue.Body)
		}

		if iteration >= c.config.ReviewFeedback.MaxIterations {
			c.log.Warn("review feedback iteration limit reached",
				"pr", prState.PRNumber,
				"iteration", iteration,
				"max", c.config.ReviewFeedback.MaxIterations,
			)

			if err := c.ghClient.ClosePullRequest(ctx, c.owner, c.repo, prState.PRNumber); err != nil {
				c.log.Warn("failed to close PR", "pr", prState.PRNumber, "error", err)
			}

			prState.Stage = StageFailed
			prState.Error = fmt.Sprintf("review feedback iteration limit reached (%d/%d)", iteration, c.config.ReviewFeedback.MaxIterations)
			c.metrics.RecordPRFailed()
			c.metrics.RecordIssueProcessed("failed")
			return nil
		}
	}

	// Create revision issue with review feedback
	issueNum, err := c.feedbackLoop.CreateReviewIssue(ctx, prState, reviews, comments, iteration+1)
	if err != nil {
		return fmt.Errorf("failed to create review issue: %w", err)
	}

	// Learn from review (self-improvement)
	if c.learningLoop != nil && len(reviews) > 0 {
		var reviewData []*memory.ReviewData
		for _, r := range reviews {
			if r.Body == "" {
				continue
			}
			reviewData = append(reviewData, &memory.ReviewData{
				Body:     r.Body,
				State:    r.State,
				Reviewer: r.User.Login,
			})
		}
		for _, comment := range comments {
			reviewData = append(reviewData, &memory.ReviewData{
				Body:     comment.Body,
				State:    "COMMENTED",
				Reviewer: comment.User.Login,
			})
		}
		if len(reviewData) > 0 {
			projectPath := c.owner + "/" + c.repo
			if learnErr := c.learningLoop.LearnFromReview(ctx, projectPath, reviewData, prState.PRURL); learnErr != nil {
				c.log.Warn("Failed to learn from review feedback", slog.Any("error", learnErr))
			}
		}
	}

	// Notify fix issue created
	if c.notifier != nil {
		if err := c.notifier.NotifyFixIssueCreated(ctx, prState, issueNum); err != nil {
			c.log.Warn("failed to send review issue notification", "error", err)
		}
	}

	c.log.Info("created revision issue for review feedback", "pr", prState.PRNumber, "issue", issueNum)

	// Close the PR and delete the branch
	if err := c.ghClient.ClosePullRequest(ctx, c.owner, c.repo, prState.PRNumber); err != nil {
		c.log.Warn("failed to close PR after review", "pr", prState.PRNumber, "error", err)
	}

	if prState.BranchName != "" {
		if err := c.ghClient.DeleteBranch(ctx, c.owner, c.repo, prState.BranchName); err != nil {
			c.log.Debug("branch cleanup after review", "branch", prState.BranchName, "error", err)
		}
	}

	prState.Stage = StageFailed
	c.metrics.RecordPRFailed()
	return nil
}

// hasChangesRequested checks if a PR has unresolved "changes requested" reviews.
// It filters out bot reviews and only considers reviews submitted after the PR was created.
func (c *Controller) hasChangesRequested(ctx context.Context, prState *PRState) bool {
	reviews, err := c.ghClient.ListPullRequestReviews(ctx, c.owner, c.repo, prState.PRNumber)
	if err != nil {
		c.log.Warn("failed to fetch reviews for changes_requested check", "pr", prState.PRNumber, "error", err)
		return false
	}

	// Track latest review state per user (only non-bot users)
	latestState := make(map[string]string)
	for _, r := range reviews {
		// Skip bot reviews (self-review)
		if strings.Contains(r.User.Login, "[bot]") || strings.HasSuffix(r.User.Login, "-bot") {
			continue
		}

		// Only consider reviews submitted after the PR entered tracking
		if r.SubmittedAt != "" && !prState.CreatedAt.IsZero() {
			submittedAt, err := time.Parse(time.RFC3339, r.SubmittedAt)
			if err == nil && submittedAt.Before(prState.CreatedAt) {
				continue
			}
		}

		latestState[r.User.Login] = r.State
	}

	for _, state := range latestState {
		if state == "CHANGES_REQUESTED" {
			return true
		}
	}

	return false
}

// handleAwaitApproval is a non-blocking tick handler for StageAwaitApproval.
//
// Tick 1 (no ApprovalRequestID): submits the request via SubmitApprovalRequest,
// persists the returned ID + ApprovalRequestedAt, stays in StageAwaitApproval.
//
// Tick N with decision recorded: advances to StageMerging (approved) or
// StageFailed (rejected/timeout).
//
// Tick N with no decision: checks wall-clock expiry against the stage timeout and
// applies default_action when expired (belt-and-suspenders for post-restart cases).
func (c *Controller) handleAwaitApproval(ctx context.Context, prState *PRState) error {
	// Path 1: submit request on first tick.
	if prState.ApprovalRequestID == "" {
		return c.submitAsyncApprovalRequest(ctx, prState)
	}

	// Path 2: decision already recorded — advance the state machine.
	if prState.ApprovalDecision != "" {
		return c.applyApprovalDecision(prState)
	}

	// Path 3: still waiting — check wall-clock expiry as a guard for post-restart
	// cases where the background goroutine in SubmitApprovalRequest is gone.
	timeout := c.approvalMgr.PreMergeTimeout()
	if !prState.ApprovalRequestedAt.IsZero() && time.Since(prState.ApprovalRequestedAt) > timeout {
		defaultAction := c.approvalMgr.PreMergeDefaultAction()
		c.log.Warn("approval request expired in controller (post-restart guard)",
			"pr", prState.PRNumber,
			"request_id", prState.ApprovalRequestID,
			"elapsed", time.Since(prState.ApprovalRequestedAt).Round(time.Second),
			"default_action", defaultAction)
		prState.ApprovalDecision = string(defaultAction)
		return c.applyApprovalDecision(prState)
	}

	// Still waiting for user input — stay in StageAwaitApproval.
	return nil
}

// submitAsyncApprovalRequest submits the first async approval request for a PR.
func (c *Controller) submitAsyncApprovalRequest(ctx context.Context, prState *PRState) error {
	// Fail closed: if approval stage is not enabled, do NOT auto-approve when the env requires approval.
	if c.approvalMgr == nil || !c.approvalMgr.IsStageEnabled(approval.StagePreMerge) {
		c.log.Error("approval misconfig: env requires approval but pre_merge.enabled=false",
			"pr", prState.PRNumber, "env", c.config.EnvironmentName())
		prState.Stage = StageFailed
		prState.Error = fmt.Sprintf(
			"approval-misconfig: env %q has require_approval=true but approval.pre_merge.enabled=false → deadlock until config fixed",
			c.config.EnvironmentName(),
		)
		c.autoMerger.postMisconfigComment(ctx, prState)
		c.metrics.RecordPRFailed()
		c.metrics.RecordIssueProcessed("failed")
		return nil
	}

	taskID := fmt.Sprintf("GH-%d", prState.IssueNumber)
	if prState.IssueNumber == 0 {
		taskID = fmt.Sprintf("PR-%d", prState.PRNumber)
	}
	req := &approval.Request{
		ID:     fmt.Sprintf("pr-%d-%d", prState.PRNumber, time.Now().UnixNano()),
		TaskID: taskID,
		Stage:  approval.StagePreMerge,
		Title:  fmt.Sprintf("Merge approval for PR #%d", prState.PRNumber),
		Metadata: map[string]interface{}{
			"pr_url":    prState.PRURL,
			"pr_title":  prState.PRTitle,
			"pr_number": prState.PRNumber,
		},
	}

	requestID, err := c.approvalMgr.SubmitApprovalRequest(ctx, req)
	if err != nil {
		return fmt.Errorf("submit approval request for PR %d: %w", prState.PRNumber, err)
	}

	prState.ApprovalRequestID = requestID
	prState.ApprovalRequestedAt = time.Now()
	// Stage intentionally stays at StageAwaitApproval.

	if c.stateStore != nil {
		if serr := c.stateStore.SavePRState(prState); serr != nil {
			c.log.Warn("failed to persist approval request state", "pr", prState.PRNumber, "error", serr)
		}
	}

	if c.memoryStore != nil {
		if merr := c.memoryStore.SetApprovalRequestID(ctx, taskID, requestID); merr != nil {
			c.log.Warn("failed to persist approval_request_id to executions",
				"pr", prState.PRNumber, "task_id", taskID, "request_id", requestID,
				"op", "SetApprovalRequestID", "error", merr)
			if errors.Is(merr, sql.ErrNoRows) {
				c.metrics.RecordApprovalPersistMiss("request_id")
			}
		}
	}

	c.log.Info("async approval request submitted",
		"pr", prState.PRNumber, "request_id", requestID)
	return nil
}

// applyApprovalDecision advances the state machine based on the recorded decision.
func (c *Controller) applyApprovalDecision(prState *PRState) error {
	switch approval.Decision(prState.ApprovalDecision) {
	case approval.DecisionApproved:
		c.log.Info("approval granted — advancing to merging stage", "pr", prState.PRNumber)
		prState.Stage = StageMerging
	case approval.DecisionRejected, approval.DecisionTimeout:
		c.log.Info("approval not granted — failing PR",
			"pr", prState.PRNumber, "decision", prState.ApprovalDecision)
		prState.Stage = StageFailed
		prState.Error = fmt.Sprintf("merge rejected: approval %s", prState.ApprovalDecision)
		c.metrics.RecordPRFailed()
		c.metrics.RecordIssueProcessed("failed")
	default:
		c.log.Warn("unknown approval decision — failing PR",
			"pr", prState.PRNumber, "decision", prState.ApprovalDecision)
		prState.Stage = StageFailed
		prState.Error = fmt.Sprintf("unknown approval decision: %q", prState.ApprovalDecision)
		c.metrics.RecordPRFailed()
		c.metrics.RecordIssueProcessed("failed")
	}
	return nil
}

// SetApprovalDecision implements approval.PRStateWriter. It finds the in-memory
// PRState whose ApprovalRequestID matches and records the decision, then persists
// via stateStore. Called by the approval.Manager's background goroutine when a
// handler fires (e.g. Telegram button tap).
func (c *Controller) SetApprovalDecision(ctx context.Context, requestID string, decision string, by string) error {
	if requestID == "" {
		return nil
	}

	// TASK-324: collect the live pointers under c.mu, then RELEASE c.mu before taking
	// any prState.mu (no-deadlock invariant: prState.mu before c.mu, never reverse).
	// ApprovalRequestID is written under prState.mu (submitAsyncApprovalRequest), so we
	// also read it under prState.mu to find the match.
	c.mu.RLock()
	live := make([]*PRState, 0, len(c.activePRs))
	for _, pr := range c.activePRs {
		live = append(live, pr)
	}
	c.mu.RUnlock()

	for _, pr := range live {
		pr.mu.Lock()
		if pr.ApprovalRequestID != requestID {
			pr.mu.Unlock()
			continue
		}
		pr.ApprovalDecision = decision
		if c.stateStore != nil {
			_ = c.stateStore.SavePRState(pr)
		}
		prNumber := pr.PRNumber
		issueNumber := pr.IssueNumber
		pr.mu.Unlock()

		// memoryStore persistence is keyed by requestID, not by the live PRState
		// fields, so it is safe (and preferable) to run it outside prState.mu.
		if c.memoryStore != nil {
			if merr := c.memoryStore.SetApprovalDecision(ctx, requestID, decision, by); merr != nil {
				taskIDStr := fmt.Sprintf("GH-%d", issueNumber)
				if errors.Is(merr, sql.ErrNoRows) {
					c.log.Warn("failed to persist approval decision to executions (no matching row)",
						"pr", prNumber, "task_id", taskIDStr, "request_id", requestID,
						"op", "SetApprovalDecision", "decision", decision, "error", merr)
					c.metrics.RecordApprovalPersistMiss("decision")
				} else {
					c.log.Warn("failed to persist approval decision to executions",
						"pr", prNumber, "task_id", taskIDStr, "request_id", requestID,
						"op", "SetApprovalDecision", "decision", decision, "error", merr)
				}
			}
		}
		c.log.Info("approval decision applied to PR state",
			"pr", prNumber, "request_id", requestID,
			"decision", decision, "by", by)
		return nil
	}
	// requestID not found in this controller — normal in multi-repo deployments.
	return nil
}

// MultiControllerStateWriter routes approval decisions to whichever controller
// owns the matching ApprovalRequestID. Use this when multiple controllers share
// a single approval.Manager (multi-repo deployments).
type MultiControllerStateWriter struct {
	controllers []*Controller
}

// NewMultiControllerStateWriter creates a writer that delegates SetApprovalDecision
// to each controller in order, stopping at the first match.
func NewMultiControllerStateWriter(controllers ...*Controller) *MultiControllerStateWriter {
	return &MultiControllerStateWriter{controllers: controllers}
}

// SetApprovalDecision implements approval.PRStateWriter by trying each controller.
func (w *MultiControllerStateWriter) SetApprovalDecision(ctx context.Context, requestID string, decision string, by string) error {
	for _, c := range w.controllers {
		if err := c.SetApprovalDecision(ctx, requestID, decision, by); err != nil {
			return err
		}
	}
	return nil
}

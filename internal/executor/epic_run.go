package executor

import (
	"context"
	"fmt"
	"strconv"
)

// ExecuteSubIssues executes created sub-issues sequentially and tracks progress on the parent.
// Each sub-issue is executed as a separate task, and the parent issue is updated with progress.
// Returns an error if any sub-issue fails; completed sub-issues remain done.
// executionPath may differ from task.ProjectPath when using worktree isolation (GH-968).
// GH-2177: repoPath is the real repository path (not a worktree). Sub-issues need this
// as their ProjectPath so they can create their own branches from the real repo.
// executionPath is still used for gh CLI commands (issue comments) that need worktree context.
func (r *Runner) ExecuteSubIssues(ctx context.Context, parent *Task, issues []CreatedIssue, executionPath string, repoPath string) error {
	if len(issues) == 0 {
		return fmt.Errorf("no sub-issues to execute")
	}

	total := len(issues)
	// Use executionPath for gh CLI commands (respects worktree isolation)
	projectPath := executionPath
	if projectPath == "" && parent != nil {
		projectPath = parent.ProjectPath
	}

	// GH-2177: Use repoPath for sub-task ProjectPath so each sub-issue branches
	// from the real repo, not the parent's worktree. Fall back to projectPath
	// for backwards compatibility (non-worktree mode).
	subTaskRepoPath := repoPath
	if subTaskRepoPath == "" {
		subTaskRepoPath = projectPath
	}

	r.log.Info("Starting sequential sub-issue execution",
		"parent_id", parent.ID,
		"total_issues", total,
	)

	// Update parent with start message
	startMsg := fmt.Sprintf("🚀 Starting sequential execution of %d sub-issues", total)
	if err := r.UpdateIssueProgress(ctx, projectPath, parent.ID, startMsg); err != nil {
		r.log.Warn("Failed to update parent progress", "error", err)
		// Non-fatal, continue execution
	}

	for i, issue := range issues {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return fmt.Errorf("execution cancelled: %w", ctx.Err())
		default:
		}

		// GH-1471: Determine issue reference and task ID format
		// For GitHub issues (Number > 0): use "GH-N" format for backwards compatibility
		// For non-GitHub adapters (Number == 0): use Identifier directly (e.g., "APP-123")
		var issueRef string
		var taskID string
		if issue.Number > 0 {
			// GitHub issue: use "GH-N" format
			taskID = fmt.Sprintf("GH-%d", issue.Number)
			issueRef = strconv.Itoa(issue.Number)
		} else if issue.Identifier != "" {
			// Non-GitHub adapter: use Identifier directly
			taskID = issue.Identifier
			issueRef = issue.Identifier
		} else {
			// Fallback (shouldn't happen)
			taskID = "unknown"
			issueRef = "unknown"
		}

		// Update parent with current progress
		progressMsg := fmt.Sprintf("⏳ Progress: %d/%d - Starting: **%s** (%s)",
			i, total, issue.Subtask.Title, issueRef)
		if err := r.UpdateIssueProgress(ctx, projectPath, parent.ID, progressMsg); err != nil {
			r.log.Warn("Failed to update parent progress", "error", err)
		}

		// Create task from sub-issue
		// GH-2177: Use real repo path so sub-issues can create branches from main,
		// not from inside the parent's worktree (which locks the branch).
		subTask := &Task{
			ID:          taskID,
			Title:       issue.Subtask.Title,
			Description: issue.Subtask.Description,
			ProjectPath: subTaskRepoPath,
			Branch:      fmt.Sprintf("pilot/%s", taskID),
			CreatePR:    true,
		}

		r.log.Info("Executing sub-issue",
			"parent_id", parent.ID,
			"sub_issue", issueRef,
			"order", i+1,
			"total", total,
		)

		// Execute the sub-task (use override if set, for testing)
		var result *ExecutionResult
		var err error
		if r.executeFunc != nil {
			// Use test override function if set
			result, err = r.executeFunc(ctx, subTask)
		} else {
			// GH-2178: Enable worktree isolation for sub-issues. Each sub-issue creates
			// its own worktree from the real repo (safe after GH-2177 set ProjectPath = repoPath).
			// Previously false (GH-948) to prevent nested worktrees, but GH-2177 ensured
			// subTask.ProjectPath points to the real repo, not the parent's worktree.
			result, err = r.executeWithOptions(ctx, subTask, true)
		}
		if err != nil {
			failMsg := fmt.Sprintf("❌ Failed on %d/%d: %s - Error: %v",
				i+1, total, issue.Subtask.Title, err)
			_ = r.UpdateIssueProgress(ctx, projectPath, parent.ID, failMsg)
			return fmt.Errorf("sub-issue %s failed: %w", issueRef, err)
		}

		if !result.Success {
			failMsg := fmt.Sprintf("❌ Failed on %d/%d: %s - %s",
				i+1, total, issue.Subtask.Title, result.Error)
			_ = r.UpdateIssueProgress(ctx, projectPath, parent.ID, failMsg)
			return fmt.Errorf("sub-issue %s failed: %s", issueRef, result.Error)
		}

		// TASK-356 #1: work-loss guard. A sub-issue runs with CreatePR=true, so a
		// successful child MUST yield a PR. When it instead reports success with real
		// commits (CommitSHA set) but no PR (empty PRUrl), its work is stranded in a
		// worktree that cleanup will discard — the failure mode that silently lost 26
		// minutes of a real port (studio-sdk #17). Refuse to close the issue or report
		// epic success: fail loud so the child issue stays OPEN for recovery/retry
		// instead of the work vanishing behind a false "completed" record.
		if result.PRUrl == "" && result.CommitSHA != "" {
			shortSHA := result.CommitSHA[:min(7, len(result.CommitSHA))]
			warnMsg := fmt.Sprintf("⚠️ %d/%d produced commits (%s) but no PR — work not delivered; leaving issue open for retry: %s",
				i+1, total, shortSHA, issue.Subtask.Title)
			_ = r.UpdateIssueProgress(ctx, projectPath, parent.ID, warnMsg)
			r.log.Error("sub-issue committed work but produced no PR — refusing to discard",
				"parent_id", parent.ID,
				"sub_issue", issueRef,
				"commit_sha", shortSHA,
				"branch", subTask.Branch,
			)
			return fmt.Errorf("sub-issue %s committed work (sha %s) but produced no PR — work would be lost, halting epic", issueRef, shortSHA)
		}

		// Register sub-issue PR with autopilot controller (GH-596)
		// Note: PR callback uses int issueNumber for GitHub compatibility
		if result.PRUrl != "" && r.onSubIssuePRCreated != nil {
			if prNum := parsePRNumberFromURL(result.PRUrl); prNum > 0 {
				r.onSubIssuePRCreated(prNum, result.PRUrl, issue.Number, result.CommitSHA, subTask.Branch, "")
			} else {
				r.log.Warn("Failed to extract PR number from sub-issue PR URL",
					"pr_url", result.PRUrl)
			}
		}

		// GH-2178: Wait for the sub-issue PR to merge before starting the next one.
		// Skip for the last sub-issue (no next issue to sequence).
		// Nil check degrades gracefully — if not wired, execution proceeds without waiting.
		if r.subIssueMergeWait != nil && result.PRUrl != "" && i < total-1 {
			prNum := parsePRNumberFromURL(result.PRUrl)
			if prNum > 0 {
				waitMsg := fmt.Sprintf("⏳ Waiting for PR #%d to merge before starting next sub-issue (%d/%d)...",
					prNum, i+1, total)
				_ = r.UpdateIssueProgress(ctx, projectPath, parent.ID, waitMsg)

				r.log.Info("Waiting for sub-issue PR to merge",
					"parent_id", parent.ID,
					"sub_issue", issueRef,
					"pr_number", prNum,
					"order", i+1,
					"total", total,
				)

				if err := r.subIssueMergeWait(ctx, prNum); err != nil {
					failMsg := fmt.Sprintf("❌ Merge wait failed for %s (PR #%d): %v", issueRef, prNum, err)
					_ = r.UpdateIssueProgress(ctx, projectPath, parent.ID, failMsg)
					return fmt.Errorf("merge wait failed for sub-issue %s (PR #%d): %w", issueRef, prNum, err)
				}

				// Sync local main branch so the next sub-issue branches from the merged state.
				if syncErr := r.syncMainBranch(ctx, subTaskRepoPath); syncErr != nil {
					r.log.Warn("Failed to sync main branch after sub-issue merge",
						"sub_issue", issueRef,
						"error", syncErr,
					)
					// Non-fatal: next sub-issue will fetch from origin anyway.
				}
			}
		}

		// Close completed sub-issue
		// GH-1471: Use Identifier for issue reference in close command
		closeComment := fmt.Sprintf("✅ Completed as part of %s", parent.ID)
		if result.PRUrl != "" {
			closeComment = fmt.Sprintf("✅ Completed as part of %s\nPR: %s", parent.ID, result.PRUrl)
		}
		if err := r.CloseIssueWithComment(ctx, projectPath, issueRef, closeComment); err != nil {
			r.log.Warn("Failed to close sub-issue", "issue", issueRef, "error", err)
			// Non-fatal, continue
		}

		r.log.Info("Sub-issue completed",
			"parent_id", parent.ID,
			"sub_issue", issueRef,
			"pr_url", result.PRUrl,
		)
	}

	// All done - update and close parent
	completeMsg := fmt.Sprintf("✅ Completed: %d/%d sub-issues done\n\nAll sub-tasks executed successfully.", total, total)
	_ = r.UpdateIssueProgress(ctx, projectPath, parent.ID, completeMsg)

	if err := r.CloseIssueWithComment(ctx, projectPath, parent.ID, "All sub-issues completed successfully."); err != nil {
		r.log.Warn("Failed to close parent issue", "error", err)
		// Non-fatal
	}

	r.log.Info("Epic execution completed",
		"parent_id", parent.ID,
		"total_completed", total,
	)

	return nil
}

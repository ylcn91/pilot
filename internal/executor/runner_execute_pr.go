package executor

import (
	"fmt"
	"log/slog"
	"strings"
)

// executeLintPushPR runs the pre-push lint gate then handles direct-commit
// push-to-main or branch push + PR/MR creation (original lines ~2122-2345). It
// returns a non-nil result/error on any push/PR abort path; otherwise (nil, nil)
// to continue.
func (r *Runner) executeLintPushPR(s *executeState) (*ExecutionResult, error) {
	task := s.task
	ctx := s.ctx
	log := s.log
	git := s.git
	executionPath := s.executionPath
	result := s.result
	backendResult := s.backendResult
	recorder := s.recorder

	// Handle direct commit mode: push directly to main

	if task.DirectCommit {
		// Pre-push lint gate (GH-1376)
		if r.config != nil && r.config.PrePushLint != nil && *r.config.PrePushLint {
			r.reportProgress(task.ID, "Linting", 95, "Running pre-push lint check...")
			lintResult := git.autoFixLint(ctx)
			if !lintResult.Clean && !lintResult.FixedAll {
				// Include unfixable lint issues in execution result for self-review
				if len(lintResult.Issues) > 0 {
					result.IntentWarning = "Lint issues detected but not auto-fixable:\n" + strings.Join(lintResult.Issues, "\n")
				}
			}
		}
		r.reportProgress(task.ID, "Pushing", 96, "Pushing to main...")

		if err := git.PushToMain(ctx); err != nil {
			result.Success = false
			result.Error = fmt.Sprintf("push to main failed: %v", err)
			r.reportProgress(task.ID, "Push Failed", 100, result.Error)
			return result, nil
		}

		// Get commit SHA for result
		sha, _ := git.GetCurrentCommitSHA(ctx)
		if sha != "" {
			result.CommitSHA = sha
		}

		log.Info("Direct commit pushed to main",
			slog.String("task_id", task.ID),
			slog.String("commit_sha", result.CommitSHA),
		)
		r.reportProgress(task.ID, "Completed", 100, "Pushed directly to main")
	} else if task.CreatePR && task.Branch != "" {
		// Create PR if requested and we have commits
		r.reportProgress(task.ID, "Creating PR", 96, "Pushing branch...")

		// Determine base branch before the no-commits guard.
		baseBranch := task.BaseBranch
		if baseBranch == "" {
			baseBranch, _ = git.GetDefaultBranch(ctx)
			if baseBranch == "" {
				baseBranch = "main"
			}
		}

		// GH-2743: pre-CreatePR no-commits guard.
		// The no-commit check at ~line 2151 only runs when result.Success==true.
		// If the initial execution fails, that check is bypassed and gh pr create
		// receives an empty branch, producing "No commits between main and <branch>".
		if guardCount, _ := git.CountNewCommits(ctx, baseBranch); guardCount == 0 {
			result.Success = false
			result.Error = "no_changes: branch has no commits relative to base (PR guard)"
			if backendResult != nil {
				backendResult.ErrorType = string(ErrorTypeNoChanges)
			}
			r.reportProgress(task.ID, "PR Failed", 100, result.Error)
			return result, nil
		}

		// Pre-push lint gate (GH-1376)
		if r.config != nil && r.config.PrePushLint != nil && *r.config.PrePushLint {
			r.reportProgress(task.ID, "Linting", 95, "Running pre-push lint check...")
			lintResult := git.autoFixLint(ctx)
			if !lintResult.Clean && !lintResult.FixedAll {
				// Include unfixable lint issues in execution result for self-review
				if len(lintResult.Issues) > 0 {
					result.IntentWarning = "Lint issues detected but not auto-fixable:\n" + strings.Join(lintResult.Issues, "\n")
				}
			}
		}
		// Push branch
		if err := git.Push(ctx, task.Branch); err != nil {
			// GH-1389: Worktree push may fail with chdir error even if data was already pushed.
			// Check if branch exists on remote before declaring failure.
			if git.RemoteBranchExists(ctx, task.Branch) {
				log.Warn("Push reported error but branch exists on remote, continuing",
					slog.Any("error", err),
					slog.String("branch", task.Branch),
				)
			} else {
				result.Success = false
				result.Error = fmt.Sprintf("push failed: %v", err)
				r.reportProgress(task.ID, "PR Failed", 100, result.Error)
				return result, nil
			}
		}

		// GH-457: Use actual pushed HEAD as CommitSHA source of truth.
		// Self-review or quality retries may push new commits after
		// result.CommitSHA was captured, causing autopilot to check CI
		// against a stale SHA.
		if pushedSHA, pushErr := git.GetCurrentCommitSHA(ctx); pushErr == nil && pushedSHA != "" {
			if result.CommitSHA != "" && result.CommitSHA != pushedSHA {
				log.Info("CommitSHA updated after push (post-execution commits detected)",
					slog.String("task_id", task.ID),
					slog.String("old_sha", result.CommitSHA[:min(7, len(result.CommitSHA))]),
					slog.String("pushed_sha", pushedSHA[:min(7, len(pushedSHA))]),
				)
			}
			result.CommitSHA = pushedSHA
		} else if pushErr != nil {
			log.Warn("Failed to get pushed HEAD SHA, using tracked SHA",
				slog.String("task_id", task.ID),
				slog.Any("error", pushErr),
			)
		}

		// GH-3126: Defense-in-depth ghost-SHA guard after push.
		// The pre-push guard above clears parent SHAs, but re-check here since
		// post-push SHA is the authoritative value that flows into autopilot CI polling.
		// Fail open on errors (missing origin ref) — only block on conclusive stale result.
		if result.CommitSHA != "" {
			if isNew, checkErr := commitSHAIsNew(ctx, executionPath, result.CommitSHA, baseBranch); checkErr != nil {
				log.Warn("executor: post-push ghost-SHA check skipped (will not block)",
					slog.String("task_id", task.ID),
					slog.String("sha", result.CommitSHA[:min(7, len(result.CommitSHA))]),
					slog.Any("error", checkErr),
				)
			} else if !isNew {
				log.Warn("executor: post-push SHA is already on base branch — aborting PR creation",
					slog.String("task_id", task.ID),
					slog.String("sha", result.CommitSHA[:min(7, len(result.CommitSHA))]),
					slog.String("base", baseBranch),
				)
				result.CommitSHA = ""
				result.Success = false
				result.Error = "no new commit produced — post-push SHA matches base branch"
				r.reportProgress(task.ID, "PR Failed", 100, result.Error)
				return result, nil
			}
		}

		r.reportProgress(task.ID, "Creating PR", 98, "Creating pull request...")

		// GH-2325: ensure the subject passed through to the PR (and the squash
		// commit on main) is a conventional commit. Falls back to a
		// label-derived prefix, then diff heuristic (GH-2735).
		diffStats, _ := git.GetDiffStats(ctx, baseBranch)
		normalizedTitle, titleErr := normalizeTitle(task.Title, task.Labels, diffStats)
		if titleErr != nil {
			result.Success = false
			result.Error = fmt.Sprintf("PR creation refused: %v", titleErr)
			log.Warn("PR creation refused: non-conventional title",
				slog.String("task_id", task.ID),
				slog.String("title", task.Title),
				slog.Any("labels", task.Labels),
			)

			// GH-2363: On the 2nd consecutive rejection for this exact title,
			// escalate with a structured comment + stop-retry labels so we
			// don't spam the same failure every retry cycle.
			if r.titleRejections != nil {
				count := r.titleRejections.record(task.ID, task.Title)
				if count >= titleRejectionMaxCount {
					if err := r.postTitleRejectionEscalation(ctx, task); err != nil {
						log.Warn("title-rejection escalation failed",
							slog.String("task_id", task.ID),
							slog.Any("error", err),
						)
					} else {
						result.TitleRejected = true
						log.Info("title-rejection escalated — posted guidance comment, stopping retries",
							slog.String("task_id", task.ID),
							slog.Int("count", count),
						)
					}
				}
			}

			r.reportProgress(task.ID, "PR Failed", 100, result.Error)
			return result, nil
		}
		// Title accepted — clear any prior rejection bookkeeping for this task.
		if r.titleRejections != nil {
			r.titleRejections.clear(task.ID)
		}
		prTitle := fmt.Sprintf("%s: %s", task.ID, normalizedTitle)

		// Route PR/MR creation through adapter-specific creator when available
		var prURL string
		if r.prCreator != nil && task.SourceAdapter != "" && task.SourceAdapter != "github" {
			// Non-GitHub adapter: use PRCreator (e.g., GitLab MR API)
			// Include "Closes #N" keyword so GitLab auto-closes the source issue on merge
			closeKeyword := ""
			if task.SourceIssueID != "" {
				closeKeyword = fmt.Sprintf("\n\nCloses #%s", task.SourceIssueID)
			}
			prBody := fmt.Sprintf("## Summary\n\nAutomated MR created by Pilot for task %s.%s\n\n## Changes\n\n%s", task.ID, closeKeyword, task.Description)
			var createErr error
			prURL, createErr = r.prCreator.CreatePR(ctx, task.Branch, baseBranch, prTitle, prBody)
			if createErr != nil {
				result.Success = false
				result.Error = fmt.Sprintf("MR creation failed: %v", createErr)
				r.reportProgress(task.ID, "MR Failed", 100, result.Error)
				return result, nil
			}
		} else {
			// GitHub: use gh CLI with auto-close keyword
			issueNum := strings.TrimPrefix(task.ID, "GH-")
			prBody := fmt.Sprintf("## Summary\n\nAutomated PR created by Pilot for task %s.\n\nCloses #%s\n\n## Changes\n\n%s", task.ID, issueNum, task.Description)
			var createErr error
			prURL, createErr = git.CreatePR(ctx, prTitle, prBody, baseBranch)
			if createErr != nil {
				result.Success = false
				result.Error = fmt.Sprintf("PR creation failed: %v", createErr)
				r.reportProgress(task.ID, "PR Failed", 100, result.Error)
				return result, nil
			}
		}

		result.PRUrl = prURL
		log.Info("Pull request created", slog.String("pr_url", prURL))
		r.reportProgress(task.ID, "Completed", 100, fmt.Sprintf("PR created: %s", prURL))
		r.saveLogEntry(task.ID, "info", "PR created: "+prURL)

		// Update recording with PR info
		if recorder != nil {
			recorder.SetPRUrl(prURL)
		}
	} else {
		r.reportProgress(task.ID, "Completed", 100, "Task completed successfully")
	}

	return nil, nil
}

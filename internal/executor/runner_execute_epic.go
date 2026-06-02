package executor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// executeEpic handles epic detection / planning mode and task decomposition
// (original lines ~201-454). It returns a non-nil result/error when the epic or
// decomposition path terminates execution; otherwise it returns (nil, nil) and
// execution falls through to normal single-task execution.
func (r *Runner) executeEpic(s *executeState) (*ExecutionResult, error) {
	task := s.task
	ctx := s.ctx
	executionPath := s.executionPath
	start := s.start
	complexity := s.complexity

	// Opt-in pipeline PLAN stage (independent of complexity.IsEpic()). Captures a
	// spec into s.planOutput, which executePrepare injects into the execute
	// prompt. No-op unless config.Pipeline.Plan is configured.
	r.executePipelinePlan(s)

	// GH-664: Skip epic mode if task has no-decompose label
	// GH-1687: Also skip if task title or description contains [no-plan] keyword
	hasNoDecompose := false
	for _, label := range task.Labels {
		if strings.EqualFold(label, NoDecomposeLabel) {
			hasNoDecompose = true
			break
		}
	}
	if !hasNoDecompose && HasNoPlanKeyword(task) {
		hasNoDecompose = true
	}
	if !hasNoDecompose && HasNoDecomposePhrase(task) {
		hasNoDecompose = true
	}

	// GH-1588: Diagnostic logging for epic detection
	r.log.Info("Epic detection check",
		slog.String("task_id", task.ID),
		slog.String("task_title", task.Title),
		slog.Any("labels", task.Labels),
		slog.Bool("has_no_decompose", hasNoDecompose),
		slog.Bool("is_epic", complexity.IsEpic()),
		slog.String("complexity", string(complexity)),
	)

	// GH-405: Epic tasks trigger planning mode instead of execution
	if complexity.IsEpic() && !hasNoDecompose {
		r.log.Info("Epic task detected, running planning mode",
			slog.String("task_id", task.ID),
			slog.String("title", task.Title),
		)
		r.reportProgress(task.ID, "Planning", 10, "Running epic planning...")

		planFn := r.planEpicFn
		if planFn == nil {
			planFn = r.PlanEpic
		}
		plan, err := planFn(ctx, task, executionPath)
		if err != nil {
			// GH-1687: Planning failure is non-fatal — fall through to direct execution
			r.log.Warn("Epic planning failed, falling back to direct execution",
				slog.String("task_id", task.ID),
				slog.Any("error", err),
			)
			r.reportProgress(task.ID, "Planning", 15, "Planning failed, falling back to direct execution...")
			// Fall through to normal execution below
		} else {
			r.reportProgress(task.ID, "Planning", 30, fmt.Sprintf("Epic planned: %d subtasks", len(plan.Subtasks)))

			// GH-1265: Detect single-package scope — if all subtasks target the same
			// directory/package, consolidate into a single task instead of creating
			// separate GitHub issues. Creating N sub-issues that all touch the same
			// package causes merge conflicts because each sub-issue branches from main
			// independently and redeclares shared types (e.g., the "pilot onboard" cascade).
			if isSinglePackageScope(plan.Subtasks, task.Description) {
				r.log.Info("Single-package scope detected, skipping epic decomposition — executing as single task",
					slog.String("task_id", task.ID),
					slog.Int("planned_subtasks", len(plan.Subtasks)),
				)
				r.reportProgress(task.ID, "Planning", 35, "Single-package scope detected, running as single task...")

				// Enrich the task description with the planned steps so the executor
				// has the full implementation plan but executes it as one unit.
				task.Description = consolidateEpicPlan(task.Description, plan.Subtasks)

				// Fall through to normal execution below (past epic and decomposer blocks)
			} else {
				if !r.issueCreationEnabled {
					r.log.Info("Issue creation disabled, executing planned epic as single task",
						slog.String("task_id", task.ID),
						slog.Int("planned_subtasks", len(plan.Subtasks)),
					)
					r.reportProgress(task.ID, "Planning", 35, "Issue creation disabled, running planned epic as single task...")
					task.Description = consolidateEpicPlan(task.Description, plan.Subtasks)
				} else {
					// Multi-package epic: safe to create separate GitHub issues

					// GH-412: Create sub-issues from the plan
					r.reportProgress(task.ID, "Creating Issues", 40, "Creating GitHub sub-issues...")

					issues, err := r.CreateSubIssues(ctx, plan, executionPath)
					if err != nil {
						// GH-2883: Recover existing sub-issues instead of failing hard when they
						// were already created by a prior run (e.g., Pilot restarted mid-epic).
						if errors.Is(err, ErrSubIssuesAlreadyExist) {
							r.log.Info("Sub-issues already exist, attempting recovery",
								slog.String("task_id", task.ID),
								slog.String("parent_id", plan.ParentTask.ID),
							)
							recover := r.recoverSubIssuesFn
							if recover == nil {
								// GH-3411: pin recovery's `gh issue list` to the validated
								// origin repo. Empty slug (unresolvable remote) falls back to
								// ambient gh behavior, matching the pre-fix read path.
								repoSlug := ""
								if owner, repo, rerr := resolveGitRemote(ctx, executionPath); rerr == nil {
									repoSlug = ghRepoSlug(owner, repo)
								}
								recover = func(ctx context.Context, dir, parentID string) ([]CreatedIssue, error) {
									return recoverExistingSubIssues(ctx, dir, repoSlug, parentID)
								}
							}
							recovered, _ := recover(ctx, executionPath, plan.ParentTask.ID)
							if allChildrenDone(recovered) {
								r.log.Info("All recovered sub-issues are done, treating epic as complete",
									slog.String("task_id", task.ID),
									slog.Int("recovered_count", len(recovered)),
								)
								r.reportProgress(task.ID, "Complete", 100, "All sub-issues already completed")
								return &ExecutionResult{
									TaskID:    task.ID,
									Success:   true,
									Output:    fmt.Sprintf("Epic already completed: %d sub-issues recovered", len(recovered)),
									Duration:  time.Since(start),
									IsEpic:    true,
									EpicPlan:  plan,
									ModelName: r.fallbackModelName(),
								}, nil
							}
							// Filter to open children only and continue execution.
							var open []CreatedIssue
							for _, iss := range recovered {
								if strings.ToLower(iss.State) == "open" {
									open = append(open, iss)
								}
							}
							r.log.Info("Executing recovered open sub-issues",
								slog.String("task_id", task.ID),
								slog.Int("open_count", len(open)),
							)
							issues = open
						} else {
							return &ExecutionResult{
								TaskID:   task.ID,
								Success:  false,
								Error:    fmt.Sprintf("failed to create sub-issues: %v", err),
								Duration: time.Since(start),
								IsEpic:   true,
								EpicPlan: plan,
							}, nil
						}
					}

					r.reportProgress(task.ID, "Executing", 50, fmt.Sprintf("Executing %d sub-issues sequentially...", len(issues)))

					// GH-412: Execute sub-issues sequentially
					// GH-2177: Pass task.ProjectPath as repoPath so sub-issues branch from
					// the real repo, not the parent's worktree path.
					if err := r.ExecuteSubIssues(ctx, task, issues, executionPath, task.ProjectPath); err != nil {
						return &ExecutionResult{
							TaskID:   task.ID,
							Success:  false,
							Error:    fmt.Sprintf("sub-issue execution failed: %v", err),
							Duration: time.Since(start),
							IsEpic:   true,
							EpicPlan: plan,
						}, nil
					}

					// GH-539: Epic sub-executions may have created commits on the branch.
					// Push branch and create PR to propagate deliverables.
					// GH-2428: Set ModelName so the saved row distinguishes "epic
					// orchestrator (no backend call)" from "telemetry-missing".
					epicResult := &ExecutionResult{
						TaskID:    task.ID,
						Success:   true,
						Output:    fmt.Sprintf("Epic completed: %d sub-issues executed", len(issues)),
						Duration:  time.Since(start),
						IsEpic:    true,
						EpicPlan:  plan,
						ModelName: r.fallbackModelName(),
					}

					if task.CreatePR && task.Branch != "" {
						epicGit := NewGitOperations(executionPath)

						r.reportProgress(task.ID, "Creating PR", 96, "Pushing epic branch...")

						if err := epicGit.Push(ctx, task.Branch); err != nil {
							r.log.Warn("Epic branch push failed",
								slog.String("task_id", task.ID),
								slog.String("branch", task.Branch),
								slog.Any("error", err),
							)
							// Don't fail the epic — sub-issues may have their own PRs
						} else {
							// Determine base branch
							baseBranch := task.BaseBranch
							if baseBranch == "" {
								baseBranch, _ = epicGit.GetDefaultBranch(ctx)
								if baseBranch == "" {
									baseBranch = "main"
								}
							}

							// GH-2743: no-commits guard for epic PR path.
							// TASK-356 #1: harvest CommitSHA ONLY after this guard passes. The epic
							// parent runs in an orchestrator-only worktree whose HEAD == base HEAD,
							// so reading the SHA before the guard recorded that foreign base SHA as
							// the epic's CommitSHA — making a no-deliverable epic look "completed"
							// (a false-positive no-op that hid the loss of the child's real work).
							if guardCount, _ := epicGit.CountNewCommits(ctx, baseBranch); guardCount == 0 {
								r.log.Warn("Epic branch has no commits vs base, skipping PR creation",
									slog.String("task_id", task.ID),
									slog.String("base_branch", baseBranch),
								)
								r.reportProgress(task.ID, "PR Skipped", 97, "epic branch has no commits relative to base")
								return epicResult, nil
							}

							// Parent branch carries real commits — safe to record its HEAD as the
							// epic's deliverable SHA (it is no longer the foreign base SHA).
							if sha, shaErr := epicGit.GetCurrentCommitSHA(ctx); shaErr == nil && sha != "" {
								epicResult.CommitSHA = sha
							}

							// Create PR with GitHub auto-close keyword
							epicIssueNum := strings.TrimPrefix(task.ID, "GH-")
							prBody := fmt.Sprintf("## Summary\n\nAutomated PR created by Pilot for epic task %s.\n\nCloses #%s\n\n## Changes\n\n%s", task.ID, epicIssueNum, task.Description)
							epicPRTitle := fmt.Sprintf("%s: %s", task.ID, task.Title)
							prURL, prErr := epicGit.CreatePR(ctx, epicPRTitle, prBody, baseBranch)
							if prErr != nil {
								r.log.Warn("Epic PR creation failed",
									slog.String("task_id", task.ID),
									slog.Any("error", prErr),
								)
							} else {
								epicResult.PRUrl = prURL
								r.log.Info("Epic PR created", slog.String("pr_url", prURL))
							}
						}
					}

					r.reportProgress(task.ID, "Complete", 100, "Epic completed successfully")
					return epicResult, nil
				}
			}
		} // else: plan succeeded
	}

	// Check for task decomposition (GH-218)
	// Decomposition happens before timeout setup because subtasks have their own timeouts
	if r.decomposer != nil {
		result := r.decomposer.Decompose(task)
		if result.Decomposed && len(result.Subtasks) > 1 {
			r.log.Info("Task decomposed",
				slog.String("task_id", task.ID),
				slog.Int("subtask_count", len(result.Subtasks)),
				slog.String("reason", result.Reason),
			)
			return r.executeDecomposedTask(ctx, task, result.Subtasks, executionPath)
		}
	}

	return nil, nil
}

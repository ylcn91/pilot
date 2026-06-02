package executor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ylcn91/pilot/internal/executor/workflow"
	"github.com/ylcn91/pilot/internal/memory"
	"github.com/ylcn91/pilot/internal/quality"
	"github.com/ylcn91/pilot/internal/replay"
	"github.com/ylcn91/pilot/internal/webhooks"
)

// Execute runs a task using the configured backend and returns the execution result.
// It handles the complete task lifecycle: branch creation, prompt building,
// backend invocation, progress tracking, and optional PR creation.
// The context can be used to cancel execution. Returns an error only for
// setup failures; execution failures are reported in ExecutionResult.
//
// When a decomposer is configured and enabled, complex tasks are automatically
// split into subtasks that run sequentially (GH-218). Only the final subtask
// creates a PR, accumulating all changes from previous subtasks.
func (r *Runner) Execute(ctx context.Context, task *Task) (*ExecutionResult, error) {
	return r.executeWithOptions(ctx, task, true)
}

// executeWithOptions is the internal implementation that allows controlling worktree creation.
// When allowWorktree is false, it skips worktree creation even if configured.
// This prevents recursive worktree creation in sub-issues and decomposed tasks.
func (r *Runner) executeWithOptions(ctx context.Context, task *Task, allowWorktree bool) (*ExecutionResult, error) {
	start := time.Now()
	defer func() {
		if r.metricsRecorder != nil {
			r.metricsRecorder.RecordExecutionDuration(time.Since(start))
		}
	}()

	// Signal monitor that execution is actually starting (queued→running transition)
	if r.monitor != nil {
		r.monitor.Start(task.ID)
	}

	// GH-1599: Log task started milestone
	r.saveLogEntry(task.ID, "info", "Task started: "+task.Title)

	// GH-386: Validate source repo matches project path to prevent cross-project execution
	if task.SourceRepo != "" && task.ProjectPath != "" {
		if err := ValidateRepoProjectMatch(task.SourceRepo, task.ProjectPath); err != nil {
			return &ExecutionResult{
				TaskID:  task.ID,
				Success: false,
				Error:   fmt.Sprintf("cross-project execution blocked: %v", err),
			}, fmt.Errorf("cross-project execution blocked: %w", err)
		}
	}

	// GH-634: Enforce team permissions before execution
	if r.teamChecker != nil && task.MemberID != "" {
		if err := r.teamChecker.CheckProjectAccess(task.MemberID, task.ProjectPath, "execute_tasks"); err != nil {
			return &ExecutionResult{
				TaskID:  task.ID,
				Success: false,
				Error:   fmt.Sprintf("permission denied: %v", err),
			}, fmt.Errorf("permission check failed: %w", err)
		}
	}

	// GH-936: Create isolated worktree if configured
	// This allows execution even when user has uncommitted changes in their working directory
	executionPath := task.ProjectPath
	var cleanupWorktree func()

	// Debug: log worktree condition state
	r.log.Info("Worktree condition check",
		slog.Bool("allowWorktree", allowWorktree),
		slog.Bool("configNotNil", r.config != nil),
		slog.Bool("useWorktree", r.config != nil && r.config.UseWorktree),
		slog.String("branch", task.Branch),
		slog.Bool("directCommit", task.DirectCommit),
	)

	if allowWorktree && r.config != nil && r.config.UseWorktree && task.Branch != "" && !task.DirectCommit {
		r.log.Info("Creating isolated worktree for execution",
			slog.String("task_id", task.ID),
			slog.String("branch", task.Branch),
		)
		r.reportProgress(task.ID, "Worktree", 1, "Creating isolated worktree...")

		var worktreePath string
		var cleanup func()
		var err error

		// GH-1078: Use pool if available, otherwise fall back to direct creation
		if r.worktreeManager != nil && r.worktreeManager.PoolSize() > 0 {
			r.log.Debug("Using worktree pool",
				slog.Int("pool_available", r.worktreeManager.PoolAvailable()),
			)
			var result *WorktreeResult
			result, err = r.worktreeManager.Acquire(ctx, task.ID, task.Branch, "")
			if err == nil {
				worktreePath = result.Path
				cleanup = result.Cleanup
			}
		} else {
			worktreePath, cleanup, err = CreateWorktreeWithBranch(
				ctx, task.ProjectPath, task.ID, task.Branch, "")
		}

		if err != nil {
			r.log.Error("Failed to create worktree",
				slog.String("task_id", task.ID),
				slog.Any("error", err),
			)
			return &ExecutionResult{
				TaskID:  task.ID,
				Success: false,
				Error:   fmt.Sprintf("failed to create worktree: %v", err),
			}, fmt.Errorf("worktree creation failed: %w", err)
		}
		cleanupWorktree = cleanup
		executionPath = worktreePath

		// Copy Navigator config to worktree (handles untracked .agent/ content)
		if err := EnsureNavigatorInWorktree(task.ProjectPath, worktreePath); err != nil {
			cleanup()
			r.log.Error("Failed to copy Navigator to worktree",
				slog.String("task_id", task.ID),
				slog.Any("error", err),
			)
			return &ExecutionResult{
				TaskID:  task.ID,
				Success: false,
				Error:   fmt.Sprintf("failed to setup navigator in worktree: %v", err),
			}, fmt.Errorf("navigator worktree setup failed: %w", err)
		}

		r.log.Info("Using isolated worktree",
			slog.String("task_id", task.ID),
			slog.String("worktree", worktreePath),
		)
		r.reportProgress(task.ID, "Worktree", 2, "Worktree ready")
	}

	// Ensure worktree cleanup on exit (handles panic, early return, success).
	// before_remove hook fires just before worktree teardown (TASK-305).
	var beforeRemoveHookFn func()
	if cleanupWorktree != nil {
		defer func() {
			if beforeRemoveHookFn != nil {
				beforeRemoveHookFn()
			}
			cleanupWorktree()
		}()
	}

	// GH-915: Run pre-flight checks to catch environmental issues early
	// Skip when using mock backends in tests (skipPreflightChecks flag)
	// GH-1002: Skip git_clean check when worktree isolation is enabled
	// LocalMode: skip git_clean because sandbox workspaces can have pre-existing files that
	// create dirty git state after our install script commits.
	if !r.skipPreflightChecks {
		preflightOpts := PreflightOptions{
			SkipGitClean: task.LocalMode || (r.config != nil && r.config.UseWorktree),
			BackendType:  r.backendType(),
		}
		if err := RunPreflightChecksWithOptions(ctx, executionPath, preflightOpts); err != nil {
			r.log.Warn("Pre-flight check failed",
				slog.String("task_id", task.ID),
				slog.Any("error", err),
			)
			return &ExecutionResult{
				TaskID:  task.ID,
				Success: false,
				Error:   fmt.Sprintf("pre-flight check failed: %v", err),
			}, fmt.Errorf("pre-flight check failed: %w", err)
		}
	}

	// Auto-init Navigator if configured and missing
	// Use executionPath to check/init in worktree if worktree isolation is active
	// Skip for LocalMode — sandbox tasks don't use Navigator (GH-2108)
	if !task.LocalMode && r.config != nil && r.config.Navigator != nil && r.config.Navigator.AutoInit {
		if err := r.maybeInitNavigator(executionPath); err != nil {
			r.log.Warn("Navigator auto-init failed", slog.Any("error", err))
			// Continue without Navigator - graceful degradation
		}
	}

	// Detect complexity for routing decisions
	complexity := DetectComplexity(task)

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

	// Apply timeout based on task complexity.
	// LocalMode: override to complex timeout (60m minimum) since sandbox tasks
	// can't be reliably classified from short descriptions alone. A "trivial"
	// classification giving 15m timeout caused filter-js-from-html to fail.
	timeout := r.modelRouter.SelectTimeout(task)
	if task.LocalMode {
		complexTimeout := r.modelRouter.GetTimeoutForComplexity(ComplexityComplex)
		if timeout < complexTimeout {
			timeout = complexTimeout
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	log := r.log.With(
		slog.String("task_id", task.ID),
		slog.String("backend", r.backend.Name()),
		slog.String("complexity", complexity.String()),
		slog.Duration("timeout", timeout),
	)

	selectedModel := r.resolveSelectedModel(task)
	if selectedModel != "" {
		log = log.With(slog.String("routed_model", selectedModel))
	}

	// Select effort if routing is enabled
	selectedEffort := r.modelRouter.SelectEffort(task)
	if selectedEffort != "" {
		log = log.With(slog.String("routed_effort", selectedEffort))
	}

	log.Info("Starting task execution",
		slog.String("project", task.ProjectPath),
		slog.String("branch", task.Branch),
		slog.Bool("create_pr", task.CreatePR),
	)

	// Emit task started event
	r.emitAlertEvent(AlertEvent{
		Type:      AlertEventTypeTaskStarted,
		TaskID:    task.ID,
		TaskTitle: task.Title,
		Project:   task.ProjectPath,
		Timestamp: time.Now(),
	})

	// Dispatch webhook for task started
	r.dispatchWebhook(ctx, webhooks.EventTaskStarted, webhooks.TaskStartedData{
		TaskID:      task.ID,
		Title:       task.Title,
		Description: task.Description,
		Project:     task.ProjectPath,
		Source:      "pilot",
	})

	// Initialize git operations in execution path (worktree or original)
	git := NewGitOperations(executionPath)

	// Create branch if specified (skip for direct commit mode and worktree mode)
	// When using worktree, CreateWorktreeWithBranch already created the branch
	useWorktree := r.config != nil && r.config.UseWorktree && task.Branch != "" && !task.DirectCommit
	if task.Branch != "" && !task.DirectCommit && !useWorktree {
		r.reportProgress(task.ID, "Branching", 3, "Switching to default branch...")

		// GH-279: Always switch to default branch and pull latest before creating new branch.
		// This prevents new branches from forking off previous pilot branches instead of main.
		// GH-836: Hard fail if we can't switch - continuing from wrong branch causes corrupted PRs.
		// GH-2290: Honor task.BaseBranch (sourced from project.default_branch / branch_from) so
		// `main → dev → feature` workflows branch off dev rather than git's HEAD.
		var defaultBranch string
		var err error
		if task.BaseBranch != "" {
			defaultBranch, err = git.SwitchToBranchAndPull(ctx, task.BaseBranch)
		} else {
			defaultBranch, err = git.SwitchToDefaultBranchAndPull(ctx)
		}
		if err != nil {
			return nil, fmt.Errorf("branch switch failed, aborting execution: failed to switch to default branch: %w", err)
		}
		r.reportProgress(task.ID, "Branching", 5, fmt.Sprintf("On %s, creating %s...", defaultBranch, task.Branch))

		if err := git.CreateBranch(ctx, task.Branch); err != nil {
			// Branch already exists - check if it's stale (GH-912)
			behindCount, behindErr := git.CommitsBehindMain(ctx, task.Branch)
			if behindErr != nil {
				log.Warn("Failed to check if branch is behind main",
					slog.String("branch", task.Branch),
					slog.Any("error", behindErr),
				)
			}

			if behindCount > 0 {
				// Branch is stale - delete and recreate from main
				log.Info("Stale branch detected, recreating from main",
					slog.String("branch", task.Branch),
					slog.Int("commits_behind", behindCount),
				)
				r.reportProgress(task.ID, "Branching", 6, fmt.Sprintf("Stale branch %s (%d behind), recreating...", task.Branch, behindCount))

				if delErr := git.DeleteBranch(ctx, task.Branch); delErr != nil {
					log.Warn("Failed to delete stale branch",
						slog.String("branch", task.Branch),
						slog.Any("error", delErr),
					)
				}
				// Create fresh branch from main
				if createErr := git.CreateBranch(ctx, task.Branch); createErr != nil {
					return nil, fmt.Errorf("failed to recreate branch after stale detection: %w", createErr)
				}
				r.reportProgress(task.ID, "Branching", 8, fmt.Sprintf("Recreated fresh branch %s", task.Branch))
			} else {
				// Branch exists and is not stale - switch to it
				if switchErr := git.SwitchBranch(ctx, task.Branch); switchErr != nil {
					return nil, fmt.Errorf("failed to create/switch branch: %w", err)
				}
				r.reportProgress(task.ID, "Branching", 8, fmt.Sprintf("Switched to existing branch %s", task.Branch))
			}
		} else {
			r.reportProgress(task.ID, "Branching", 8, fmt.Sprintf("Created branch %s", task.Branch))
			r.saveLogEntry(task.ID, "info", "Branch created: "+task.Branch)
		}
	}

	// GH-994: Create task documentation if Navigator is present
	agentPath := filepath.Join(executionPath, ".agent")
	if _, err := os.Stat(agentPath); err == nil {
		if err := CreateTaskDoc(agentPath, task); err != nil {
			log.Warn("Failed to create task doc", slog.Any("error", err))
		}
	}

	// Run parallel research phase for medium/complex tasks (GH-217)
	var researchResult *ResearchResult
	if r.parallelRunner != nil && complexity.ShouldRunResearch() {
		r.reportProgress(task.ID, "Research", 10, "Running parallel research...")
		r.saveLogEntry(task.ID, "info", "Exploring codebase...")
		var researchErr error
		researchResult, researchErr = r.parallelRunner.ExecuteResearchPhase(ctx, task)
		if researchErr != nil {
			log.Warn("Research phase failed, continuing without research context",
				slog.String("task_id", task.ID),
				slog.Any("error", researchErr),
			)
		} else if researchResult != nil && len(researchResult.Findings) > 0 {
			log.Info("Research phase completed",
				slog.String("task_id", task.ID),
				slog.Int("findings", len(researchResult.Findings)),
				slog.Duration("duration", researchResult.Duration),
				slog.Int64("tokens", researchResult.TotalTokens),
			)
		}
	}

	// Load per-repo workflow override (.pilot/workflow.yaml) — TASK-304.
	// Applied before prompt construction so overrides are visible immediately.
	var workflowMaxTurns int
	repoWorkflow, wfErr := workflow.Load(executionPath)
	if wfErr != nil {
		log.Warn("Failed to load .pilot/workflow.yaml, using defaults",
			slog.String("task_id", task.ID),
			slog.Any("error", wfErr),
		)
	} else if repoWorkflow != nil {
		log.Info("Loaded .pilot/workflow.yaml",
			slog.String("task_id", task.ID),
			slog.Int("max_turns", repoWorkflow.Agent.MaxTurns),
			slog.String("reasoning_effort", repoWorkflow.Agent.ReasoningEffort),
		)
		if repoWorkflow.Agent.ReasoningEffort != "" {
			selectedEffort = repoWorkflow.Agent.ReasoningEffort
		}
		workflowMaxTurns = repoWorkflow.Agent.MaxTurns
	}

	// TASK-305: fire after_create and wire before_remove now that the workflow is loaded.
	var hookEnv []string
	if repoWorkflow != nil {
		hookEnv = append(os.Environ(), "PILOT_TASK_ID="+task.ID)
		runWorkflowHook(ctx, "after_create", repoWorkflow.Hooks.AfterCreate, executionPath, hookEnv, log)
		if len(repoWorkflow.Hooks.BeforeRemove) > 0 {
			scripts := repoWorkflow.Hooks.BeforeRemove
			env := hookEnv
			beforeRemoveHookFn = func() {
				runWorkflowHook(context.Background(), "before_remove", scripts, executionPath, env, log)
			}
		}
	}

	// Build the prompt
	prompt := r.BuildPrompt(task, executionPath)

	// Append per-repo workflow prompt appendix if present (TASK-304).
	if repoWorkflow != nil && repoWorkflow.PromptAppendix != "" {
		prompt += "\n\n## Project Workflow\n\n" + repoWorkflow.PromptAppendix
	}

	// Append research context if available (GH-217)
	if researchResult != nil && len(researchResult.Findings) > 0 {
		prompt = r.appendResearchContext(prompt, researchResult)
	}

	// State for tracking progress
	state := &progressState{phase: "Starting", budgetCancel: cancel}

	// Initialize recorder if recording is enabled
	var recorder *replay.Recorder
	if r.enableRecording {
		var recErr error
		recorder, recErr = replay.NewRecorder(task.ID, task.ProjectPath, r.getRecordingsPath())
		if recErr != nil {
			log.Warn("Failed to create recorder, continuing without recording", slog.Any("error", recErr))
		} else {
			recorder.SetBranch(task.Branch)
			log.Debug("Recording enabled", slog.String("recording_id", recorder.GetRecordingID()))
		}
	}

	// Report start
	backendName := r.backend.Name()
	r.reportProgress(task.ID, "Starting", 0, fmt.Sprintf("Initializing %s...", backendName))

	// Clean stale pilot hooks unconditionally — even when hooks.enabled is false.
	// Prevents dead entries from accumulating after OS reboots clear temp dirs (GH-1749).
	// Clean project root first (always), then worktree path if different (GH-1884).
	projectSettingsPath := filepath.Join(task.ProjectPath, ".claude", "settings.json")
	if cleanErr := CleanStalePilotHooks(projectSettingsPath); cleanErr != nil {
		log.Warn("Failed to clean stale pilot hooks in project root", slog.Any("error", cleanErr))
	}
	if executionPath != task.ProjectPath {
		worktreeSettingsPath := filepath.Join(executionPath, ".claude", "settings.json")
		if cleanErr := CleanStalePilotHooks(worktreeSettingsPath); cleanErr != nil {
			log.Warn("Failed to clean stale pilot hooks in worktree", slog.Any("error", cleanErr))
		}
	}

	// Setup Claude Code hooks if enabled (GH-1266)
	var hookRestoreFunc func() error
	if r.config != nil && r.config.Hooks != nil && r.config.Hooks.Enabled {
		log.Debug("Setting up Claude Code hooks", slog.String("task_id", task.ID))

		// Create temporary directory for hook scripts
		scriptDir, err := os.MkdirTemp("", "pilot-hooks-")
		if err != nil {
			log.Error("Failed to create hooks script directory", slog.Any("error", err))
		} else {
			// Write embedded scripts
			if err := WriteEmbeddedScripts(scriptDir); err != nil {
				log.Error("Failed to write embedded hook scripts", slog.Any("error", err))
			} else {
				// Generate Claude settings
				hookSettings := GenerateClaudeSettings(r.config.Hooks, scriptDir)

				// Merge with existing settings.json (worktree-safe path)
				settingsPath := filepath.Join(executionPath, ".claude", "settings.json")
				_, mergeErr := MergeWithExisting(settingsPath, hookSettings)
				if mergeErr != nil {
					log.Error("Failed to setup Claude hooks", slog.Any("error", mergeErr))
					// Clean up script directory
					if rmErr := os.RemoveAll(scriptDir); rmErr != nil {
						log.Warn("Failed to clean up hook scripts after merge error", slog.Any("error", rmErr))
					}
				} else {
					hookRestoreFunc = func() error {
						// Instead of blind restoreFunc() (which may write back stale entries
						// from a previous crash), use targeted cleanup (GH-1884).
						if cleanErr := CleanStalePilotHooks(settingsPath); cleanErr != nil {
							log.Warn("Failed to clean pilot hooks from settings", slog.Any("error", cleanErr))
						}
						// Clean up script directory
						if rmErr := os.RemoveAll(scriptDir); rmErr != nil {
							log.Warn("Failed to clean up hook scripts", slog.Any("error", rmErr))
						}
						return nil
					}
					log.Debug("Claude Code hooks configured",
						slog.String("settings_path", settingsPath),
						slog.String("script_dir", scriptDir))
				}
			}
		}
	}

	// Ensure cleanup happens regardless of execution outcome
	defer func() {
		if hookRestoreFunc != nil {
			_ = hookRestoreFunc() // Error already logged inside hookRestoreFunc
		}
	}()

	// GH-1599: Log implementation phase
	r.saveLogEntry(task.ID, "info", "Implementing changes...")

	// TASK-308: Stall detection — track last event time and spawn a watchdog.
	var (
		lastEventAt       atomic.Int64
		stallDetectedFlag atomic.Bool
		stallDone         = make(chan struct{})
	)
	lastEventAt.Store(time.Now().UnixNano())
	stallTimeout := r.effectiveStallTimeout()
	var stallExecutionCtx context.Context
	var stallCancel context.CancelFunc
	if stallTimeout > 0 {
		stallExecutionCtx, stallCancel = context.WithCancel(ctx)
		go r.runStallWatchdog(task.ID, &lastEventAt, &stallDetectedFlag, stallTimeout, stallDone, stallCancel)
	} else {
		stallExecutionCtx = ctx
		stallCancel = func() {}
	}

	// Execute via backend with watchdog (GH-882)
	// Watchdog kills subprocess after 2x timeout as a safety net for processes
	// that ignore context cancellation.
	// TASK-305: before_run hook fires just before agent execution.
	if repoWorkflow != nil {
		runWorkflowHook(ctx, "before_run", repoWorkflow.Hooks.BeforeRun, executionPath, hookEnv, log)
	}

	watchdogTimeout := 2 * timeout
	allowedTools, mcpConfigPath := r.executionToolOptions()
	backendResult, err := r.backend.Execute(stallExecutionCtx, ExecuteOptions{
		Prompt:          prompt,
		ProjectPath:     executionPath, // Use worktree path if active
		Verbose:         task.Verbose,
		Model:           selectedModel,
		Effort:          selectedEffort,
		MaxTurns:        workflowMaxTurns, // TASK-304: per-repo .pilot/workflow.yaml override
		FromPR:          task.FromPR,      // GH-1267: session resumption from PR context
		WatchdogTimeout: watchdogTimeout,
		AllowedTools:    allowedTools,
		MCPConfigPath:   mcpConfigPath,
		WatchdogCallback: func(pid int, watchdogDuration time.Duration) {
			log.Warn("Watchdog killed subprocess",
				slog.Int("pid", pid),
				slog.Duration("watchdog_timeout", watchdogDuration),
				slog.Duration("configured_timeout", timeout),
			)
			r.reportProgress(task.ID, "Watchdog Kill", 100, fmt.Sprintf("Process killed by watchdog after %v (2x timeout)", watchdogDuration))

			// Emit watchdog kill alert
			r.emitAlertEvent(AlertEvent{
				Type:      AlertEventTypeWatchdogKill,
				TaskID:    task.ID,
				TaskTitle: task.Title,
				Project:   task.ProjectPath,
				Error:     fmt.Sprintf("subprocess killed by watchdog after %v", watchdogDuration),
				Metadata: map[string]string{
					"pid":                fmt.Sprintf("%d", pid),
					"watchdog_timeout":   watchdogDuration.String(),
					"configured_timeout": timeout.String(),
					"complexity":         complexity.String(),
				},
				Timestamp: time.Now(),
			})
		},
		EventHandler: func(event BackendEvent) {
			// TASK-308: touch the last-event timestamp so the stall watchdog resets.
			lastEventAt.Store(time.Now().UnixNano())

			// Record the event
			if recorder != nil {
				if recErr := recorder.RecordEvent(event.Raw); recErr != nil {
					log.Warn("Failed to record event", slog.Any("error", recErr))
				}
			}

			// Process event for progress tracking
			r.processBackendEvent(task.ID, event, state)
		},
	})

	// Stop stall watchdog and release stall context resources.
	close(stallDone)
	stallCancel()

	// TASK-305: after_run hook fires as soon as the agent finishes (success or error).
	if repoWorkflow != nil {
		runWorkflowHook(ctx, "after_run", repoWorkflow.Hooks.AfterRun, executionPath, hookEnv, log)
	}

	// Transfer stallDetected flag to progressState for post-Execute checks.
	if stallDetectedFlag.Load() {
		state.stallDetected = true
	}

	duration := time.Since(start)

	// Build execution result
	result := &ExecutionResult{
		TaskID:          task.ID,
		Duration:        duration,
		EffortLevel:     selectedEffort,
		ComplexityLevel: complexity.String(),
	}

	if err != nil {
		result.Success = false

		// GH-539: Check if this was a per-task budget limit breach
		if state.budgetExceeded {
			result.Outcome = "budget_exceeded" // TASK-358: not a code failure
			result.Error = fmt.Sprintf("per-task budget limit exceeded: %s", state.budgetReason)
			result.TokensInput = state.tokensInput
			result.TokensOutput = state.tokensOutput
			result.TokensTotal = state.tokensInput + state.tokensOutput
			result.CacheCreationInputTokens = state.cacheCreationInputTokens
			result.CacheReadInputTokens = state.cacheReadInputTokens
			result.ModelName = state.modelName
			if result.ModelName == "" {
				result.ModelName = r.fallbackModelName()
			}
			result.EstimatedCostUSD = estimateCostWithCache(result.TokensInput, result.TokensOutput, result.CacheCreationInputTokens, result.CacheReadInputTokens, result.ModelName)
			log.Warn("Task cancelled due to per-task budget limit",
				slog.String("task_id", task.ID),
				slog.String("reason", state.budgetReason),
				slog.Int64("input_tokens", state.tokensInput),
				slog.Int64("output_tokens", state.tokensOutput),
				slog.Duration("duration", duration),
			)
			r.reportProgress(task.ID, "Budget Exceeded", 100, result.Error)

			// Emit budget exceeded alert event
			r.emitAlertEvent(AlertEvent{
				Type:      AlertEventTypeTaskFailed,
				TaskID:    task.ID,
				TaskTitle: task.Title,
				Project:   task.ProjectPath,
				Error:     result.Error,
				Metadata: map[string]string{
					"reason":        "budget_exceeded",
					"input_tokens":  fmt.Sprintf("%d", state.tokensInput),
					"output_tokens": fmt.Sprintf("%d", state.tokensOutput),
				},
				Timestamp: time.Now(),
			})

			if recorder != nil {
				recorder.SetModel(state.modelName)
				recorder.SetNavigator(state.hasNavigator)
				if finErr := recorder.Finish("budget_exceeded"); finErr != nil {
					log.Warn("Failed to finish recording", slog.Any("error", finErr))
				}
			}
			return result, nil
		}

		// TASK-308: Check if this was a stall (no event activity for stall_timeout).
		if state.stallDetected {
			result.Outcome = "stalled" // TASK-358: incomplete run, not a code failure
			result.Error = fmt.Sprintf("session stalled: no agent event for >%v", stallTimeout)
			result.TokensInput = state.tokensInput
			result.TokensOutput = state.tokensOutput
			result.TokensTotal = state.tokensInput + state.tokensOutput
			result.CacheCreationInputTokens = state.cacheCreationInputTokens
			result.CacheReadInputTokens = state.cacheReadInputTokens
			result.ModelName = state.modelName
			if result.ModelName == "" {
				result.ModelName = r.fallbackModelName()
			}
			result.EstimatedCostUSD = estimateCostWithCache(result.TokensInput, result.TokensOutput, result.CacheCreationInputTokens, result.CacheReadInputTokens, result.ModelName)
			log.Warn("Task stalled: no agent event activity",
				slog.String("task_id", task.ID),
				slog.Duration("stall_timeout", stallTimeout),
				slog.Duration("duration", duration),
			)
			r.reportProgress(task.ID, "Stalled", 100, result.Error)
			if r.monitor != nil {
				r.monitor.Stall(task.ID, result.Error)
			}
			r.emitAlertEvent(AlertEvent{
				Type:      AlertEventTypeTaskFailed,
				TaskID:    task.ID,
				TaskTitle: task.Title,
				Project:   task.ProjectPath,
				Error:     result.Error,
				Metadata: map[string]string{
					"reason":        "stalled",
					"stall_timeout": stallTimeout.String(),
					"duration_ms":   fmt.Sprintf("%d", duration.Milliseconds()),
				},
				Timestamp: time.Now(),
			})
			if r.metricsRecorder != nil {
				r.metricsRecorder.RecordExecution(result.ModelName, "stalled")
			}
			if recorder != nil {
				recorder.SetModel(state.modelName)
				recorder.SetNavigator(state.hasNavigator)
				if finErr := recorder.Finish("stalled"); finErr != nil {
					log.Warn("Failed to finish recording", slog.Any("error", finErr))
				}
			}
			return result, nil
		}

		// Check if this was a timeout
		timedOut := ctx.Err() == context.DeadlineExceeded
		if timedOut {
			result.Error = fmt.Sprintf("task timed out after %v", timeout)
			log.Error("Task timed out",
				slog.String("task_id", task.ID),
				slog.String("complexity", complexity.String()),
				slog.Duration("timeout", timeout),
				slog.Duration("duration", duration),
			)
			r.reportProgress(task.ID, "Timeout", 100, result.Error)

			// Emit task timeout event with complexity metadata
			r.emitAlertEvent(AlertEvent{
				Type:      AlertEventTypeTaskTimeout,
				TaskID:    task.ID,
				TaskTitle: task.Title,
				Project:   task.ProjectPath,
				Error:     result.Error,
				Metadata: map[string]string{
					"complexity":  complexity.String(),
					"timeout":     timeout.String(),
					"duration_ms": fmt.Sprintf("%d", duration.Milliseconds()),
				},
				Timestamp: time.Now(),
			})

			// Dispatch webhook for task timeout
			r.dispatchWebhook(ctx, webhooks.EventTaskTimeout, webhooks.TaskTimeoutData{
				TaskID:     task.ID,
				Title:      task.Title,
				Project:    task.ProjectPath,
				Duration:   duration,
				Timeout:    timeout,
				Complexity: complexity.String(),
				Phase:      state.phase,
			})
		} else {
			// GH-917: Check for classified Claude Code error types
			alertType := AlertEventTypeTaskFailed
			errorCategory := "unknown"
			var stderrOutput string // GH-917-5: Always capture stderr for logging

			if beErr, ok := err.(BackendError); ok {
				result.Error = beErr.Error()
				stderrOutput = beErr.ErrorStderr() // Capture stderr from classified error

				// Map error type to alert event type and category
				switch beErr.ErrorType() {
				case "rate_limit":
					alertType = AlertEventTypeRateLimit
					errorCategory = "rate_limit"
					log.Warn("Backend hit rate limit",
						slog.String("task_id", task.ID),
						slog.String("stderr", beErr.ErrorStderr()),
						slog.Duration("duration", duration),
					)
					r.reportProgress(task.ID, "Rate Limited", 100, "Backend hit rate limit - retry later")

				case "invalid_config":
					alertType = AlertEventTypeConfigError
					errorCategory = "invalid_config"
					log.Error("Invalid backend configuration",
						slog.String("task_id", task.ID),
						slog.String("message", beErr.ErrorMessage()),
						slog.String("stderr", beErr.ErrorStderr()),
					)
					r.reportProgress(task.ID, "Config Error", 100, beErr.ErrorMessage())

				case "api_error":
					alertType = AlertEventTypeAPIError
					errorCategory = "api_error"
					log.Error("Backend API error",
						slog.String("task_id", task.ID),
						slog.String("message", beErr.ErrorMessage()),
						slog.String("stderr", beErr.ErrorStderr()),
					)
					r.reportProgress(task.ID, "API Error", 100, beErr.ErrorMessage())

				case "oom_killed":
					// GH-2332: distinct alert so operators can spot memory-pressure
					// patterns instead of burying OOM kills in the generic "unknown" bucket.
					alertType = AlertEventTypeOOMKilled
					errorCategory = "oom_killed"
					log.Error("Backend OOM-killed",
						slog.String("task_id", task.ID),
						slog.String("message", beErr.ErrorMessage()),
						slog.String("stderr", beErr.ErrorStderr()),
						slog.Duration("duration", duration),
					)
					r.reportProgress(task.ID, "OOM Killed", 100, beErr.ErrorMessage())

				default:
					// GH-917-5: Log stderr for process errors and unknown errors too
					log.Error("Backend execution failed",
						slog.String("error", result.Error),
						slog.String("error_type", beErr.ErrorType()),
						slog.String("stderr", beErr.ErrorStderr()),
						slog.Duration("duration", duration),
					)
					r.reportProgress(task.ID, "Failed", 100, result.Error)
				}
			} else {
				result.Error = err.Error()
				// GH-917-5: Log even when error is not a classified backend error
				log.Error("Backend execution failed",
					slog.String("error", result.Error),
					slog.String("error_type", "unclassified"),
					slog.Duration("duration", duration),
				)
				r.reportProgress(task.ID, "Failed", 100, result.Error)
			}

			// GH-920: Check for smart retry before emitting alerts
			// Note: state.smartRetryAttempt tracks retry attempts for this error path
			if r.retrier != nil {
				decision := r.retrier.Evaluate(err, state.smartRetryAttempt, timeout)
				if decision.ShouldRetry {
					// GH-1030: Record correction for drift detection
					if r.driftDetector != nil {
						r.driftDetector.RecordCorrection("retry_triggered", fmt.Sprintf("Error: %s, Retry attempt: %d", err.Error(), state.smartRetryAttempt+1))
					}
					state.smartRetryAttempt++
					log.Info("Smart retry triggered",
						slog.String("task_id", task.ID),
						slog.String("error_category", errorCategory),
						slog.Int("attempt", state.smartRetryAttempt),
						slog.Duration("backoff", decision.BackoffDuration),
					)
					r.reportProgress(task.ID, "Retrying", 50, fmt.Sprintf("Waiting %v before retry (attempt %d)...", decision.BackoffDuration, state.smartRetryAttempt))

					// Sleep for backoff duration
					if sleepErr := r.retrier.Sleep(ctx, decision.BackoffDuration); sleepErr != nil {
						log.Warn("Retry sleep interrupted", slog.Any("error", sleepErr))
						// Fall through to emit alerts
					} else {
						// Re-execute with potentially extended timeout
						retryTimeout := timeout
						if decision.ExtendedTimeout > 0 {
							retryTimeout = decision.ExtendedTimeout
						}
						retryCtx, retryCancel := context.WithTimeout(context.Background(), retryTimeout)

						r.reportProgress(task.ID, "Re-executing", 55, fmt.Sprintf("Retry attempt %d with %v timeout...", state.smartRetryAttempt, retryTimeout))

						smartAllowed, smartMCP := r.executionToolOptions()
						retryResult, retryErr := r.backend.Execute(retryCtx, ExecuteOptions{
							Prompt:          prompt,
							ProjectPath:     executionPath, // TASK-323: retry in the worktree, not the user's real repo
							Verbose:         task.Verbose,
							Model:           selectedModel,
							Effort:          selectedEffort,
							WatchdogTimeout: 2 * retryTimeout,
							AllowedTools:    smartAllowed,
							MCPConfigPath:   smartMCP,
							EventHandler: func(event BackendEvent) {
								if recorder != nil {
									_ = recorder.RecordEvent(event.Raw)
								}
								r.processBackendEvent(task.ID, event, state)
							},
						})
						retryCancel()

						if retryErr == nil && retryResult != nil && retryResult.Success {
							// Retry succeeded! Update backendResult and continue
							log.Info("Smart retry succeeded",
								slog.String("task_id", task.ID),
								slog.Int("attempt", state.smartRetryAttempt),
							)
							r.reportProgress(task.ID, "Retry Success", 90, "Retry completed successfully")

							// Update results from retry
							backendResult = retryResult
							err = nil
							goto retrySucceeded
						}
						// Retry failed, continue to emit alerts
						log.Warn("Smart retry failed",
							slog.String("task_id", task.ID),
							slog.Int("attempt", state.smartRetryAttempt),
							slog.Any("error", retryErr),
						)
					}
				}
			}

			// GH-1716: If execution was killed and decompose_on_kill is enabled,
			// attempt decomposition as last resort before failing.
			if r.retrier != nil && r.retrier.config.DecomposeOnKill && r.decomposer != nil {
				if beErr, ok := err.(BackendError); ok && beErr.ErrorType() == "timeout" {
					log.Info("Execution killed, attempting decomposition fallback",
						slog.String("task_id", task.ID))

					decompResult := r.decomposer.DecomposeForRetry(ctx, task)
					if decompResult.Decomposed && len(decompResult.Subtasks) > 1 {
						log.Info("Decomposition fallback succeeded",
							slog.String("task_id", task.ID),
							slog.Int("subtask_count", len(decompResult.Subtasks)))
						return r.executeDecomposedTask(ctx, task, decompResult.Subtasks, executionPath)
					}
				}
			}

			// GH-917-5: Include stderr in alert metadata for debugging
			metadata := map[string]string{
				"error_category": errorCategory,
			}
			if stderrOutput != "" {
				metadata["stderr"] = stderrOutput
			}

			// Emit alert event with error category metadata
			r.emitAlertEvent(AlertEvent{
				Type:      alertType,
				TaskID:    task.ID,
				TaskTitle: task.Title,
				Project:   task.ProjectPath,
				Error:     result.Error,
				Metadata:  metadata,
				Timestamp: time.Now(),
			})

			// Dispatch webhook for task failed (non-timeout)
			r.dispatchWebhook(ctx, webhooks.EventTaskFailed, webhooks.TaskFailedData{
				TaskID:   task.ID,
				Title:    task.Title,
				Project:  task.ProjectPath,
				Duration: duration,
				Error:    result.Error,
				Phase:    state.phase,
			})
		}

		// GH-1599: Log task failed milestone
		r.saveLogEntry(task.ID, "error", "Task failed: "+result.Error)

		// GH-2328: persist stderr + final assistant message + error type so
		// "unknown: exit status 1" is actually diagnosable. Without this,
		// failures look identical regardless of whether Claude refused, hit a
		// rate limit, was OOM-killed, or crashed silently.
		r.persistBackendDiagnostics(task.ID, backendResult)

		// Finish recording with failed status
		if recorder != nil {
			recorder.SetModel(state.modelName)
			recorder.SetNavigator(state.hasNavigator)
			if finErr := recorder.Finish("failed"); finErr != nil {
				log.Warn("Failed to finish recording", slog.Any("error", finErr))
			}
		}
		return result, nil
	}

retrySucceeded:
	// Copy backend result to execution result
	result.Success = backendResult.Success
	result.Output = backendResult.Output
	result.Error = backendResult.Error
	result.TokensInput = backendResult.TokensInput
	result.TokensOutput = backendResult.TokensOutput
	result.TokensTotal = backendResult.TokensInput + backendResult.TokensOutput
	result.ModelName = backendResult.Model
	// GH-3028: propagate RSS telemetry from backend to execution result.
	result.PeakRSSMB = backendResult.PeakRSSMB
	result.FinalRSSMB = backendResult.FinalRSSMB

	// Track research phase tokens (GH-217)
	if researchResult != nil {
		result.ResearchTokens = researchResult.TotalTokens
		result.TokensTotal += researchResult.TotalTokens
	}

	// Extract commit SHA from state (parsed from Claude Code output)
	if len(state.commitSHAs) > 0 {
		result.CommitSHA = state.commitSHAs[len(state.commitSHAs)-1] // Use last commit
	}

	// Post-execution summary via structured output (GH-1264)
	// This replaces brittle regex parsing with reliable --json-schema output
	if result.CommitSHA == "" && result.Success && r.config != nil && r.config.ClaudeCode != nil && r.config.ClaudeCode.UseStructuredOutput {
		if summary, summaryErr := r.getPostExecutionSummary(ctx); summaryErr == nil {
			if summary.CommitSHA != "" {
				result.CommitSHA = summary.CommitSHA
				log.Info("CommitSHA extracted via post-execution summary",
					slog.String("task_id", task.ID),
					slog.String("sha", summary.CommitSHA[:min(7, len(summary.CommitSHA))]),
					slog.String("branch", summary.BranchName),
				)
			}
		} else {
			log.Debug("post-execution summary failed, falling back to git",
				slog.String("task_id", task.ID),
				slog.Any("error", summaryErr),
			)
		}
	}

	// Fallback: if output parsing missed the commit SHA, ask git directly.
	// This handles cases where Claude's git commit output format doesn't match
	// the extractCommitSHA() pattern (e.g. different flags, localized output).
	if result.CommitSHA == "" && task.Branch != "" && result.Success {
		baseBranch := task.BaseBranch
		if baseBranch == "" {
			baseBranch, _ = git.GetDefaultBranch(ctx)
			if baseBranch == "" {
				baseBranch = "main"
			}
		}
		if commitCount, countErr := git.CountNewCommits(ctx, baseBranch); countErr == nil && commitCount > 0 {
			if sha, shaErr := git.GetCurrentCommitSHA(ctx); shaErr == nil && sha != "" {
				log.Info("CommitSHA recovered via git (output parsing missed it)",
					slog.String("task_id", task.ID),
					slog.String("sha", sha[:min(7, len(sha))]),
					slog.Int("new_commits", commitCount),
				)
				result.CommitSHA = sha
			}
		}
	}

	// GH-3126: Ghost-SHA guard — reject SHAs that are already on the base branch.
	// When Claude makes no new commit, git log returns the parent (pre-execution) SHA.
	// Recording that as CommitSHA causes IsTaskShipped to return true on a no-op run,
	// triggering pilot-done + issue close with no actual work delivered.
	// Fail open on check errors (e.g. no origin configured in test repos): only reject
	// when the check conclusively shows the SHA is already on origin/<base>.
	if result.CommitSHA != "" && result.Success {
		ghostBase := task.BaseBranch
		if ghostBase == "" {
			ghostBase, _ = git.GetDefaultBranch(ctx)
			if ghostBase == "" {
				ghostBase = "main"
			}
		}
		if isNew, checkErr := commitSHAIsNew(ctx, executionPath, result.CommitSHA, ghostBase); checkErr != nil {
			log.Warn("executor: ghost-SHA check skipped (will not block)",
				slog.String("task_id", task.ID),
				slog.String("sha", result.CommitSHA[:min(7, len(result.CommitSHA))]),
				slog.Any("error", checkErr),
			)
		} else if !isNew {
			log.Warn("executor: harvested SHA is already on base branch — no new commit",
				slog.String("task_id", task.ID),
				slog.String("sha", result.CommitSHA[:min(7, len(result.CommitSHA))]),
				slog.String("base", ghostBase),
			)
			result.CommitSHA = ""
			result.Success = false
			result.Error = "no new commit produced — worktree HEAD matches base branch parent"
		}
	}

	// Fill in additional metrics from state
	result.FilesChanged = state.filesWrite
	result.CacheCreationInputTokens = state.cacheCreationInputTokens
	result.CacheReadInputTokens = state.cacheReadInputTokens
	if result.ModelName == "" {
		result.ModelName = state.modelName
	}
	if result.ModelName == "" {
		// GH-2428: derive from config (DefaultModel/OpenCode.Model/backend type)
		// instead of hardcoding "claude-opus-4-6". The hardcoded value was stale
		// (Claude Code reports 4-7) and silently labelled OpenCode/GLM runs as
		// Claude Opus, biasing model-outcome metrics.
		result.ModelName = r.fallbackModelName()
	}
	// Estimate cost based on token usage (including research tokens) with cache-aware pricing (GH-2164)
	result.EstimatedCostUSD = estimateCostWithCache(result.TokensInput+result.ResearchTokens, result.TokensOutput, result.CacheCreationInputTokens, result.CacheReadInputTokens, result.ModelName)

	// Emit Prometheus counters for token usage, cost, and execution outcome (GH-2855).
	if r.metricsRecorder != nil {
		model := result.ModelName
		r.metricsRecorder.RecordTokens(model, "input", result.TokensInput+result.ResearchTokens)
		r.metricsRecorder.RecordTokens(model, "output", result.TokensOutput)
		if result.CacheCreationInputTokens > 0 {
			r.metricsRecorder.RecordTokens(model, "cache_creation", result.CacheCreationInputTokens)
		}
		if result.CacheReadInputTokens > 0 {
			r.metricsRecorder.RecordTokens(model, "cache_read", result.CacheReadInputTokens)
		}
		r.metricsRecorder.RecordCost(model, result.EstimatedCostUSD)
		outcomeLabel := "success"
		if !result.Success {
			outcomeLabel = "failed"
		}
		r.metricsRecorder.RecordExecution(model, outcomeLabel)
	}

	if !result.Success {
		log.Error("Task execution failed",
			slog.String("error", result.Error),
			slog.Duration("duration", duration),
		)
		r.reportProgress(task.ID, "Failed", 100, result.Error)
		r.saveLogEntry(task.ID, "error", "Task failed: "+result.Error)

		// GH-2328: persist stderr + final assistant message + error type.
		r.persistBackendDiagnostics(task.ID, backendResult)

		// Emit task failed event
		r.emitAlertEvent(AlertEvent{
			Type:      AlertEventTypeTaskFailed,
			TaskID:    task.ID,
			TaskTitle: task.Title,
			Project:   task.ProjectPath,
			Error:     result.Error,
			Timestamp: time.Now(),
		})

		// Dispatch webhook for task failed
		r.dispatchWebhook(ctx, webhooks.EventTaskFailed, webhooks.TaskFailedData{
			TaskID:   task.ID,
			Title:    task.Title,
			Project:  task.ProjectPath,
			Duration: duration,
			Error:    result.Error,
			Phase:    state.phase,
		})

		// Finish recording with failed status
		if recorder != nil {
			recorder.SetModel(result.ModelName)
			recorder.SetNavigator(state.hasNavigator)
			if finErr := recorder.Finish("failed"); finErr != nil {
				log.Warn("Failed to finish recording", slog.Any("error", finErr))
			}
		}
	} else {
		result.Success = true

		// Log execution metrics for observability (GH-54 speed optimization)
		metrics := NewExecutionMetrics(
			task.ID,
			complexity,
			result.ModelName,
			duration,
			state,
			timeout,
			false, // not timed out
		)
		log.Info("Task completed",
			slog.String("task_id", metrics.TaskID),
			slog.String("complexity", metrics.Complexity.String()),
			slog.String("model", metrics.Model),
			slog.Duration("duration", metrics.Duration),
			slog.Bool("navigator_skipped", metrics.NavigatorSkipped),
			slog.Int64("tokens_in", metrics.TokensIn),
			slog.Int64("tokens_out", metrics.TokensOut),
			slog.Float64("cost_usd", metrics.EstimatedCostUSD),
			slog.Int("files_read", metrics.FilesRead),
			slog.Int("files_written", metrics.FilesWritten),
		)
		r.reportProgress(task.ID, "Completed", 90, "Execution completed")

		// No-commit detection and retry (GH-916)
		// ~10% of failures are "No commits between main and pilot/GH-XXX"
		// Claude runs successfully but makes no actual changes, then PR creation fails.
		if task.CreatePR && !task.DirectCommit && task.Branch != "" {
			baseBranch := task.BaseBranch
			if baseBranch == "" {
				baseBranch, _ = git.GetDefaultBranch(ctx)
				if baseBranch == "" {
					baseBranch = "main"
				}
			}

			commitCount, countErr := git.CountNewCommits(ctx, baseBranch)
			if countErr != nil {
				log.Warn("Failed to count commits for no-commit check",
					slog.String("task_id", task.ID),
					slog.Any("error", countErr),
				)
			} else if commitCount == 0 {
				log.Warn("Claude made no commits, retrying with explicit instruction",
					slog.String("task_id", task.ID),
					slog.String("branch", task.Branch),
				)
				r.reportProgress(task.ID, "Retry", 91, "No commits detected, retrying...")

				// Build retry prompt with explicit instruction.
				// GH-2777: Offer DECLINED:<reason> as an escape hatch so Claude can
				// signal that a task is genuinely unactionable rather than silently
				// producing no output. The DECLINED path avoids the pilot-failed label
				// and adds pilot-needs-clarification instead.
				retryPrompt := fmt.Sprintf(`## Retry: No Changes Detected

The previous execution completed but made no code changes.

**Original Task:** %s

%s

**You have two options:**

**Option A — Implement:** If this task is actionable, implement the required changes and create at least one git commit. Do NOT just analyze or plan — actually write and commit code.

**Option B — Decline:** If this task is genuinely unactionable (e.g. the requirements are ambiguous, the requested feature already exists, or it would require information you don't have), output exactly one line in this format and nothing else after it:

  DECLINED:<concise reason — one sentence>

Examples of valid DECLINED lines:
  DECLINED: The authentication module requested already exists in internal/auth/jwt.go.
  DECLINED: The issue asks to "improve performance" without specifying which endpoint or metric.
  DECLINED: This requires production database credentials that are not available in this environment.

Only use DECLINED if implementation is truly impossible or undefined. Do not decline due to difficulty alone.`, task.Title, task.Description)

				// Execute retry
				noopRetryAllowed, noopRetryMCP := r.executionToolOptions()
				retryResult, retryErr := r.backend.Execute(ctx, ExecuteOptions{
					Prompt:          retryPrompt,
					ProjectPath:     executionPath, // TASK-323: retry in the worktree so CountNewCommits can see its commits
					Verbose:         task.Verbose,
					Model:           selectedModel,
					Effort:          selectedEffort,
					WatchdogTimeout: watchdogTimeout,
					AllowedTools:    noopRetryAllowed,
					MCPConfigPath:   noopRetryMCP,
					EventHandler: func(event BackendEvent) {
						// Track tokens from retry
						state.tokensInput += event.TokensInput
						state.tokensOutput += event.TokensOutput
						state.cacheCreationInputTokens += event.CacheCreationInputTokens
						state.cacheReadInputTokens += event.CacheReadInputTokens
						// Extract any commit SHAs from retry
						if event.Type == EventTypeToolResult && event.ToolResult != "" {
							extractCommitSHA(event.ToolResult, state)
						}
						if recorder != nil {
							if recErr := recorder.RecordEvent(event.Raw); recErr != nil {
								log.Warn("Failed to record retry event", slog.Any("error", recErr))
							}
						}
					},
				})

				// Update result with retry tokens
				if retryResult != nil {
					result.TokensInput += retryResult.TokensInput
					result.TokensOutput += retryResult.TokensOutput
					result.TokensTotal = result.TokensInput + result.TokensOutput
				}

				// Check again after retry
				commitCount, _ = git.CountNewCommits(ctx, baseBranch)
				if commitCount == 0 {
					result.Success = false

					// GH-2777: Collect the last assistant text from the retry response
					// so we can check for an explicit DECLINED marker first.
					refusal := ""
					if backendResult != nil {
						refusal = strings.TrimSpace(backendResult.LastAssistantText)
					}
					if retryResult != nil && strings.TrimSpace(retryResult.LastAssistantText) != "" {
						refusal = strings.TrimSpace(retryResult.LastAssistantText)
					}

					// GH-2777: Check for an explicit DECLINED:<reason> marker before
					// classifying as a generic no_changes failure. DECLINED avoids
					// pilot-failed and instead adds pilot-needs-clarification.
					if declinedReason, ok := parseDeclinedReason(refusal); ok {
						result.Declined = true
						result.DeclinedReason = declinedReason
						result.Outcome = "declined" // TASK-358
						if backendResult != nil {
							backendResult.ErrorType = string(ErrorTypeDeclined)
						}
						log.Warn("Task declined by executor",
							slog.String("task_id", task.ID),
							slog.String("reason", declinedReason),
						)
						r.reportProgress(task.ID, "Declined", 100, "Task declined: "+declinedReason)
						r.persistBackendDiagnostics(task.ID, backendResult)

						if recorder != nil {
							recorder.SetModel(result.ModelName)
							recorder.SetNavigator(state.hasNavigator)
							if finErr := recorder.Finish("declined"); finErr != nil {
								log.Warn("Failed to finish recording", slog.Any("error", finErr))
							}
						}
						return result, nil
					}

					// GH-2328: classify this as ErrorTypeNoChanges and carry the
					// final assistant message so the failure comment surfaces the
					// refusal reason instead of a generic "no changes" string.
					result.Outcome = "no_op" // TASK-358: no edits made, not a code failure
					if refusal != "" {
						result.Error = fmt.Sprintf("no_changes: Claude completed but made no code changes after retry — %s", refusal)
					} else {
						result.Error = "no_changes: Claude completed but made no code changes after retry"
					}
					if backendResult != nil {
						backendResult.ErrorType = string(ErrorTypeNoChanges)
						if refusal != "" {
							backendResult.LastAssistantText = refusal
						}
					}
					log.Error("No commits after retry",
						slog.String("task_id", task.ID),
					)
					r.reportProgress(task.ID, "Failed", 100, result.Error)

					// GH-2328: persist no_changes classification + refusal text.
					r.persistBackendDiagnostics(task.ID, backendResult)

					// Emit task failed event
					r.emitAlertEvent(AlertEvent{
						Type:      AlertEventTypeTaskFailed,
						TaskID:    task.ID,
						TaskTitle: task.Title,
						Project:   task.ProjectPath,
						Error:     result.Error,
						Metadata: map[string]string{
							"reason": "no_commits_after_retry",
						},
						Timestamp: time.Now(),
					})

					// Finish recording with failed status
					if recorder != nil {
						recorder.SetModel(result.ModelName)
						recorder.SetNavigator(state.hasNavigator)
						if finErr := recorder.Finish("no_commits"); finErr != nil {
							log.Warn("Failed to finish recording", slog.Any("error", finErr))
						}
					}
					return result, nil
				} else if retryErr != nil {
					log.Warn("Retry execution error (but commits exist)",
						slog.String("task_id", task.ID),
						slog.Any("error", retryErr),
						slog.Int("commit_count", commitCount),
					)
				}

				log.Info("Retry successful - commits detected",
					slog.String("task_id", task.ID),
					slog.Int("commit_count", commitCount),
				)
				r.reportProgress(task.ID, "Retry Success", 92, fmt.Sprintf("Retry successful: %d commits", commitCount))

				// Update commit SHA from retry if state captured it
				if len(state.commitSHAs) > 0 {
					result.CommitSHA = state.commitSHAs[len(state.commitSHAs)-1]
				} else if sha, shaErr := git.GetCurrentCommitSHA(ctx); shaErr == nil {
					result.CommitSHA = sha
				}
			}
		}

		// Auto-enable minimal build gate if not configured (GH-363)
		// This ensures broken code never becomes a PR, even without explicit quality config
		if r.qualityCheckerFactory == nil {
			buildCmd := quality.DetectBuildCommand(executionPath)
			testCmd := quality.DetectTestCommand(executionPath)
			if buildCmd != "" {
				log.Info("Auto-enabling build gate (no quality config)",
					slog.String("command", buildCmd),
				)

				// Create minimal quality checker with auto-detected build command
				minimalConfig := quality.MinimalBuildGate()
				minimalConfig.Gates[0].Command = buildCmd

				// GH-2398: also auto-enable a test gate when a test runner is
				// detectable. Empty testCmd → skip the gate entirely instead of
				// failing it on workspaces that lack a Makefile / test harness.
				if testCmd != "" {
					log.Info("Auto-enabling test gate", slog.String("command", testCmd))
					minimalConfig.Gates = append(minimalConfig.Gates, &quality.Gate{
						Name:        "test",
						Type:        quality.GateTest,
						Command:     testCmd,
						Required:    true,
						Timeout:     5 * time.Minute,
						MaxRetries:  1,
						RetryDelay:  3 * time.Second,
						FailureHint: "Fix failing tests in the changed files",
					})
				}

				r.qualityCheckerFactory = func(taskID, projectPath string) QualityChecker {
					return &simpleQualityChecker{
						config:      minimalConfig,
						projectPath: projectPath,
						taskID:      taskID,
					}
				}
			}
		}

		// Track if quality gates passed for self-review decision (GH-1079)
		qualityGatesPassed := false

		// Run quality gates if configured.
		// Previously skipped in LocalMode (v25 OOM concern), re-enabled since
		// deps are now pre-installed and gate runs pytest only (bounded cost).
		if r.qualityCheckerFactory != nil {
			const maxAutoRetries = 2 // Circuit breaker to prevent infinite loops

			// Track quality gate results across retries (GH-209)
			var finalOutcome *QualityOutcome
			var totalQualityRetries int

			for retryAttempt := 0; retryAttempt <= maxAutoRetries; retryAttempt++ {
				r.reportProgress(task.ID, "Quality Gates", 91, "Running quality checks...")
				r.saveLogEntry(task.ID, "info", "Running tests...")

				checker := r.qualityCheckerFactory(task.ID, executionPath)
				outcome, qErr := checker.Check(ctx)
				if qErr != nil {
					log.Error("Quality gate check error", slog.Any("error", qErr))
					result.Success = false
					result.Error = fmt.Sprintf("quality gate error: %v", qErr)
					r.reportProgress(task.ID, "Quality Failed", 100, result.Error)

					// Emit task failed event
					r.emitAlertEvent(AlertEvent{
						Type:      AlertEventTypeTaskFailed,
						TaskID:    task.ID,
						TaskTitle: task.Title,
						Project:   task.ProjectPath,
						Error:     result.Error,
						Timestamp: time.Now(),
					})

					// Dispatch webhook for task failed
					r.dispatchWebhook(ctx, webhooks.EventTaskFailed, webhooks.TaskFailedData{
						TaskID:   task.ID,
						Title:    task.Title,
						Project:  task.ProjectPath,
						Duration: time.Since(start),
						Error:    result.Error,
						Phase:    "Quality Gates",
					})

					if recorder != nil {
						recorder.SetModel(result.ModelName)
						recorder.SetNavigator(state.hasNavigator)
						if finErr := recorder.Finish("failed"); finErr != nil {
							log.Warn("Failed to finish recording", slog.Any("error", finErr))
						}
					}
					return result, nil
				}

				// Quality gates passed - exit retry loop
				if outcome.Passed {
					finalOutcome = outcome
					qualityGatesPassed = true
					r.reportProgress(task.ID, "Quality Passed", 94, "All quality gates passed")

					// Run simplification phase if enabled (GH-995)
					if r.config != nil && r.config.Simplification != nil && r.config.Simplification.Enabled {
						r.reportProgress(task.ID, "Simplifying", 95, "Simplifying code...")
						simplified, simplifyErr := SimplifyModifiedFiles(executionPath, r.config.Simplification)
						if simplifyErr != nil {
							log.Warn("Simplification error", slog.Any("error", simplifyErr))
							// Continue anyway - simplification is advisory
						} else if len(simplified) > 0 {
							log.Info("Simplified files", slog.Int("count", len(simplified)), slog.Any("files", simplified))
						}
					}

					// Note: Self-review now runs in parallel with intent judge after quality gates (GH-1079)

					break
				}
				// Track this outcome for potential failure reporting
				finalOutcome = outcome

				// Quality gates failed
				log.Warn("Quality gates failed",
					slog.Bool("should_retry", outcome.ShouldRetry),
					slog.Int("attempt", outcome.Attempt),
					slog.Int("retry_attempt", retryAttempt),
				)

				// Check if we should retry with Claude Code
				if outcome.ShouldRetry && retryAttempt < maxAutoRetries {
					totalQualityRetries++ // Track total retries across all gates (GH-209)
					r.reportProgress(task.ID, "Quality Retry", 92,
						fmt.Sprintf("Fixing issues (attempt %d/%d)...", retryAttempt+1, maxAutoRetries))

					// GH-1066: Record correction for drift detection
					if r.driftDetector != nil {
						r.driftDetector.RecordCorrection("quality_gate_retry", fmt.Sprintf("Quality gate failure: %s, Retry attempt: %d", outcome.RetryFeedback, retryAttempt+1))
					}

					// Emit retry event
					r.emitAlertEvent(AlertEvent{
						Type:      AlertEventTypeTaskRetry,
						TaskID:    task.ID,
						TaskTitle: task.Title,
						Project:   task.ProjectPath,
						Metadata: map[string]string{
							"attempt":  strconv.Itoa(retryAttempt + 1),
							"feedback": truncateText(outcome.RetryFeedback, 500),
						},
						Timestamp: time.Now(),
					})

					// Build retry prompt with feedback
					retryPrompt := r.buildRetryPrompt(task, outcome.RetryFeedback, retryAttempt+1)

					log.Info("Re-invoking Claude Code with retry feedback",
						slog.String("task_id", task.ID),
						slog.Int("retry_attempt", retryAttempt+1),
					)

					// Re-invoke backend with retry prompt
					feedbackAllowed, feedbackMCP := r.executionToolOptions()
					retryResult, retryErr := r.backend.Execute(ctx, ExecuteOptions{
						Prompt:        retryPrompt,
						ProjectPath:   task.ProjectPath,
						Verbose:       task.Verbose,
						Model:         selectedModel,
						Effort:        selectedEffort,
						AllowedTools:  feedbackAllowed,
						MCPConfigPath: feedbackMCP,
						EventHandler: func(event BackendEvent) {
							if recorder != nil {
								if recErr := recorder.RecordEvent(event.Raw); recErr != nil {
									log.Warn("Failed to record retry event", slog.Any("error", recErr))
								}
							}
							r.processBackendEvent(task.ID, event, state)
						},
					})

					if retryErr != nil {
						result.Success = false

						// GH-917: Check for classified backend error types in retry
						alertType := AlertEventTypeTaskFailed
						errorCategory := "unknown"

						if beErr, ok := retryErr.(BackendError); ok {
							result.Error = fmt.Sprintf("retry execution failed: %v", beErr)

							switch beErr.ErrorType() {
							case "rate_limit":
								alertType = AlertEventTypeRateLimit
								errorCategory = "rate_limit"
								log.Warn("Retry hit rate limit",
									slog.String("task_id", task.ID),
									slog.Int("retry_attempt", retryAttempt+1),
								)
								r.reportProgress(task.ID, "Rate Limited", 100, "Retry hit rate limit")
							case "invalid_config":
								alertType = AlertEventTypeConfigError
								errorCategory = "invalid_config"
								log.Error("Retry failed: invalid config", slog.String("message", beErr.ErrorMessage()))
								r.reportProgress(task.ID, "Config Error", 100, beErr.ErrorMessage())
							case "api_error":
								alertType = AlertEventTypeAPIError
								errorCategory = "api_error"
								log.Error("Retry failed: API error", slog.String("message", beErr.ErrorMessage()))
								r.reportProgress(task.ID, "API Error", 100, beErr.ErrorMessage())
							case "oom_killed":
								// GH-2332: surface OOM kills distinctly in the retry path too.
								alertType = AlertEventTypeOOMKilled
								errorCategory = "oom_killed"
								log.Error("Retry failed: OOM-killed", slog.String("message", beErr.ErrorMessage()))
								r.reportProgress(task.ID, "OOM Killed", 100, beErr.ErrorMessage())
							default:
								log.Error("Retry execution failed", slog.Any("error", retryErr))
								r.reportProgress(task.ID, "Retry Failed", 100, result.Error)
							}
						} else {
							result.Error = fmt.Sprintf("retry execution failed: %v", retryErr)
							log.Error("Retry execution failed", slog.Any("error", retryErr))
							r.reportProgress(task.ID, "Retry Failed", 100, result.Error)
						}

						r.emitAlertEvent(AlertEvent{
							Type:      alertType,
							TaskID:    task.ID,
							TaskTitle: task.Title,
							Project:   task.ProjectPath,
							Error:     result.Error,
							Metadata: map[string]string{
								"error_category": errorCategory,
								"phase":          "quality_retry",
							},
							Timestamp: time.Now(),
						})

						// Dispatch webhook for task failed
						r.dispatchWebhook(ctx, webhooks.EventTaskFailed, webhooks.TaskFailedData{
							TaskID:   task.ID,
							Title:    task.Title,
							Project:  task.ProjectPath,
							Duration: time.Since(start),
							Error:    result.Error,
							Phase:    "Quality Retry",
						})

						if recorder != nil {
							recorder.SetModel(result.ModelName)
							recorder.SetNavigator(state.hasNavigator)
							if finErr := recorder.Finish("failed"); finErr != nil {
								log.Warn("Failed to finish recording", slog.Any("error", finErr))
							}
						}
						return result, nil
					}

					// Update result with retry execution stats
					result.TokensInput += retryResult.TokensInput
					result.TokensOutput += retryResult.TokensOutput
					result.TokensTotal = result.TokensInput + result.TokensOutput
					if retryResult.Model != "" {
						result.ModelName = retryResult.Model
					}

					// Extract new commit SHA if any
					if len(state.commitSHAs) > 0 {
						result.CommitSHA = state.commitSHAs[len(state.commitSHAs)-1]
					}

					// Continue to next iteration to re-check quality gates
					r.reportProgress(task.ID, "Re-testing", 93, "Re-running quality gates...")
					continue
				}

				// No more retries allowed - fail the task
				result.Success = false
				if retryAttempt >= maxAutoRetries {
					result.Error = fmt.Sprintf("quality gates failed after %d auto-retries", maxAutoRetries)
				} else {
					result.Error = "quality gates failed, max retries exhausted"
				}

				r.reportProgress(task.ID, "Quality Failed", 100, "Quality gates did not pass")

				// Emit task failed event
				r.emitAlertEvent(AlertEvent{
					Type:      AlertEventTypeTaskFailed,
					TaskID:    task.ID,
					TaskTitle: task.Title,
					Project:   task.ProjectPath,
					Error:     result.Error,
					Timestamp: time.Now(),
				})

				// Dispatch webhook for task failed
				r.dispatchWebhook(ctx, webhooks.EventTaskFailed, webhooks.TaskFailedData{
					TaskID:   task.ID,
					Title:    task.Title,
					Project:  task.ProjectPath,
					Duration: time.Since(start),
					Error:    result.Error,
					Phase:    "Quality Gates",
				})

				if recorder != nil {
					recorder.SetModel(result.ModelName)
					recorder.SetNavigator(state.hasNavigator)
					if finErr := recorder.Finish("failed"); finErr != nil {
						log.Warn("Failed to finish recording", slog.Any("error", finErr))
					}
				}
				return result, nil
			}

			// Populate quality gate results in ExecutionResult (GH-209)
			if finalOutcome != nil {
				result.QualityGates = r.buildQualityGatesResult(finalOutcome, totalQualityRetries)
			}
		}

		r.reportProgress(task.ID, "Finalizing", 95, "Preparing for completion")

		// Warn if PR creation requested but quality gates not configured (GH-248)
		if task.CreatePR && r.qualityCheckerFactory == nil {
			log.Warn("quality gates not configured - PR created without local validation",
				slog.String("task_id", task.ID),
				slog.String("project", task.ProjectPath),
			)
		}

		// GH-1079: Run self-review and intent judge in parallel (saves 2-5 min per task)
		// Both are independent read-only operations:
		// - Self-review checks code quality (syntax, wiring, style)
		// - Intent judge verifies diff matches issue intent
		var intentVerdict *JudgeVerdict
		var intentErr error
		var intentDiff string
		var intentBaseBranch string

		// Determine if intent judge should run
		runIntentJudge := r.intentJudge != nil && task.CreatePR && !task.DirectCommit && task.Branch != ""

		// Log skip reasons for intent judge
		if r.intentJudge == nil {
			log.Debug("Intent judge skipped: not initialized")
		} else if !task.CreatePR {
			log.Debug("Intent judge skipped: CreatePR=false")
		} else if task.DirectCommit {
			log.Debug("Intent judge skipped: DirectCommit=true")
		} else if task.Branch == "" {
			log.Debug("Intent judge skipped: no branch")
		}

		// Get diff before parallel execution (needed for intent judge)
		if runIntentJudge {
			intentBaseBranch = task.BaseBranch
			if intentBaseBranch == "" {
				intentBaseBranch, _ = git.GetDefaultBranch(ctx)
				if intentBaseBranch == "" {
					intentBaseBranch = "main"
				}
			}
			intentDiff, intentErr = git.GetDiff(ctx, intentBaseBranch)
			if intentErr != nil {
				log.Warn("Intent judge skipped: failed to get diff",
					slog.String("task_id", task.ID),
					slog.Any("error", intentErr),
				)
				runIntentJudge = false
			} else if intentDiff == "" {
				runIntentJudge = false
			}
		}

		// Determine if self-review should run:
		// 1. Quality gates configured AND passed
		// 2. OR quality gates not configured AND CreatePR=true (GH-364)
		runSelfReview := qualityGatesPassed || (r.qualityCheckerFactory == nil && task.CreatePR)

		// Run self-review and intent judge in parallel
		var wg sync.WaitGroup
		var selfReviewErr error

		if runSelfReview {
			r.saveLogEntry(task.ID, "info", "Running self-review...")
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := r.runSelfReview(ctx, task, state); err != nil {
					selfReviewErr = err
				}
			}()
		}

		if runIntentJudge {
			wg.Add(1)
			go func() {
				defer wg.Done()
				log.Info("Intent judge running",
					slog.String("task_id", task.ID),
					slog.Int("diff_len", len(intentDiff)),
				)
				r.reportProgress(task.ID, "Intent Check", 96, "Verifying diff matches intent...")
				intentVerdict, intentErr = r.intentJudge.Judge(ctx, task.Title, task.Description, intentDiff)
			}()
		}

		wg.Wait()

		// Handle self-review result
		if runSelfReview && selfReviewErr != nil {
			log.Warn("Self-review error", slog.Any("error", selfReviewErr))
			// Continue anyway - self-review is advisory
		}

		// Handle intent judge result
		if runIntentJudge {
			if intentErr != nil {
				log.Warn("Intent judge error (continuing to PR)",
					slog.String("task_id", task.ID),
					slog.Any("error", intentErr),
				)
			} else if intentVerdict != nil && !intentVerdict.Passed {
				log.Warn("Intent judge vetoed diff",
					slog.String("task_id", task.ID),
					slog.String("reason", intentVerdict.Reason),
					slog.Float64("confidence", intentVerdict.Confidence),
				)

				if !state.intentRetried {
					state.intentRetried = true
					r.reportProgress(task.ID, "Intent Retry", 80, "Retrying with intent feedback...")

					// Re-anchor the retry on the original acceptance criteria so the
					// fix is judged against the same target as the first attempt
					// instead of drifting toward the veto reason alone.
					var acSection string
					if len(task.AcceptanceCriteria) > 0 {
						var acb strings.Builder
						acb.WriteString("\n\n## Acceptance Criteria\n\n")
						for _, ac := range task.AcceptanceCriteria {
							acb.WriteString(fmt.Sprintf("- [ ] %s\n", ac))
						}
						acSection = acb.String()
					}

					// Re-inject persisted constraints/decisions from the run doc so
					// the retry stays anchored to context captured earlier in the run.
					var docSection string
					if runDoc, docErr := loadRunDoc(agentPath, task.ID); docErr == nil && runDoc != nil {
						var db strings.Builder
						if len(runDoc.Constraints) > 0 {
							db.WriteString("\n\n## Constraints\n\n")
							for _, c := range runDoc.Constraints {
								db.WriteString(fmt.Sprintf("- %s\n", c))
							}
						}
						if len(runDoc.DecisionLog) > 0 {
							db.WriteString("\n\n## Prior Decisions\n\n")
							for _, d := range runDoc.DecisionLog {
								db.WriteString(fmt.Sprintf("- %s — %s\n", d.Decision, d.Reasoning))
							}
						}
						docSection = db.String()
					}

					retryPrompt := fmt.Sprintf(
						"## Intent Alignment Retry\n\nThe intent judge flagged the previous implementation:\n\n**Reason:** %s\n\nPlease fix the issues above. Focus on implementing exactly what the issue asks for.\n\n## Original Task: %s\n\n%s%s%s",
						intentVerdict.Reason, task.Title, task.Description, acSection, docSection,
					)

					intentAllowed, intentMCP := r.executionToolOptions()
					_, retryErr := r.backend.Execute(ctx, ExecuteOptions{
						Prompt:        retryPrompt,
						ProjectPath:   task.ProjectPath,
						Verbose:       task.Verbose,
						Model:         selectedModel,
						Effort:        selectedEffort,
						AllowedTools:  intentAllowed,
						MCPConfigPath: intentMCP,
						EventHandler: func(event BackendEvent) {
							state.tokensInput += event.TokensInput
							state.tokensOutput += event.TokensOutput
							state.cacheCreationInputTokens += event.CacheCreationInputTokens
							state.cacheReadInputTokens += event.CacheReadInputTokens
							if event.Type == EventTypeToolResult && event.ToolResult != "" {
								extractCommitSHA(event.ToolResult, state)
							}
						},
					})

					if retryErr == nil {
						// Update result tokens
						result.TokensInput = state.tokensInput
						result.TokensOutput = state.tokensOutput
						result.TokensTotal = state.tokensInput + state.tokensOutput

						// Re-judge the new diff
						newDiff, _ := git.GetDiff(ctx, intentBaseBranch)
						if newDiff != "" {
							v2, _ := r.intentJudge.Judge(ctx, task.Title, task.Description, newDiff)
							if v2 != nil && !v2.Passed {
								result.IntentWarning = v2.Reason
							}
						}
					} else {
						result.IntentWarning = intentVerdict.Reason
					}
				} else {
					result.IntentWarning = intentVerdict.Reason
				}
			}
		}

		// Handle direct commit mode: push directly to main

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
		if task.DirectCommit {
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

		// GH-1599: Log task completed milestone
		r.saveLogEntry(task.ID, "info", "Task completed successfully")

		// Emit task completed event
		r.emitAlertEvent(AlertEvent{
			Type:      AlertEventTypeTaskCompleted,
			TaskID:    task.ID,
			TaskTitle: task.Title,
			Project:   task.ProjectPath,
			Metadata: map[string]string{
				"duration_ms": fmt.Sprintf("%d", duration.Milliseconds()),
				"pr_url":      result.PRUrl,
			},
			Timestamp: time.Now(),
		})

		// Dispatch webhook for task completed
		r.dispatchWebhook(ctx, webhooks.EventTaskCompleted, webhooks.TaskCompletedData{
			TaskID:    task.ID,
			Title:     task.Title,
			Project:   task.ProjectPath,
			Duration:  duration,
			PRCreated: result.PRUrl != "",
			PRURL:     result.PRUrl,
		})

		// Finish recording with completed status
		if recorder != nil {
			recorder.SetCommitSHA(result.CommitSHA)
			recorder.SetModel(result.ModelName)
			recorder.SetNavigator(state.hasNavigator)
			if finErr := recorder.Finish("completed"); finErr != nil {
				log.Warn("Failed to finish recording", slog.Any("error", finErr))
			} else {
				log.Info("Recording saved", slog.String("recording_id", recorder.GetRecordingID()))
			}
		}

		// Sync Navigator index (GH-57) - update DEVELOPMENT-README.md
		if state.hasNavigator {
			if syncErr := r.syncNavigatorIndex(task, "completed", executionPath); syncErr != nil {
				log.Warn("Failed to sync Navigator index", slog.Any("error", syncErr))
			}

			// GH-1063: Archive completed task documentation
			agentPath := filepath.Join(executionPath, ".agent")
			if archiveErr := ArchiveTaskDoc(agentPath, task.ID); archiveErr != nil {
				log.Warn("Failed to archive task documentation", slog.Any("error", archiveErr))
			}

			// GH-1388: Update feature matrix for feature tasks
			if strings.HasPrefix(strings.ToLower(task.Title), "feat(") {
				ver := "unknown"
				if r.config != nil && r.config.Version != "" {
					ver = r.config.Version
				}
				if fmErr := UpdateFeatureMatrix(agentPath, task, ver); fmErr != nil {
					log.Warn("Failed to update feature matrix", slog.Any("error", fmErr))
				}
			}

			// GH-1064: Create context marker for completed task
			marker := &ContextMarker{
				Name:        fmt.Sprintf("task-completed-%s", task.ID),
				Description: fmt.Sprintf("Task completed: %s", task.Title),
				TaskID:      task.ID,
				CurrentFocus: fmt.Sprintf("Completed %s. %d files changed, %d lines added, %d removed. Cost: $%.2f.",
					task.Title, result.FilesChanged, result.LinesAdded, result.LinesRemoved,
					result.EstimatedCostUSD),
			}

			// Add modified files list (GH-1388)
			if len(state.modifiedFiles) > 0 {
				marker.CurrentFocus += fmt.Sprintf(" Modified: %s.", strings.Join(state.modifiedFiles, ", "))
			}

			// Add commit SHA and PR info if available
			if result.CommitSHA != "" {
				marker.Commits = append(marker.Commits, result.CommitSHA)
			}
			if result.PRUrl != "" {
				marker.CurrentFocus += fmt.Sprintf(" PR: %s", result.PRUrl)
			}

			if createMarkerErr := CreateMarker(agentPath, marker); createMarkerErr != nil {
				log.Warn("Failed to create completion marker", slog.Any("error", createMarkerErr))
			} else {
				log.Debug("Created completion context marker", slog.String("marker_path", marker.FilePath))
			}
		}

		// GH-1018: Sync main branch with origin after task completion
		// This prevents local/remote divergence over time
		if r.config != nil && r.config.SyncMainAfterTask {
			if syncErr := r.syncMainBranch(ctx, task.ProjectPath); syncErr != nil {
				log.Warn("Failed to sync main branch", slog.Any("error", syncErr))
			}
		}

		// GH-1065: Store experiential memory after successful task completion
		if r.knowledge != nil {
			projectID := "pilot" // Default fallback
			if task.ProjectPath != "" {
				projectID = filepath.Base(task.ProjectPath)
			}

			// Enrich content with execution metrics
			content := fmt.Sprintf(
				"Completed %s: %s. Modified %d files. Duration: %v. Model: %s.",
				task.ID, task.Title, result.FilesChanged, duration, result.ModelName,
			)
			if result.IntentWarning != "" {
				content += fmt.Sprintf(" Intent warning: %s.", result.IntentWarning)
			}

			// Enrich context with branch, PR URL, and cost
			contextStr := fmt.Sprintf("Branch: %s, Cost: $%.2f",
				task.Branch, result.EstimatedCostUSD)
			if result.PRUrl != "" {
				contextStr += fmt.Sprintf(", PR: %s", result.PRUrl)
			}

			memory := &memory.Memory{
				Type:       memory.MemoryTypeLearning,
				Content:    content,
				Context:    contextStr,
				Confidence: 1.0,
				ProjectID:  projectID,
			}

			if addErr := r.knowledge.AddMemory(memory); addErr != nil {
				log.Warn("Failed to store task completion memory", slog.Any("error", addErr))
			} else {
				log.Debug("Stored task completion memory", slog.String("task_id", task.ID))
			}
		}
	}

	// GH-1813: Record execution outcome for pattern learning (self-improvement)
	r.recordLearning(ctx, task, result)

	// GH-2015: Record execution into knowledge graph for cross-project learnings
	r.recordGraphLearning(task, result)

	// GH-1991: Record outcome for model routing escalation
	r.recordOutcome(task, result, complexity, duration)

	return result, nil
}

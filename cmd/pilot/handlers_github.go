package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/alerts"
	"github.com/ylcn91/pilot/internal/budget"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/logging"
)

// handleGitHubIssueWithResult processes a GitHub issue and returns result with PR info
// Used in sequential mode to enable PR merge waiting
// sourceRepo is the "owner/repo" string that the issue came from (GH-929)
func handleGitHubIssueWithResult(ctx context.Context, cfg *config.Config, client *github.Client, issue *github.Issue, projectPath string, sourceRepo string, dispatcher *executor.Dispatcher, runner *executor.Runner, monitor *executor.Monitor, program *tea.Program, alertsEngine *alerts.Engine, enforcer *budget.Enforcer) (*github.IssueResult, error) {
	taskID := fmt.Sprintf("GH-%d", issue.Number)

	// GH-1853: Construct board sync for GitHub Projects V2 status transitions.
	// boardSync is nil when project_board config is missing or disabled — syncBoardStatus handles nil safely.
	var boardSync *github.ProjectBoardSync
	if cfg.Adapters.GitHub.ProjectBoard != nil && cfg.Adapters.GitHub.ProjectBoard.Enabled {
		parts := strings.Split(cfg.Adapters.GitHub.Repo, "/")
		if len(parts) == 2 && parts[0] != "" {
			boardSync = github.NewProjectBoardSync(client, cfg.Adapters.GitHub.ProjectBoard, parts[0])
		} else {
			slog.Warn("board sync disabled: invalid repo format, expected owner/repo", "repo", cfg.Adapters.GitHub.Repo)
		}
	}

	// GH-386: Pre-execution validation - fail fast if repo doesn't match project
	if err := executor.ValidateRepoProjectMatch(sourceRepo, projectPath); err != nil {
		logging.WithComponent("github").Error("cross-project execution blocked",
			slog.Any("error", err),
			slog.Int("issue_number", issue.Number),
			slog.String("repo", sourceRepo),
			slog.String("project_path", projectPath),
		)
		wrappedErr := fmt.Errorf("cross-project execution blocked: %w", err)
		return &github.IssueResult{
			Success: false,
			Error:   wrappedErr,
		}, wrappedErr
	}

	taskDesc := fmt.Sprintf("GitHub Issue #%d: %s\n\n%s", issue.Number, issue.Title, issue.Body)
	branchName := fmt.Sprintf("pilot/%s", taskID)

	// GH-489: For autopilot-fix issues, reuse the original branch so the fix
	// lands on the same branch as the failed PR (not a new branch).
	// GH-1267: Also extract PR number for --from-pr session resumption.
	var fromPR int
	for _, label := range issue.Labels {
		if label.Name == "autopilot-fix" {
			if parsed := parseAutopilotBranch(issue.Body); parsed != "" {
				branchName = parsed
				slog.Info("using original branch from autopilot-fix metadata",
					slog.String("branch", branchName),
					slog.Int("issue", issue.Number),
				)
			}
			// GH-1267: Extract PR number for session resumption
			if pr := parseAutopilotPR(issue.Body); pr > 0 {
				fromPR = pr
				slog.Info("extracted PR number from autopilot-fix metadata",
					slog.Int("pr", fromPR),
					slog.Int("issue", issue.Number),
				)
			}
			break
		}
	}

	// Always create branches and PRs - required for autopilot workflow
	// GH-386: Include SourceRepo for cross-project validation in executor
	// GH-920: Extract acceptance criteria for prompt inclusion
	// GH-1267: Include FromPR for --from-pr session resumption
	labels := extractGitHubLabelNames(issue)

	slog.Info("Task labels extracted",
		slog.String("task_id", taskID),
		slog.Any("labels", labels),
		slog.Int("label_count", len(issue.Labels)),
	)

	task := &executor.Task{
		ID:                 taskID,
		Title:              issue.Title,
		Description:        taskDesc,
		ProjectPath:        projectPath,
		Branch:             branchName,
		CreatePR:           true,
		SourceRepo:         sourceRepo,
		MemberID:           resolveGitHubMemberID(issue),                 // GH-634: RBAC lookup
		Labels:             labels,                                       // GH-727: flow labels for complexity classifier
		AcceptanceCriteria: github.ExtractAcceptanceCriteria(issue.Body), // GH-920: acceptance criteria in prompts
		FromPR:             fromPR,                                       // GH-1267: session resumption from PR context
		// GH-2290: honor project.default_branch / branch_from so branching and PR target
		// follow the configured integration branch (e.g. `dev` in main → dev → feature).
		BaseBranch: cfg.FindProjectByRepo(sourceRepo).ResolveBaseBranch(),
		// Propagate parent state so isParentDone() can refuse sub-issue creation
		// when the daemon re-dispatches a closed/merged parent. Without this, an
		// empty State + empty Labels combo bypasses the gate at epic.go and
		// permits spurious sub-issue spawning (GH-201 OAuth dispatch loop).
		State: issue.State,
	}

	parts := strings.Split(sourceRepo, "/")

	// GH-2619: Pre-dispatch spec quality gate — block issues whose bodies are too thin to execute.
	if len(parts) == 2 {
		parentResolver := func(parentNum int) (*github.Issue, error) {
			return client.GetIssue(ctx, parts[0], parts[1], parentNum)
		}
		specResult := github.ValidateSpec(issue, parentResolver)
		if !specResult.Valid && specResult.SkipReason == "" {
			applySpecGuard(ctx, client, parts[0], parts[1], issue, specResult.FailureReasons)
			return &github.IssueResult{Success: false}, nil
		}
	}

	// Add pilot-in-progress label before execution begins
	if len(parts) == 2 {
		if err := client.AddLabels(ctx, parts[0], parts[1], issue.Number, []string{github.LabelInProgress}); err != nil {
			logGitHubAPIError("AddLabels", parts[0], parts[1], issue.Number, err)
		}
	}

	// GH-1853: Move issue to "In Progress" column on project board
	syncBoardStatus(ctx, boardSync, issue.NodeID, cfg.Adapters.GitHub.ProjectBoard.GetStatuses().InProgress)

	deps := HandlerDeps{
		Cfg:          cfg,
		Dispatcher:   dispatcher,
		Runner:       runner,
		Monitor:      monitor,
		Program:      program,
		AlertsEngine: alertsEngine,
		Enforcer:     enforcer,
		ProjectPath:  projectPath,
	}
	info := IssueInfo{
		TaskID:   taskID,
		Title:    issue.Title,
		URL:      issue.HTMLURL,
		Adapter:  "github",
		LogEmoji: "📥",
	}

	// Note: monitor.Start() is NOT called here — it's called by runner.executeWithOptions()
	// when execution actually begins, enabling accurate queued→running dashboard transitions.
	hr, execErr := handleIssueGeneric(ctx, deps, info, task)

	// Build the issue result. GH-3270: when the outer error is nil but the executor
	// recorded a failure string (e.g. "no new commit produced"), surface it as the
	// IssueResult.Error so the poller can call IsPermanentFailure on it.
	issueErr := hr.Error
	if issueErr == nil && hr.Result != nil && hr.Result.Error != "" {
		issueErr = fmt.Errorf("%s", hr.Result.Error)
	}
	issueResult := &github.IssueResult{
		Success:    hr.Success,
		BranchName: hr.BranchName,
		PRNumber:   hr.PRNumber,
		PRURL:      hr.PRURL,
		HeadSHA:    hr.HeadSHA,
		Error:      issueErr,
	}

	// Post-execution: label management, close issue, add rich execution comment
	if len(parts) == 2 {
		if err := client.RemoveLabel(ctx, parts[0], parts[1], issue.Number, github.LabelInProgress); err != nil {
			logGitHubAPIError("RemoveLabel", parts[0], parts[1], issue.Number, err)
		}

		// GH-1853: Resolve board statuses once for all paths (nil-safe via GetStatuses)
		boardStatuses := cfg.Adapters.GitHub.ProjectBoard.GetStatuses()

		if execErr != nil {
			// GH-2402: Classify deterministic failures (e.g. non-conventional title)
			// as pilot-blocked instead of pilot-failed so the poller stops retrying.
			failureLabel := github.LabelFailed
			if executor.IsPermanentFailure(execErr.Error()) {
				failureLabel = github.LabelBlocked
			}
			if err := client.AddLabels(ctx, parts[0], parts[1], issue.Number, []string{failureLabel}); err != nil {
				logGitHubAPIError("AddLabels", parts[0], parts[1], issue.Number, err)
			}
			syncBoardStatus(ctx, boardSync, issue.NodeID, boardStatuses.Failed) // GH-1853
			comment := fmt.Sprintf("❌ Pilot execution failed:\n\n```\n%s\n```", execErr.Error())
			if failureLabel == github.LabelBlocked {
				comment += "\n\nThis is a deterministic failure (`pilot-blocked`). Retries are paused — fix the underlying issue (e.g. rename to a conventional commit title) and remove the `pilot-blocked` label to resume."
			}
			if _, err := client.AddComment(ctx, parts[0], parts[1], issue.Number, comment); err != nil {
				logGitHubAPIError("AddComment", parts[0], parts[1], issue.Number, err)
			}
		} else if hr.Result != nil && hr.Result.Success {
			// Validate deliverables before marking as done.
			// GH-3053: skip for epic-parent results — sub-issues handle the work.
			if !hr.Result.IsEpic && hr.Result.CommitSHA == "" && hr.Result.PRUrl == "" {
				// No commits and no PR - mark as failed
				if err := client.AddLabels(ctx, parts[0], parts[1], issue.Number, []string{github.LabelFailed}); err != nil {
					logGitHubAPIError("AddLabels", parts[0], parts[1], issue.Number, err)
				}
				syncBoardStatus(ctx, boardSync, issue.NodeID, boardStatuses.Failed) // GH-1853
				comment := fmt.Sprintf("⚠️ Pilot execution completed but no changes were made.\n\n**Duration:** %s\n**Branch:** `%s`\n\nNo commits or PR were created. The task may need clarification or manual intervention.",
					hr.Result.Duration, branchName)
				if _, err := client.AddComment(ctx, parts[0], parts[1], issue.Number, comment); err != nil {
					logGitHubAPIError("AddComment", parts[0], parts[1], issue.Number, err)
				}
				// Update issueResult to reflect failure
				issueResult.Success = false
			} else {
				// Has deliverables — PR created, awaiting merge.
				// GH-3139/TASK-301: pilot-done + issue close are deferred to merge
				// (handleMerging in autopilot) so a later merge conflict cannot
				// ghost-close the issue before any code reaches main.
				// GH-1869: Move to Review column when PR is created
				if hr.PRNumber > 0 {
					syncBoardStatus(ctx, boardSync, issue.NodeID, boardStatuses.Review)

					// GH-2099: Auto-assign PR reviewers from project config
					requestReviewersFromConfig(ctx, cfg, client, sourceRepo, parts[0], parts[1], hr.PRNumber)
				}

				// GH-1302/GH-2402: Clean up stale pilot-failed label from prior failed attempt.
				// Removed unconditionally — the in-memory `issue` object is the snapshot
				// from dispatch and won't reflect labels added during execution
				// (e.g. by an earlier retry on the same poll cycle).
				if err := client.RemoveLabel(ctx, parts[0], parts[1], issue.Number, github.LabelFailed); err != nil {
					// 404 is expected if label doesn't exist — log at debug level
					slog.Debug("pilot-failed label cleanup", "issue", issue.Number, "error", err)
				}

				comment := buildExecutionComment(hr.Result, branchName)
				if _, err := client.AddComment(ctx, parts[0], parts[1], issue.Number, comment); err != nil {
					logGitHubAPIError("AddComment", parts[0], parts[1], issue.Number, err)
				}
			}
		} else if hr.Result != nil {
			// GH-2363: Title-guard escalation already posted its own structured
			// comment and added pilot-failed + pilot-title-rejected. Skip the
			// generic failure-comment path to avoid duplicate noise.
			if hr.Result.TitleRejected {
				syncBoardStatus(ctx, boardSync, issue.NodeID, boardStatuses.Failed)
			} else if hr.Result.Declined {
				// GH-2777: Claude explicitly declined the task as unactionable.
				// Add pilot-needs-clarification instead of pilot-failed so the poller
				// blocks without incrementing the retry counter.
				if err := client.RemoveLabel(ctx, parts[0], parts[1], issue.Number, github.LabelInProgress); err != nil {
					slog.Debug("pilot-in-progress removal (declined)", "issue", issue.Number, "error", err)
				}
				if err := client.AddLabels(ctx, parts[0], parts[1], issue.Number, []string{github.LabelNeedsClarification}); err != nil {
					logGitHubAPIError("AddLabels(needs-clarification)", parts[0], parts[1], issue.Number, err)
				}
				syncBoardStatus(ctx, boardSync, issue.NodeID, boardStatuses.Failed)
				reason := hr.Result.DeclinedReason
				if reason == "" {
					reason = "task could not be implemented as specified"
				}
				comment := fmt.Sprintf("🤔 **Pilot needs clarification before implementing this task**\n\n**Reason**: %s\n\nTo resume, clarify the requirements and remove the `%s` label.", reason, github.LabelNeedsClarification)
				if _, err := client.AddComment(ctx, parts[0], parts[1], issue.Number, comment); err != nil {
					logGitHubAPIError("AddComment(declined)", parts[0], parts[1], issue.Number, err)
				}
			} else if hr.Result.Error != "" && strings.Contains(hr.Result.Error, noOpErrorMarker) &&
				issueAlreadyMerged(ctx, client, parts[0], parts[1], issue.Number) {
				// TASK-321: a "no new commit produced" no-op is ambiguous — it can mean
				// the work was ALREADY merged to main (a re-dispatch of a shipped issue),
				// not a genuine failure. When a merged PR already exists for this issue,
				// the correct outcome is done+closed, NOT pilot-blocked. (A genuine
				// no-op with no merged PR falls through to the blocked path below.)
				if err := client.AddLabels(ctx, parts[0], parts[1], issue.Number, []string{github.LabelDone}); err != nil {
					logGitHubAPIError("AddLabels(done)", parts[0], parts[1], issue.Number, err)
				}
				if err := client.RemoveLabel(ctx, parts[0], parts[1], issue.Number, github.LabelBlocked); err != nil {
					slog.Debug("pilot-blocked cleanup (already-merged)", "issue", issue.Number, "error", err)
				}
				syncBoardStatus(ctx, boardSync, issue.NodeID, boardStatuses.Done)
				if err := client.UpdateIssueState(ctx, parts[0], parts[1], issue.Number, "closed"); err != nil {
					logGitHubAPIError("UpdateIssueState(closed)", parts[0], parts[1], issue.Number, err)
				}
				comment := "✅ No new commit was produced because the work for this issue is **already merged to `main`**. This was a re-dispatch of completed work, not a failure — closing as done. (TASK-321)"
				if _, err := client.AddComment(ctx, parts[0], parts[1], issue.Number, comment); err != nil {
					logGitHubAPIError("AddComment(already-merged)", parts[0], parts[1], issue.Number, err)
				}
				issueResult.Success = true
			} else if hr.Result.Error != "" && strings.Contains(hr.Result.Error, noOpErrorMarker) &&
				issueHasOpenPR(ctx, client, parts[0], parts[1], issue.Number) {
				// TASK-341: a "no new commit produced" no-op with an OPEN pilot PR is
				// the awaiting-merge window — pilot-done + close are deferred to merge
				// time (GH-3139/TASK-301), so a re-dispatch finds the work already on
				// the branch and produces this no-op even though a healthy PR is open.
				// This is a redundant re-dispatch of shipped work, NOT a failure: leave
				// it for the autopilot merge flow and do NOT add pilot-blocked. (The
				// already-merged case is handled by the branch above; a genuine no-op
				// with neither a merged nor an open PR falls through to the blocked
				// path below.) No comment is posted — the run can repeat before merge
				// and we must not spam the issue.
				slog.Info("no-op re-dispatch with open PR — awaiting merge, not blocked (TASK-341)",
					slog.Int("issue", issue.Number),
				)
				// Defense-in-depth: clear a stale pilot-blocked from a prior phantom
				// classification so the open PR is not held out of the merge flow.
				if err := client.RemoveLabel(ctx, parts[0], parts[1], issue.Number, github.LabelBlocked); err != nil {
					slog.Debug("pilot-blocked cleanup (awaiting-merge)", "issue", issue.Number, "error", err)
				}
				// issueResult.Success stays false (no new deliverable this run), but no
				// failure/blocked label is applied — the open PR is the deliverable.
			} else {
				// result exists but Success is false - mark as failed
				// GH-2402: Use pilot-blocked for deterministic failures so we don't retry.
				failureLabel := github.LabelFailed
				if hr.Result.Error != "" && executor.IsPermanentFailure(hr.Result.Error) {
					failureLabel = github.LabelBlocked
				}
				if err := client.AddLabels(ctx, parts[0], parts[1], issue.Number, []string{failureLabel}); err != nil {
					logGitHubAPIError("AddLabels", parts[0], parts[1], issue.Number, err)
				}
				syncBoardStatus(ctx, boardSync, issue.NodeID, boardStatuses.Failed) // GH-1853
				comment := buildFailureComment(hr.Result)
				if failureLabel == github.LabelBlocked {
					comment += "\n\nThis is a deterministic failure (`pilot-blocked`). Retries are paused — fix the underlying issue and remove the `pilot-blocked` label to resume."
				}
				if _, err := client.AddComment(ctx, parts[0], parts[1], issue.Number, comment); err != nil {
					logGitHubAPIError("AddComment", parts[0], parts[1], issue.Number, err)
				}
			}
		}
	}

	return issueResult, execErr
}

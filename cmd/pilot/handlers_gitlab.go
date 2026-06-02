package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ylcn91/pilot/internal/adapters/gitlab"
	"github.com/ylcn91/pilot/internal/alerts"
	"github.com/ylcn91/pilot/internal/budget"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/logging"
)

func handleGitLabIssueWithResult(ctx context.Context, cfg *config.Config, client *gitlab.Client, issue *gitlab.Issue, projectPath string, dispatcher *executor.Dispatcher, runner *executor.Runner, monitor *executor.Monitor, program *tea.Program, alertsEngine *alerts.Engine, enforcer *budget.Enforcer) (*gitlab.IssueResult, error) {
	taskID := fmt.Sprintf("GL-%d", issue.IID)
	branchName := fmt.Sprintf("pilot/%s", taskID)

	taskDesc := fmt.Sprintf("GitLab Issue %s: %s\n\n%s", taskID, issue.Title, issue.Description)

	task := &executor.Task{
		ID:            taskID,
		Title:         issue.Title,
		Description:   taskDesc,
		ProjectPath:   projectPath,
		Branch:        branchName,
		CreatePR:      true,
		SourceAdapter: "gitlab",
		SourceIssueID: fmt.Sprintf("%d", issue.IID),
		// GH-2290: honor project.default_branch / branch_from for GitLab MRs too —
		// this is the reporter's exact case (main → dev → feature).
		BaseBranch: cfg.FindProjectByPath(projectPath).ResolveBaseBranch(),
	}

	// Wire GitLab client as PRCreator so the runner creates MRs via
	// the GitLab API instead of the gh CLI.
	runner.SetPRCreator(client)

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
		URL:      issue.WebURL,
		Adapter:  "gitlab",
		LogEmoji: "🦊",
	}

	hr, execErr := handleIssueGeneric(ctx, deps, info, task)

	issueResult := &gitlab.IssueResult{
		Success:    hr.Success,
		BranchName: hr.BranchName,
		MRNumber:   hr.PRNumber,
		MRURL:      hr.PRURL,
		HeadSHA:    hr.HeadSHA,
		Error:      hr.Error,
	}

	if execErr != nil {
		note := fmt.Sprintf("❌ Pilot execution failed:\n\n%s", execErr.Error())
		if _, err := client.AddIssueNote(ctx, issue.IID, note); err != nil {
			logging.WithComponent("gitlab").Warn("Failed to add failure note",
				slog.Int("iid", issue.IID),
				slog.Any("error", err),
			)
		}
	} else if hr.Result != nil && hr.Result.Success {
		if !hr.Result.IsEpic && hr.Result.CommitSHA == "" && hr.Result.PRUrl == "" { // GH-3053
			note := fmt.Sprintf("⚠️ Pilot execution completed but no changes were made.\n\nDuration: %s\nBranch: %s\n\nNo commits or MR were created. The task may need clarification or manual intervention.",
				hr.Result.Duration, branchName)
			if _, err := client.AddIssueNote(ctx, issue.IID, note); err != nil {
				logging.WithComponent("gitlab").Warn("Failed to add note",
					slog.Int("iid", issue.IID),
					slog.Any("error", err),
				)
			}
			issueResult.Success = false
		} else {
			var parts []string
			parts = append(parts, "✅ Pilot execution completed successfully!")
			parts = append(parts, "")
			if hr.Result.PRUrl != "" {
				parts = append(parts, fmt.Sprintf("Merge Request: %s", hr.Result.PRUrl))
			}
			if hr.Result.CommitSHA != "" {
				parts = append(parts, fmt.Sprintf("Commit: %s", hr.Result.CommitSHA[:min(8, len(hr.Result.CommitSHA))]))
			}
			parts = append(parts, fmt.Sprintf("Branch: %s", branchName))
			parts = append(parts, fmt.Sprintf("Duration: %s", hr.Result.Duration))
			note := strings.Join(parts, "\n")
			if _, err := client.AddIssueNote(ctx, issue.IID, note); err != nil {
				logging.WithComponent("gitlab").Warn("Failed to add success note",
					slog.Int("iid", issue.IID),
					slog.Any("error", err),
				)
			}
		}
	} else if hr.Result != nil {
		note := fmt.Sprintf("❌ Pilot execution failed\n\nError: %s\nDuration: %s", hr.Result.Error, hr.Result.Duration)
		if _, err := client.AddIssueNote(ctx, issue.IID, note); err != nil {
			logging.WithComponent("gitlab").Warn("Failed to add failure note",
				slog.Int("iid", issue.IID),
				slog.Any("error", err),
			)
		}
	}

	return issueResult, execErr
}

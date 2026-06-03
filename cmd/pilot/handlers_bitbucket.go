package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ylcn91/pilot/internal/adapters/bitbucket"
	"github.com/ylcn91/pilot/internal/alerts"
	"github.com/ylcn91/pilot/internal/budget"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/logging"
)

func handleBitbucketIssueWithResult(ctx context.Context, cfg *config.Config, client *bitbucket.Client, issue *bitbucket.Issue, projectPath string, dispatcher *executor.Dispatcher, runner *executor.Runner, monitor *executor.Monitor, program *tea.Program, alertsEngine *alerts.Engine, enforcer *budget.Enforcer) (*bitbucket.IssueResult, error) {
	taskID := fmt.Sprintf("BB-%d", issue.ID)
	branchName := fmt.Sprintf("pilot/%s", taskID)

	description := ""
	if issue.Content != nil {
		description = issue.Content.Raw
	}
	taskDesc := fmt.Sprintf("Bitbucket Issue %s: %s\n\n%s", taskID, issue.Title, description)

	issueURL := ""
	if issue.Links != nil && issue.Links.HTML != nil {
		issueURL = issue.Links.HTML.Href
	}

	task := &executor.Task{
		ID:            taskID,
		Title:         issue.Title,
		Description:   taskDesc,
		ProjectPath:   projectPath,
		Branch:        branchName,
		CreatePR:      true,
		SourceAdapter: "bitbucket",
		SourceIssueID: fmt.Sprintf("%d", issue.ID),
		BaseBranch:    cfg.FindProjectByPath(projectPath).ResolveBaseBranch(),
	}

	// Wire Bitbucket client as PRCreator so the runner creates PRs via
	// the Bitbucket API instead of the gh CLI.
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
		URL:      issueURL,
		Adapter:  "bitbucket",
		LogEmoji: "🪣",
	}

	hr, execErr := handleIssueGeneric(ctx, deps, info, task)

	issueResult := &bitbucket.IssueResult{
		Success:    hr.Success,
		BranchName: hr.BranchName,
		PRNumber:   hr.PRNumber,
		PRURL:      hr.PRURL,
		HeadSHA:    hr.HeadSHA,
		Error:      hr.Error,
	}

	if execErr != nil {
		note := fmt.Sprintf("❌ Pilot execution failed:\n\n%s", execErr.Error())
		if _, err := client.AddIssueComment(ctx, issue.ID, note); err != nil {
			logging.WithComponent("bitbucket").Warn("Failed to add failure comment",
				slog.Int("id", issue.ID),
				slog.Any("error", err),
			)
		}
	} else if hr.Result != nil && hr.Result.Success {
		if !hr.Result.IsEpic && hr.Result.CommitSHA == "" && hr.Result.PRUrl == "" {
			note := fmt.Sprintf("⚠️ Pilot execution completed but no changes were made.\n\nDuration: %s\nBranch: %s\n\nNo commits or PR were created. The task may need clarification or manual intervention.",
				hr.Result.Duration, branchName)
			if _, err := client.AddIssueComment(ctx, issue.ID, note); err != nil {
				logging.WithComponent("bitbucket").Warn("Failed to add comment",
					slog.Int("id", issue.ID),
					slog.Any("error", err),
				)
			}
			issueResult.Success = false
		} else {
			var parts []string
			parts = append(parts, "✅ Pilot execution completed successfully!")
			parts = append(parts, "")
			if hr.Result.PRUrl != "" {
				parts = append(parts, fmt.Sprintf("Pull Request: %s", hr.Result.PRUrl))
			}
			if hr.Result.CommitSHA != "" {
				parts = append(parts, fmt.Sprintf("Commit: %s", hr.Result.CommitSHA[:min(8, len(hr.Result.CommitSHA))]))
			}
			parts = append(parts, fmt.Sprintf("Branch: %s", branchName))
			parts = append(parts, fmt.Sprintf("Duration: %s", hr.Result.Duration))
			note := strings.Join(parts, "\n")
			if _, err := client.AddIssueComment(ctx, issue.ID, note); err != nil {
				logging.WithComponent("bitbucket").Warn("Failed to add success comment",
					slog.Int("id", issue.ID),
					slog.Any("error", err),
				)
			}
		}
	} else if hr.Result != nil {
		note := fmt.Sprintf("❌ Pilot execution failed\n\nError: %s\nDuration: %s", hr.Result.Error, hr.Result.Duration)
		if _, err := client.AddIssueComment(ctx, issue.ID, note); err != nil {
			logging.WithComponent("bitbucket").Warn("Failed to add failure comment",
				slog.Int("id", issue.ID),
				slog.Any("error", err),
			)
		}
	}

	return issueResult, execErr
}

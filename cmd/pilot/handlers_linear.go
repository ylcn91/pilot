package main

import (
	"context"
	"fmt"
	"log/slog"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/adapters/linear"
	"github.com/ylcn91/pilot/internal/alerts"
	"github.com/ylcn91/pilot/internal/budget"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/logging"
)

// handleLinearIssueWithResult processes a Linear issue picked up by the poller (GH-393)
func handleLinearIssueWithResult(ctx context.Context, cfg *config.Config, client *linear.Client, issue *linear.Issue, projectPath string, dispatcher *executor.Dispatcher, runner *executor.Runner, monitor *executor.Monitor, program *tea.Program, alertsEngine *alerts.Engine, enforcer *budget.Enforcer) (*linear.IssueResult, error) {
	taskID := issue.Identifier // e.g., "APP-123"

	taskDesc := fmt.Sprintf("Linear Issue %s: %s\n\n%s", issue.Identifier, issue.Title, issue.Description)
	branchName := fmt.Sprintf("pilot/%s", taskID)

	// GH-920: Extract acceptance criteria from Linear issue description
	// GH-1472: Set SourceAdapter/SourceIssueID for sub-issue creation via Linear API
	task := &executor.Task{
		ID:                 taskID,
		Title:              issue.Title,
		Description:        taskDesc,
		ProjectPath:        projectPath,
		Branch:             branchName,
		CreatePR:           true,
		AcceptanceCriteria: github.ExtractAcceptanceCriteria(issue.Description),
		SourceAdapter:      "linear",
		SourceIssueID:      issue.ID,
		BaseBranch:         resolveProjectBaseBranch(cfg, projectPath), // GH-2290
	}

	// GH-1472: Wire Linear client as SubIssueCreator for epic decomposition
	runner.SetSubIssueCreator(client)

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
		URL:      fmt.Sprintf("https://linear.app/issue/%s", issue.Identifier),
		Adapter:  "linear",
		LogEmoji: "📊",
	}

	hr, execErr := handleIssueGeneric(ctx, deps, info, task)

	// Build issue result
	issueResult := &linear.IssueResult{
		Success:    hr.Success,
		BranchName: hr.BranchName, // GH-1361: always set branch for autopilot wiring
		PRNumber:   hr.PRNumber,
		PRURL:      hr.PRURL,
		HeadSHA:    hr.HeadSHA, // GH-1361: for autopilot CI monitoring
		Error:      hr.Error,
	}

	// Post-execution: add comment, transition issue to Done state
	if execErr != nil {
		comment := fmt.Sprintf("❌ Pilot execution failed:\n\n```\n%s\n```", execErr.Error())
		if err := client.AddComment(ctx, issue.ID, comment); err != nil {
			logging.WithComponent("linear").Warn("Failed to add comment",
				slog.String("issue", issue.Identifier),
				slog.Any("error", err),
			)
		}
	} else if hr.Result != nil && hr.Result.Success {
		// Validate deliverables before marking as done
		if !hr.Result.IsEpic && hr.Result.CommitSHA == "" && hr.Result.PRUrl == "" { // GH-3053
			comment := fmt.Sprintf("⚠️ Pilot execution completed but no changes were made.\n\n**Duration:** %s\n**Branch:** `%s`\n\nNo commits or PR were created. The task may need clarification or manual intervention.",
				hr.Result.Duration, branchName)
			if err := client.AddComment(ctx, issue.ID, comment); err != nil {
				logging.WithComponent("linear").Warn("Failed to add comment",
					slog.String("issue", issue.Identifier),
					slog.Any("error", err),
				)
			}
			issueResult.Success = false
		} else {
			comment := buildExecutionComment(hr.Result, branchName)
			if err := client.AddComment(ctx, issue.ID, comment); err != nil {
				logging.WithComponent("linear").Warn("Failed to add comment",
					slog.String("issue", issue.Identifier),
					slog.Any("error", err),
				)
			}

			// GH-1403: Best-effort state transition to Done
			doneStateID, err := client.GetTeamDoneStateID(ctx, issue.Team.Key)
			if err != nil {
				logging.WithComponent("linear").Warn("failed to get done state ID for team",
					slog.String("issue", issue.Identifier),
					slog.String("team", issue.Team.Key),
					slog.Any("error", err),
				)
			} else if err := client.UpdateIssueState(ctx, issue.ID, doneStateID); err != nil {
				logging.WithComponent("linear").Warn("failed to transition issue to done state",
					slog.String("issue", issue.Identifier),
					slog.String("state_id", doneStateID),
					slog.Any("error", err),
				)
			}
		}
	} else if hr.Result != nil {
		comment := buildFailureComment(hr.Result)
		if err := client.AddComment(ctx, issue.ID, comment); err != nil {
			logging.WithComponent("linear").Warn("Failed to add comment",
				slog.String("issue", issue.Identifier),
				slog.Any("error", err),
			)
		}
	}

	return issueResult, execErr
}

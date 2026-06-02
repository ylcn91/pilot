package main

import (
	"context"
	"fmt"
	"log/slog"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ylcn91/pilot/internal/adapters/azuredevops"
	"github.com/ylcn91/pilot/internal/alerts"
	"github.com/ylcn91/pilot/internal/budget"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/logging"
)

// handleAzureDevOpsWorkItemWithResult processes an Azure DevOps work item picked up by the poller (GH-2132).
func handleAzureDevOpsWorkItemWithResult(ctx context.Context, cfg *config.Config, client *azuredevops.Client, notifier *azuredevops.Notifier, wi *azuredevops.WorkItem, projectPath string, dispatcher *executor.Dispatcher, runner *executor.Runner, monitor *executor.Monitor, program *tea.Program, alertsEngine *alerts.Engine, enforcer *budget.Enforcer) (*azuredevops.WorkItemResult, error) {
	taskID := fmt.Sprintf("ADO-%d", wi.ID)
	title := wi.GetTitle()

	taskDesc := fmt.Sprintf("Azure DevOps Work Item %d: %s\n\n%s", wi.ID, title, wi.GetDescription())
	branchName := fmt.Sprintf("pilot/%s", taskID)

	task := &executor.Task{
		ID:          taskID,
		Title:       title,
		Description: taskDesc,
		ProjectPath: projectPath,
		Branch:      branchName,
		CreatePR:    true,
		BaseBranch:  resolveProjectBaseBranch(cfg, projectPath), // GH-2290
	}

	// GH-2132: Notify task started via notifier (adds in-progress tag + comment)
	if notifier != nil {
		if err := notifier.NotifyTaskStarted(ctx, wi.ID, taskID); err != nil {
			logging.WithComponent("azuredevops").Warn("Failed to notify task started",
				slog.Int("work_item_id", wi.ID),
				slog.Any("error", err),
			)
		}
	}

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
		Title:    title,
		URL:      wi.URL,
		Adapter:  "azuredevops",
		LogEmoji: "🔷",
	}

	hr, execErr := handleIssueGeneric(ctx, deps, info, task)

	// Build work item result
	wiResult := &azuredevops.WorkItemResult{
		Success:    hr.Success,
		BranchName: hr.BranchName,
		PRNumber:   hr.PRNumber,
		PRURL:      hr.PRURL,
		HeadSHA:    hr.HeadSHA,
		Error:      hr.Error,
	}

	// Post-execution notifications via notifier
	if notifier != nil {
		if execErr != nil {
			if err := notifier.NotifyTaskFailed(ctx, wi.ID, execErr.Error()); err != nil {
				logging.WithComponent("azuredevops").Warn("Failed to notify task failed",
					slog.Int("work_item_id", wi.ID),
					slog.Any("error", err),
				)
			}
		} else if hr.Result != nil && hr.Result.Success {
			if !hr.Result.IsEpic && hr.Result.CommitSHA == "" && hr.Result.PRUrl == "" { // GH-3053
				if err := notifier.NotifyTaskFailed(ctx, wi.ID, "Execution completed but no changes were made"); err != nil {
					logging.WithComponent("azuredevops").Warn("Failed to notify no-changes",
						slog.Int("work_item_id", wi.ID),
						slog.Any("error", err),
					)
				}
				wiResult.Success = false
			} else {
				summary := fmt.Sprintf("Duration: %s", hr.Result.Duration)
				if err := notifier.NotifyTaskCompleted(ctx, wi.ID, hr.Result.PRUrl, summary); err != nil {
					logging.WithComponent("azuredevops").Warn("Failed to notify task completed",
						slog.Int("work_item_id", wi.ID),
						slog.Any("error", err),
					)
				}
				// Link PR if created
				if hr.PRNumber > 0 {
					if err := notifier.LinkPR(ctx, wi.ID, hr.PRNumber, hr.PRURL); err != nil {
						logging.WithComponent("azuredevops").Warn("Failed to link PR",
							slog.Int("work_item_id", wi.ID),
							slog.Any("error", err),
						)
					}
				}
			}
		} else if hr.Result != nil {
			if err := notifier.NotifyTaskFailed(ctx, wi.ID, hr.Result.Error); err != nil {
				logging.WithComponent("azuredevops").Warn("Failed to notify task failed",
					slog.Int("work_item_id", wi.ID),
					slog.Any("error", err),
				)
			}
		}
	}

	return wiResult, execErr
}

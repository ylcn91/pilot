package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ylcn91/pilot/internal/adapters/asana"
	"github.com/ylcn91/pilot/internal/alerts"
	"github.com/ylcn91/pilot/internal/budget"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/logging"
)

// handleAsanaTaskWithResult processes an Asana task picked up by the poller (GH-906)
func handleAsanaTaskWithResult(ctx context.Context, cfg *config.Config, client *asana.Client, task *asana.Task, projectPath string, dispatcher *executor.Dispatcher, runner *executor.Runner, monitor *executor.Monitor, program *tea.Program, alertsEngine *alerts.Engine, enforcer *budget.Enforcer) (*asana.TaskResult, error) {
	taskID := "ASANA-" + task.GID

	// Get task URL
	taskURL := task.Permalink
	if taskURL == "" {
		taskURL = "https://app.asana.com/0/0/" + task.GID
	}

	taskDesc := fmt.Sprintf("Asana Task %s: %s\n\n%s", task.GID, task.Name, task.Notes)
	branchName := fmt.Sprintf("pilot/%s", taskID)

	execTask := &executor.Task{
		ID:          taskID,
		Title:       task.Name,
		Description: taskDesc,
		ProjectPath: projectPath,
		Branch:      branchName,
		CreatePR:    true,
		BaseBranch:  resolveProjectBaseBranch(cfg, projectPath), // GH-2290
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
		Title:    task.Name,
		URL:      taskURL,
		Adapter:  "asana",
		LogEmoji: "📦",
	}

	hr, execErr := handleIssueGeneric(ctx, deps, info, execTask)

	// Build task result
	taskResult := &asana.TaskResult{
		Success:    hr.Success,
		BranchName: hr.BranchName, // GH-1399: always set branch for autopilot wiring
		PRNumber:   hr.PRNumber,
		PRURL:      hr.PRURL,
		HeadSHA:    hr.HeadSHA, // GH-1399: for autopilot CI monitoring
		Error:      hr.Error,
	}

	// Post-execution: add comment, call CompleteTask()
	if execErr != nil {
		comment := fmt.Sprintf("❌ Pilot execution failed:\n\n%s", execErr.Error())
		if _, err := client.AddComment(ctx, task.GID, comment); err != nil {
			logging.WithComponent("asana").Warn("Failed to add comment",
				slog.String("task", task.GID),
				slog.Any("error", err),
			)
		}
	} else if hr.Result != nil && hr.Result.Success {
		// Validate deliverables before marking as done
		if !hr.Result.IsEpic && hr.Result.CommitSHA == "" && hr.Result.PRUrl == "" { // GH-3053
			comment := fmt.Sprintf("⚠️ Pilot execution completed but no changes were made.\n\nDuration: %s\nBranch: %s\n\nNo commits or PR were created. The task may need clarification or manual intervention.",
				hr.Result.Duration, branchName)
			if _, err := client.AddComment(ctx, task.GID, comment); err != nil {
				logging.WithComponent("asana").Warn("Failed to add comment",
					slog.String("task", task.GID),
					slog.Any("error", err),
				)
			}
			taskResult.Success = false
		} else {
			comment := buildAsanaExecutionComment(hr.Result, branchName)
			if _, err := client.AddComment(ctx, task.GID, comment); err != nil {
				logging.WithComponent("asana").Warn("Failed to add comment",
					slog.String("task", task.GID),
					slog.Any("error", err),
				)
			}

			// GH-1403: Best-effort task completion
			if _, err := client.CompleteTask(ctx, task.GID); err != nil {
				logging.WithComponent("asana").Warn("failed to complete task",
					slog.String("task", task.GID),
					slog.Any("error", err),
				)
			}
		}
	} else if hr.Result != nil {
		comment := buildAsanaFailureComment(hr.Result)
		if _, err := client.AddComment(ctx, task.GID, comment); err != nil {
			logging.WithComponent("asana").Warn("Failed to add comment",
				slog.String("task", task.GID),
				slog.Any("error", err),
			)
		}
	}

	return taskResult, execErr
}

// buildAsanaExecutionComment creates a comment for successful Asana execution
func buildAsanaExecutionComment(result *executor.ExecutionResult, branchName string) string {
	var parts []string
	parts = append(parts, "✅ Pilot execution completed successfully!")
	parts = append(parts, "")

	if result.PRUrl != "" {
		parts = append(parts, fmt.Sprintf("Pull Request: %s", result.PRUrl))
	}
	if result.CommitSHA != "" {
		parts = append(parts, fmt.Sprintf("Commit: %s", result.CommitSHA[:min(8, len(result.CommitSHA))]))
	}
	parts = append(parts, fmt.Sprintf("Branch: %s", branchName))
	parts = append(parts, fmt.Sprintf("Duration: %s", result.Duration))

	return strings.Join(parts, "\n")
}

// buildAsanaFailureComment creates a comment for failed Asana execution
func buildAsanaFailureComment(result *executor.ExecutionResult) string {
	var parts []string
	parts = append(parts, "❌ Pilot execution failed")
	parts = append(parts, "")
	if result.Error != "" {
		parts = append(parts, fmt.Sprintf("Error: %s", result.Error))
	}
	if result.Duration > 0 {
		parts = append(parts, fmt.Sprintf("Duration: %s", result.Duration))
	}
	return strings.Join(parts, "\n")
}

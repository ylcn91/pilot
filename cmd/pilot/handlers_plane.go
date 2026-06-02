package main

import (
	"context"
	"fmt"
	"log/slog"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ylcn91/pilot/internal/adapters/plane"
	"github.com/ylcn91/pilot/internal/alerts"
	"github.com/ylcn91/pilot/internal/budget"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/logging"
)

// handlePlaneIssueWithResult processes a Plane.so work item picked up by the poller (GH-1833).
func handlePlaneIssueWithResult(ctx context.Context, cfg *config.Config, client *plane.Client, issue *plane.WorkItem, projectPath string, dispatcher *executor.Dispatcher, runner *executor.Runner, monitor *executor.Monitor, program *tea.Program, alertsEngine *alerts.Engine, enforcer *budget.Enforcer) (*plane.IssueResult, error) {
	// Use first 8 chars of UUID as short task ID for display
	taskID := "PLANE-" + issue.ID[:8]
	title := issue.Name

	taskDesc := fmt.Sprintf("Plane Issue %s: %s\n\n%s", taskID, title, issue.Description)
	branchName := fmt.Sprintf("pilot/%s", taskID)

	task := &executor.Task{
		ID:            taskID,
		Title:         title,
		Description:   taskDesc,
		ProjectPath:   projectPath,
		Branch:        branchName,
		CreatePR:      true,
		SourceAdapter: "plane",
		SourceIssueID: issue.ID,
		BaseBranch:    resolveProjectBaseBranch(cfg, projectPath), // GH-2290
	}

	// Wire Plane client as SubIssueCreator for epic decomposition (GH-1833)
	// Configure workspace slug and default project on the client for CreateIssue calls
	subCreatorClient := plane.NewClient(
		cfg.Adapters.Plane.BaseURL,
		cfg.Adapters.Plane.APIKey,
		plane.WithWorkspaceSlug(cfg.Adapters.Plane.WorkspaceSlug),
		plane.WithDefaultProjectID(issue.ProjectID),
	)
	runner.SetSubIssueCreator(subCreatorClient)

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
		URL:      fmt.Sprintf("%s/workspaces/%s/projects/%s/work-items/%s", cfg.Adapters.Plane.BaseURL, cfg.Adapters.Plane.WorkspaceSlug, issue.ProjectID, issue.ID),
		Adapter:  "plane",
		LogEmoji: "📊",
	}

	hr, execErr := handleIssueGeneric(ctx, deps, info, task)

	// Build issue result
	issueResult := &plane.IssueResult{
		Success:    hr.Success,
		BranchName: hr.BranchName,
		PRNumber:   hr.PRNumber,
		PRURL:      hr.PRURL,
		HeadSHA:    hr.HeadSHA,
		Error:      hr.Error,
	}

	// Post-execution: add HTML comment, transition work item state
	workspaceSlug := cfg.Adapters.Plane.WorkspaceSlug
	projectID := issue.ProjectID
	if execErr != nil {
		comment := fmt.Sprintf("<p>❌ Pilot execution failed:</p><pre>%s</pre>", execErr.Error())
		if err := client.AddComment(ctx, workspaceSlug, projectID, issue.ID, comment); err != nil {
			logging.WithComponent("plane").Warn("Failed to add failure comment",
				slog.String("issue_id", issue.ID),
				slog.Any("error", err),
			)
		}
	} else if hr.Result != nil && hr.Result.Success {
		if !hr.Result.IsEpic && hr.Result.CommitSHA == "" && hr.Result.PRUrl == "" { // GH-3053
			comment := fmt.Sprintf("<p>⚠️ Pilot execution completed but no changes were made.</p><p>Duration: %s<br>Branch: <code>%s</code></p><p>No commits or PR were created. The task may need clarification or manual intervention.</p>",
				hr.Result.Duration, branchName)
			if err := client.AddComment(ctx, workspaceSlug, projectID, issue.ID, comment); err != nil {
				logging.WithComponent("plane").Warn("Failed to add comment",
					slog.String("issue_id", issue.ID),
					slog.Any("error", err),
				)
			}
			issueResult.Success = false
		} else {
			comment := buildPlaneExecutionComment(hr.Result, branchName)
			if err := client.AddComment(ctx, workspaceSlug, projectID, issue.ID, comment); err != nil {
				logging.WithComponent("plane").Warn("Failed to add success comment",
					slog.String("issue_id", issue.ID),
					slog.Any("error", err),
				)
			}
		}
	} else if hr.Result != nil {
		comment := fmt.Sprintf("<p>❌ Pilot execution failed:</p><pre>%s</pre>", hr.Result.Error)
		if err := client.AddComment(ctx, workspaceSlug, projectID, issue.ID, comment); err != nil {
			logging.WithComponent("plane").Warn("Failed to add failure comment",
				slog.String("issue_id", issue.ID),
				slog.Any("error", err),
			)
		}
	}

	return issueResult, execErr
}

// buildPlaneExecutionComment creates an HTML comment for a successful Plane.so execution.
func buildPlaneExecutionComment(result *executor.ExecutionResult, branchName string) string {
	comment := "<p>✅ Pilot execution completed successfully.</p>"
	if result.PRUrl != "" {
		comment += fmt.Sprintf("<p>🔗 <a href=\"%s\">View Pull Request</a></p>", result.PRUrl)
	}
	comment += fmt.Sprintf("<p>🌿 Branch: <code>%s</code></p>", branchName)
	if result.Duration > 0 {
		comment += fmt.Sprintf("<p>⏱ Duration: %s</p>", result.Duration)
	}
	return comment
}

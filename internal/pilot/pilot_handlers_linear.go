package pilot

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ylcn91/pilot/internal/adapters/linear"
	"github.com/ylcn91/pilot/internal/logging"
	"github.com/ylcn91/pilot/internal/webhooks"
)

// handleLinearIssue handles a new Linear issue (legacy single-workspace mode)
func (p *Pilot) handleLinearIssue(ctx context.Context, issue *linear.Issue) error {
	logging.WithComponent("pilot").Info("Received Linear issue",
		slog.String("identifier", issue.Identifier),
		slog.String("title", issue.Title))

	// Find project for this issue
	projectPath := p.findProjectForIssue(issue)
	if projectPath == "" {
		return fmt.Errorf("no project configured for issue %s", issue.Identifier)
	}

	// Track task ID -> Linear issue ID mapping for completion callback
	// Task ID format matches bridge.go: "TASK-{identifier}"
	taskID := fmt.Sprintf("TASK-%s", issue.Identifier)
	p.linearTasksMu.Lock()
	p.linearTasks[taskID] = linearTaskInfo{IssueID: issue.ID, WorkspaceName: ""}
	p.linearTasksMu.Unlock()

	// Dispatch outbound webhook
	if p.webhookManager.IsEnabled() {
		p.webhookManager.Dispatch(ctx, webhooks.NewEvent(webhooks.EventTaskStarted, &webhooks.TaskStartedData{
			TaskID: taskID, Title: issue.Title, Project: projectPath, Source: "linear", SourceID: issue.Identifier,
		}))
	}

	// Notify that task has started
	if p.linearNotify != nil {
		if err := p.linearNotify.NotifyTaskStarted(ctx, issue.ID, taskID); err != nil {
			logging.WithComponent("pilot").Warn("Failed to notify task started", slog.Any("error", err))
		}
	}

	// Process ticket through orchestrator
	err := p.orchestrator.ProcessTicket(ctx, issue, projectPath)

	// Immediate errors are handled here; async completion is handled by handleTaskCompletion
	if err != nil && p.linearNotify != nil {
		if notifyErr := p.linearNotify.NotifyTaskFailed(ctx, issue.ID, err.Error()); notifyErr != nil {
			logging.WithComponent("pilot").Warn("Failed to notify task failed", slog.Any("error", notifyErr))
		}
		// Clean up tracking on immediate error
		p.linearTasksMu.Lock()
		delete(p.linearTasks, taskID)
		p.linearTasksMu.Unlock()
	}

	return err
}

// handleLinearIssueMultiWorkspace handles a new Linear issue in multi-workspace mode (GH-391)
func (p *Pilot) handleLinearIssueMultiWorkspace(ctx context.Context, issue *linear.Issue, workspaceName string) error {
	logging.WithComponent("pilot").Info("Received Linear issue",
		slog.String("identifier", issue.Identifier),
		slog.String("title", issue.Title),
		slog.String("workspace", workspaceName))

	// Get workspace handler for project resolution and notifications
	ws := p.linearMultiWH.GetWorkspace(workspaceName)
	if ws == nil {
		return fmt.Errorf("workspace %s not found", workspaceName)
	}

	// Find project for this issue
	var projectPath string

	// GH-1684: Check project-level linear.project_id mapping first
	if issue.Project != nil {
		if proj := p.config.GetProjectByLinearID(issue.Project.ID); proj != nil {
			projectPath = proj.Path
		}
	}

	// Fall back to workspace-specific mapping
	if projectPath == "" {
		pilotProject := ws.ResolvePilotProject(issue)
		if pilotProject != "" {
			if proj := p.config.GetProjectByName(pilotProject); proj != nil {
				projectPath = proj.Path
			}
		}
	}

	// Fall back to generic matching
	if projectPath == "" {
		projectPath = p.findProjectForIssue(issue)
	}

	if projectPath == "" {
		return fmt.Errorf("no project configured for issue %s in workspace %s", issue.Identifier, workspaceName)
	}

	// Track task ID -> Linear issue ID + workspace mapping for completion callback
	taskID := fmt.Sprintf("TASK-%s", issue.Identifier)
	p.linearTasksMu.Lock()
	p.linearTasks[taskID] = linearTaskInfo{IssueID: issue.ID, WorkspaceName: workspaceName}
	p.linearTasksMu.Unlock()

	// Notify that task has started
	notifier := ws.Notifier()
	if notifier != nil {
		if err := notifier.NotifyTaskStarted(ctx, issue.ID, taskID); err != nil {
			logging.WithComponent("pilot").Warn("Failed to notify task started", slog.Any("error", err))
		}
	}

	// Process ticket through orchestrator
	err := p.orchestrator.ProcessTicket(ctx, issue, projectPath)

	// Immediate errors are handled here; async completion is handled by handleTaskCompletion
	if err != nil && notifier != nil {
		if notifyErr := notifier.NotifyTaskFailed(ctx, issue.ID, err.Error()); notifyErr != nil {
			logging.WithComponent("pilot").Warn("Failed to notify task failed", slog.Any("error", notifyErr))
		}
		// Clean up tracking on immediate error
		p.linearTasksMu.Lock()
		delete(p.linearTasks, taskID)
		p.linearTasksMu.Unlock()
	}

	return err
}

// handleTaskCompletion handles task completion events from the orchestrator
func (p *Pilot) handleTaskCompletion(taskID, prURL string, success bool, errMsg string) {
	// Dispatch outbound webhook
	if p.webhookManager.IsEnabled() {
		ctx := context.Background()
		if success {
			p.webhookManager.Dispatch(ctx, webhooks.NewEvent(webhooks.EventTaskCompleted, &webhooks.TaskCompletedData{
				TaskID:    taskID,
				PRCreated: prURL != "",
				PRURL:     prURL,
			}))
		} else {
			p.webhookManager.Dispatch(ctx, webhooks.NewEvent(webhooks.EventTaskFailed, &webhooks.TaskFailedData{
				TaskID: taskID,
				Error:  errMsg,
			}))
		}
	}

	// Check if this is a Linear task
	p.linearTasksMu.Lock()
	taskInfo, isLinear := p.linearTasks[taskID]
	if isLinear {
		delete(p.linearTasks, taskID)
	}
	p.linearTasksMu.Unlock()

	if !isLinear {
		return
	}

	ctx := context.Background()

	// Get the appropriate notifier (GH-391: multi-workspace support)
	var notifier *linear.Notifier
	if taskInfo.WorkspaceName != "" && p.linearMultiWH != nil {
		notifier = p.linearMultiWH.GetNotifier(taskInfo.WorkspaceName)
	} else if p.linearNotify != nil {
		notifier = p.linearNotify
	}

	if notifier == nil {
		logging.WithComponent("pilot").Warn("No notifier found for Linear task",
			slog.String("task_id", taskID),
			slog.String("workspace", taskInfo.WorkspaceName))
		return
	}

	if success {
		if err := notifier.NotifyTaskCompleted(ctx, taskInfo.IssueID, prURL, ""); err != nil {
			logging.WithComponent("pilot").Warn("Failed to notify Linear task completed",
				slog.String("task_id", taskID),
				slog.Any("error", err))
		}
	} else {
		if err := notifier.NotifyTaskFailed(ctx, taskInfo.IssueID, errMsg); err != nil {
			logging.WithComponent("pilot").Warn("Failed to notify Linear task failed",
				slog.String("task_id", taskID),
				slog.Any("error", err))
		}
	}
}

// findProjectForIssue finds the project path for an issue
func (p *Pilot) findProjectForIssue(issue *linear.Issue) string {
	// Try to match by project name or team
	for _, proj := range p.config.Projects {
		// Match by name
		if issue.Project != nil && issue.Project.Name == proj.Name {
			return proj.Path
		}
		// Match by team key
		if issue.Team.Key != "" && proj.Name == issue.Team.Key {
			return proj.Path
		}
	}

	// Return first project as fallback
	if len(p.config.Projects) > 0 {
		return p.config.Projects[0].Path
	}

	return ""
}

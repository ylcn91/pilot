package orchestrator

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ylcn91/pilot/internal/adapters/asana"
	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/adapters/gitlab"
	"github.com/ylcn91/pilot/internal/adapters/jira"
	"github.com/ylcn91/pilot/internal/adapters/linear"
	"github.com/ylcn91/pilot/internal/adapters/plane"
	"github.com/ylcn91/pilot/internal/logging"
)

// processGenericTicket is the shared body for every Process*Ticket method:
// plan the ticket via the bridge, best-effort save the task document, build the
// internal Task, and queue it. The branch is supplied by the caller because
// each adapter derives it from a different identifier. ticket is non-nil only
// for the Linear path; for every other source it stays nil so the worker reads
// Task.Priority (derived from TicketData.Priority) instead of Ticket.Priority.
func (o *Orchestrator) processGenericTicket(ctx context.Context, td *TicketData, projectPath, branch string, ticket *linear.Issue) error {
	doc, err := o.bridge.PlanTicket(ctx, td)
	if err != nil {
		return fmt.Errorf("failed to plan ticket: %w", err)
	}

	if err := o.saveTaskDocument(projectPath, doc); err != nil {
		logging.WithComponent("orchestrator").Warn("Failed to save task document", slog.Any("error", err))
	}

	task := &Task{
		ID:          doc.ID,
		Ticket:      ticket,
		Document:    doc,
		ProjectPath: projectPath,
		Branch:      branch,
		Priority:    float64(td.Priority),
	}

	o.QueueTask(task)

	return nil
}

// ProcessTicket processes a new ticket from Linear
func (o *Orchestrator) ProcessTicket(ctx context.Context, issue *linear.Issue, projectPath string) error {
	ticket := &TicketData{
		ID:          issue.ID,
		Identifier:  issue.Identifier,
		Title:       issue.Title,
		Description: issue.Description,
		Priority:    issue.Priority,
		Labels:      extractLabelNames(issue.Labels),
	}
	return o.processGenericTicket(ctx, ticket, projectPath, fmt.Sprintf("pilot/%s", issue.Identifier), issue)
}

// extractLabelNames extracts label names from Linear labels
func extractLabelNames(labels []linear.Label) []string {
	names := make([]string, len(labels))
	for i, label := range labels {
		names[i] = label.Name
	}
	return names
}

// ProcessGithubTicket processes a new ticket from GitHub Issues
func (o *Orchestrator) ProcessGithubTicket(ctx context.Context, task *github.TaskInfo, projectPath string) error {
	ticket := &TicketData{
		ID:          task.ID,
		Identifier:  task.ID, // GH-42 format
		Title:       task.Title,
		Description: task.Description,
		Priority:    int(task.Priority),
		Labels:      task.Labels,
	}
	return o.processGenericTicket(ctx, ticket, projectPath, fmt.Sprintf("pilot/%s", task.ID), nil)
}

// ProcessGitlabTicket processes a new ticket from GitLab Issues
func (o *Orchestrator) ProcessGitlabTicket(ctx context.Context, task *gitlab.TaskInfo, projectPath string) error {
	ticket := &TicketData{
		ID:          task.ID,
		Identifier:  task.ID, // GL-42 format
		Title:       task.Title,
		Description: task.Description,
		Priority:    int(task.Priority),
		Labels:      task.Labels,
	}
	return o.processGenericTicket(ctx, ticket, projectPath, fmt.Sprintf("pilot/%s", task.ID), nil)
}

// ProcessJiraTicket processes a new ticket from Jira
func (o *Orchestrator) ProcessJiraTicket(ctx context.Context, task *jira.TaskInfo, projectPath string) error {
	ticket := &TicketData{
		ID:          task.ID,
		Identifier:  task.IssueKey, // PROJ-123 format
		Title:       task.Title,
		Description: task.Description,
		Priority:    int(task.Priority),
		Labels:      task.Labels,
	}
	return o.processGenericTicket(ctx, ticket, projectPath, fmt.Sprintf("pilot/%s", task.IssueKey), nil)
}

// ProcessAsanaTicket processes a new ticket from Asana (GH-2044)
func (o *Orchestrator) ProcessAsanaTicket(ctx context.Context, task *asana.TaskInfo, projectPath string) error {
	ticket := &TicketData{
		ID:          task.ID,
		Identifier:  task.ID,
		Title:       task.Title,
		Description: task.Description,
		Priority:    int(task.Priority),
		Labels:      task.Labels,
		Project:     task.ProjectName,
	}
	return o.processGenericTicket(ctx, ticket, projectPath, fmt.Sprintf("pilot/%s", task.ID), nil)
}

// ProcessPlaneTicket processes a new ticket from Plane (GH-2044)
func (o *Orchestrator) ProcessPlaneTicket(ctx context.Context, item *plane.WebhookWorkItemData, projectPath string) error {
	ticket := &TicketData{
		ID:         item.ID,
		Identifier: fmt.Sprintf("PLANE-%d", item.SequenceID),
		Title:      item.Name,
	}
	return o.processGenericTicket(ctx, ticket, projectPath, fmt.Sprintf("pilot/PLANE-%d", item.SequenceID), nil)
}

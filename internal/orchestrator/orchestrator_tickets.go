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

// ProcessTicket processes a new ticket from Linear
func (o *Orchestrator) ProcessTicket(ctx context.Context, issue *linear.Issue, projectPath string) error {
	// Convert ticket to task document
	ticket := &TicketData{
		ID:          issue.ID,
		Identifier:  issue.Identifier,
		Title:       issue.Title,
		Description: issue.Description,
		Priority:    issue.Priority,
		Labels:      extractLabelNames(issue.Labels),
	}

	doc, err := o.bridge.PlanTicket(ctx, ticket)
	if err != nil {
		return fmt.Errorf("failed to plan ticket: %w", err)
	}

	// Save task document
	if err := o.saveTaskDocument(projectPath, doc); err != nil {
		logging.WithComponent("orchestrator").Warn("Failed to save task document", slog.Any("error", err))
	}

	// Create task
	task := &Task{
		ID:          doc.ID,
		Ticket:      issue,
		Document:    doc,
		ProjectPath: projectPath,
		Branch:      fmt.Sprintf("pilot/%s", issue.Identifier),
	}

	// Queue task
	o.QueueTask(task)

	return nil
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
	// Convert GitHub task to task document via bridge
	ticket := &TicketData{
		ID:          task.ID,
		Identifier:  task.ID, // GH-42 format
		Title:       task.Title,
		Description: task.Description,
		Priority:    int(task.Priority),
		Labels:      task.Labels,
	}

	doc, err := o.bridge.PlanTicket(ctx, ticket)
	if err != nil {
		return fmt.Errorf("failed to plan ticket: %w", err)
	}

	// Save task document
	if err := o.saveTaskDocument(projectPath, doc); err != nil {
		logging.WithComponent("orchestrator").Warn("Failed to save task document", slog.Any("error", err))
	}

	// Create internal task
	internalTask := &Task{
		ID:          doc.ID,
		Document:    doc,
		ProjectPath: projectPath,
		Branch:      fmt.Sprintf("pilot/%s", task.ID),
		Priority:    float64(task.Priority),
	}

	// Queue task
	o.QueueTask(internalTask)

	return nil
}

// ProcessGitlabTicket processes a new ticket from GitLab Issues
func (o *Orchestrator) ProcessGitlabTicket(ctx context.Context, task *gitlab.TaskInfo, projectPath string) error {
	// Convert GitLab task to task document via bridge
	ticket := &TicketData{
		ID:          task.ID,
		Identifier:  task.ID, // GL-42 format
		Title:       task.Title,
		Description: task.Description,
		Priority:    int(task.Priority),
		Labels:      task.Labels,
	}

	doc, err := o.bridge.PlanTicket(ctx, ticket)
	if err != nil {
		return fmt.Errorf("failed to plan ticket: %w", err)
	}

	// Save task document
	if err := o.saveTaskDocument(projectPath, doc); err != nil {
		logging.WithComponent("orchestrator").Warn("Failed to save task document", slog.Any("error", err))
	}

	// Create internal task
	internalTask := &Task{
		ID:          doc.ID,
		Document:    doc,
		ProjectPath: projectPath,
		Branch:      fmt.Sprintf("pilot/%s", task.ID),
		Priority:    float64(task.Priority),
	}

	// Queue task
	o.QueueTask(internalTask)

	return nil
}

// ProcessJiraTicket processes a new ticket from Jira
func (o *Orchestrator) ProcessJiraTicket(ctx context.Context, task *jira.TaskInfo, projectPath string) error {
	// Convert Jira task to task document via bridge
	ticket := &TicketData{
		ID:          task.ID,
		Identifier:  task.IssueKey, // PROJ-123 format
		Title:       task.Title,
		Description: task.Description,
		Priority:    int(task.Priority),
		Labels:      task.Labels,
	}

	doc, err := o.bridge.PlanTicket(ctx, ticket)
	if err != nil {
		return fmt.Errorf("failed to plan ticket: %w", err)
	}

	// Save task document
	if err := o.saveTaskDocument(projectPath, doc); err != nil {
		logging.WithComponent("orchestrator").Warn("Failed to save task document", slog.Any("error", err))
	}

	// Create internal task
	internalTask := &Task{
		ID:          doc.ID,
		Document:    doc,
		ProjectPath: projectPath,
		Branch:      fmt.Sprintf("pilot/%s", task.IssueKey),
		Priority:    float64(task.Priority),
	}

	// Queue task
	o.QueueTask(internalTask)

	return nil
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

	doc, err := o.bridge.PlanTicket(ctx, ticket)
	if err != nil {
		return fmt.Errorf("failed to plan ticket: %w", err)
	}

	if err := o.saveTaskDocument(projectPath, doc); err != nil {
		logging.WithComponent("orchestrator").Warn("Failed to save task document", slog.Any("error", err))
	}

	internalTask := &Task{
		ID:          doc.ID,
		Document:    doc,
		ProjectPath: projectPath,
		Branch:      fmt.Sprintf("pilot/%s", task.ID),
		Priority:    float64(task.Priority),
	}

	o.QueueTask(internalTask)

	return nil
}

// ProcessPlaneTicket processes a new ticket from Plane (GH-2044)
func (o *Orchestrator) ProcessPlaneTicket(ctx context.Context, item *plane.WebhookWorkItemData, projectPath string) error {
	ticket := &TicketData{
		ID:         item.ID,
		Identifier: fmt.Sprintf("PLANE-%d", item.SequenceID),
		Title:      item.Name,
	}

	doc, err := o.bridge.PlanTicket(ctx, ticket)
	if err != nil {
		return fmt.Errorf("failed to plan ticket: %w", err)
	}

	if err := o.saveTaskDocument(projectPath, doc); err != nil {
		logging.WithComponent("orchestrator").Warn("Failed to save task document", slog.Any("error", err))
	}

	internalTask := &Task{
		ID:          doc.ID,
		Document:    doc,
		ProjectPath: projectPath,
		Branch:      fmt.Sprintf("pilot/PLANE-%d", item.SequenceID),
	}

	o.QueueTask(internalTask)

	return nil
}

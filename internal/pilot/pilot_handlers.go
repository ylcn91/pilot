package pilot

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ylcn91/pilot/internal/adapters/asana"
	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/adapters/jira"
	"github.com/ylcn91/pilot/internal/adapters/plane"
	"github.com/ylcn91/pilot/internal/logging"
)

// handleGithubIssue handles a new GitHub issue
func (p *Pilot) handleGithubIssue(ctx context.Context, issue *github.Issue, repo *github.Repository) error {
	logging.WithComponent("pilot").Info("Received GitHub issue",
		slog.String("repo", repo.FullName),
		slog.Int("number", issue.Number),
		slog.String("title", issue.Title))

	// Convert to task
	task := github.ConvertIssueToTask(issue, repo)

	// Find project for this repo
	projectPath := p.findProjectForGithubRepo(repo)
	if projectPath == "" {
		return fmt.Errorf("no project configured for repo %s", repo.FullName)
	}

	// Notify that task has started
	if p.githubNotify != nil {
		if err := p.githubNotify.NotifyTaskStarted(ctx, repo.Owner.Login, repo.Name, issue.Number, task.ID); err != nil {
			logging.WithComponent("pilot").Warn("Failed to notify task started", slog.Any("error", err))
		}
	}

	// Process ticket through orchestrator
	err := p.orchestrator.ProcessGithubTicket(ctx, task, projectPath)

	// Update GitHub with result
	if p.githubNotify != nil {
		if err != nil {
			if notifyErr := p.githubNotify.NotifyTaskFailed(ctx, repo.Owner.Login, repo.Name, issue.Number, err.Error()); notifyErr != nil {
				logging.WithComponent("pilot").Warn("Failed to notify task failed", slog.Any("error", notifyErr))
			}
		}
		// Success notification handled by orchestrator when PR is created
	}

	return err
}

// findProjectForGithubRepo finds the project path for a GitHub repo
func (p *Pilot) findProjectForGithubRepo(repo *github.Repository) string {
	// Try to match by repo name or full name
	for _, proj := range p.config.Projects {
		// Match by name
		if repo.Name == proj.Name {
			return proj.Path
		}
		// Match by full name (org/repo)
		if repo.FullName == proj.Name {
			return proj.Path
		}
	}

	// Return first project as fallback
	if len(p.config.Projects) > 0 {
		return p.config.Projects[0].Path
	}

	return ""
}

// handleJiraIssue handles a new Jira issue
func (p *Pilot) handleJiraIssue(ctx context.Context, issue *jira.Issue) error {
	logging.WithComponent("pilot").Info("Received Jira issue",
		slog.String("key", issue.Key),
		slog.String("summary", issue.Fields.Summary))

	// Convert to task
	task := jira.ConvertIssueToTask(issue, p.config.Adapters.Jira.BaseURL)

	// Find project for this Jira project
	projectPath := p.findProjectForJiraProject(issue.Fields.Project.Key)
	if projectPath == "" {
		return fmt.Errorf("no project configured for Jira project %s", issue.Fields.Project.Key)
	}

	// Process ticket through orchestrator
	return p.orchestrator.ProcessJiraTicket(ctx, task, projectPath)
}

// findProjectForJiraProject finds the project path for a Jira project key
func (p *Pilot) findProjectForJiraProject(projectKey string) string {
	for _, proj := range p.config.Projects {
		if proj.Name == projectKey {
			return proj.Path
		}
	}

	// Return first project as fallback
	if len(p.config.Projects) > 0 {
		return p.config.Projects[0].Path
	}

	return ""
}

// handleAsanaTask handles a new Asana task (GH-2044)
func (p *Pilot) handleAsanaTask(ctx context.Context, task *asana.Task) error {
	logging.WithComponent("pilot").Info("Received Asana task",
		slog.String("gid", task.GID),
		slog.String("name", task.Name))

	taskInfo := asana.ConvertToTaskInfo(task)

	// Find project (use first project as fallback)
	projectPath := p.defaultProjectPath()

	return p.orchestrator.ProcessAsanaTicket(ctx, taskInfo, projectPath)
}

// handlePlaneWorkItem handles a new Plane work item (GH-2044)
func (p *Pilot) handlePlaneWorkItem(ctx context.Context, item *plane.WebhookWorkItemData) error {
	logging.WithComponent("pilot").Info("Received Plane work item",
		slog.String("id", item.ID),
		slog.String("name", item.Name))

	projectPath := p.defaultProjectPath()

	return p.orchestrator.ProcessPlaneTicket(ctx, item, projectPath)
}

// defaultProjectPath returns the first configured project path
func (p *Pilot) defaultProjectPath() string {
	if len(p.config.Projects) > 0 {
		return p.config.Projects[0].Path
	}
	return ""
}

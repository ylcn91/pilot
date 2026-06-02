package pilot

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ylcn91/pilot/internal/adapters/gitlab"
	"github.com/ylcn91/pilot/internal/logging"
)

// handleGitlabIssue handles a new GitLab issue
func (p *Pilot) handleGitlabIssue(ctx context.Context, issue *gitlab.Issue, project *gitlab.Project) error {
	logging.WithComponent("pilot").Info("Received GitLab issue",
		slog.String("project", project.PathWithNamespace),
		slog.Int("iid", issue.IID),
		slog.String("title", issue.Title))

	// Convert to task
	task := gitlab.ConvertIssueToTask(issue, project)

	// Find project for this GitLab project
	projectPath := p.findProjectForGitlabProject(project)
	if projectPath == "" {
		return fmt.Errorf("no project configured for GitLab project %s", project.PathWithNamespace)
	}

	// Notify that task has started
	if p.gitlabNotify != nil {
		if err := p.gitlabNotify.NotifyTaskStarted(ctx, issue.IID, task.ID); err != nil {
			logging.WithComponent("pilot").Warn("Failed to notify task started", slog.Any("error", err))
		}
	}

	// Process ticket through orchestrator
	err := p.orchestrator.ProcessGitlabTicket(ctx, task, projectPath)

	// Update GitLab with result
	if p.gitlabNotify != nil {
		if err != nil {
			if notifyErr := p.gitlabNotify.NotifyTaskFailed(ctx, issue.IID, err.Error()); notifyErr != nil {
				logging.WithComponent("pilot").Warn("Failed to notify task failed", slog.Any("error", notifyErr))
			}
		}
		// Success notification handled by orchestrator when MR is created
	}

	return err
}

// findProjectForGitlabProject finds the project path for a GitLab project
func (p *Pilot) findProjectForGitlabProject(project *gitlab.Project) string {
	// Try to match by project name or path
	for _, proj := range p.config.Projects {
		// Match by name
		if project.Name == proj.Name {
			return proj.Path
		}
		// Match by path with namespace (namespace/project)
		if project.PathWithNamespace == proj.Name {
			return proj.Path
		}
	}

	// Return first project as fallback
	if len(p.config.Projects) > 0 {
		return p.config.Projects[0].Path
	}

	return ""
}

// parseGitlabWebhookPayload parses a GitLab webhook payload from a generic map
func (p *Pilot) parseGitlabWebhookPayload(payload map[string]interface{}) *gitlab.IssueWebhookPayload {
	result := &gitlab.IssueWebhookPayload{}

	// Parse object_kind and event_type
	if v, ok := payload["object_kind"].(string); ok {
		result.ObjectKind = v
	}
	if v, ok := payload["event_type"].(string); ok {
		result.EventType = v
	}

	// Parse project
	if projectData, ok := payload["project"].(map[string]interface{}); ok {
		result.Project = &gitlab.WebhookProject{}
		if v, ok := projectData["id"].(float64); ok {
			result.Project.ID = int(v)
		}
		if v, ok := projectData["name"].(string); ok {
			result.Project.Name = v
		}
		if v, ok := projectData["path_with_namespace"].(string); ok {
			result.Project.PathWithNamespace = v
		}
		if v, ok := projectData["web_url"].(string); ok {
			result.Project.WebURL = v
		}
		if v, ok := projectData["default_branch"].(string); ok {
			result.Project.DefaultBranch = v
		}
	}

	// Parse object_attributes (issue details)
	if attrs, ok := payload["object_attributes"].(map[string]interface{}); ok {
		result.ObjectAttributes = &gitlab.IssueAttributes{}
		if v, ok := attrs["id"].(float64); ok {
			result.ObjectAttributes.ID = int(v)
		}
		if v, ok := attrs["iid"].(float64); ok {
			result.ObjectAttributes.IID = int(v)
		}
		if v, ok := attrs["title"].(string); ok {
			result.ObjectAttributes.Title = v
		}
		if v, ok := attrs["description"].(string); ok {
			result.ObjectAttributes.Description = v
		}
		if v, ok := attrs["state"].(string); ok {
			result.ObjectAttributes.State = v
		}
		if v, ok := attrs["action"].(string); ok {
			result.ObjectAttributes.Action = v
		}
		if v, ok := attrs["url"].(string); ok {
			result.ObjectAttributes.URL = v
		}
	}

	// Parse labels
	if labelsData, ok := payload["labels"].([]interface{}); ok {
		for _, l := range labelsData {
			if labelMap, ok := l.(map[string]interface{}); ok {
				label := &gitlab.WebhookLabel{}
				if v, ok := labelMap["id"].(float64); ok {
					label.ID = int(v)
				}
				if v, ok := labelMap["title"].(string); ok {
					label.Title = v
				}
				result.Labels = append(result.Labels, label)
			}
		}
	}

	// Parse changes (for update events)
	if changesData, ok := payload["changes"].(map[string]interface{}); ok {
		result.Changes = &gitlab.IssueChanges{}
		if labelsChange, ok := changesData["labels"].(map[string]interface{}); ok {
			result.Changes.Labels = &gitlab.LabelChange{}

			if prev, ok := labelsChange["previous"].([]interface{}); ok {
				for _, l := range prev {
					if labelMap, ok := l.(map[string]interface{}); ok {
						label := &gitlab.WebhookLabel{}
						if v, ok := labelMap["id"].(float64); ok {
							label.ID = int(v)
						}
						if v, ok := labelMap["title"].(string); ok {
							label.Title = v
						}
						result.Changes.Labels.Previous = append(result.Changes.Labels.Previous, label)
					}
				}
			}

			if curr, ok := labelsChange["current"].([]interface{}); ok {
				for _, l := range curr {
					if labelMap, ok := l.(map[string]interface{}); ok {
						label := &gitlab.WebhookLabel{}
						if v, ok := labelMap["id"].(float64); ok {
							label.ID = int(v)
						}
						if v, ok := labelMap["title"].(string); ok {
							label.Title = v
						}
						result.Changes.Labels.Current = append(result.Changes.Labels.Current, label)
					}
				}
			}
		}
	}

	return result
}

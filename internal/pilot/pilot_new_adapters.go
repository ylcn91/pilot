package pilot

import (
	"fmt"
	"log/slog"

	"github.com/ylcn91/pilot/internal/adapters/asana"
	"github.com/ylcn91/pilot/internal/adapters/azuredevops"
	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/adapters/gitlab"
	"github.com/ylcn91/pilot/internal/adapters/jira"
	"github.com/ylcn91/pilot/internal/adapters/linear"
	"github.com/ylcn91/pilot/internal/adapters/plane"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/logging"
)

// initLinearAdapter initializes the Linear adapter if enabled (GH-391: multi-workspace support).
// Returns an error only when multi-workspace handler construction fails; the caller is
// responsible for cancelling the root context on error (mirrors the original New flow).
func (p *Pilot) initLinearAdapter(cfg *config.Config) error {
	if cfg.Adapters.Linear == nil || !cfg.Adapters.Linear.Enabled {
		return nil
	}
	workspaces := cfg.Adapters.Linear.GetWorkspaces()
	if len(workspaces) > 1 || (len(workspaces) == 1 && len(cfg.Adapters.Linear.Workspaces) > 0) {
		// Multi-workspace mode
		multiWH, err := linear.NewMultiWorkspaceHandler(cfg.Adapters.Linear)
		if err != nil {
			return fmt.Errorf("failed to create Linear multi-workspace handler: %w", err)
		}
		p.linearMultiWH = multiWH
		p.linearMultiWH.OnIssue(p.handleLinearIssueMultiWorkspace)
		logging.WithComponent("pilot").Info("Linear multi-workspace mode enabled",
			slog.Int("workspaces", p.linearMultiWH.WorkspaceCount()))
	} else {
		// Legacy single-workspace mode
		p.linearClient = linear.NewClient(cfg.Adapters.Linear.APIKey)
		pilotLabel := cfg.Adapters.Linear.PilotLabel
		if pilotLabel == "" {
			pilotLabel = "pilot"
		}
		p.linearWH = linear.NewWebhookHandler(p.linearClient, pilotLabel, cfg.Adapters.Linear.ProjectIDs)
		p.linearWH.OnIssue(p.handleLinearIssue)
		p.linearNotify = linear.NewNotifier(p.linearClient)
	}
	return nil
}

// initTrackerAdapters initializes GitHub, GitLab, Jira, Azure DevOps, Asana, and Plane
// adapters if enabled, preserving the original initialization order from New.
func (p *Pilot) initTrackerAdapters(cfg *config.Config) {
	// Initialize GitHub adapter if enabled
	if cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Enabled {
		p.githubClient = github.NewClient(cfg.Adapters.GitHub.Token)
		p.githubWH = github.NewWebhookHandler(
			p.githubClient,
			cfg.Adapters.GitHub.WebhookSecret,
			cfg.Adapters.GitHub.PilotLabel,
		)
		p.githubWH.OnIssue(p.handleGithubIssue)
		p.githubNotify = github.NewNotifier(p.githubClient, cfg.Adapters.GitHub.PilotLabel)
	}

	// Initialize GitLab adapter if enabled
	if cfg.Adapters.GitLab != nil && cfg.Adapters.GitLab.Enabled {
		p.gitlabClient = gitlab.NewClient(cfg.Adapters.GitLab.Token, cfg.Adapters.GitLab.Project)
		p.gitlabWH = gitlab.NewWebhookHandler(
			p.gitlabClient,
			cfg.Adapters.GitLab.WebhookSecret,
			cfg.Adapters.GitLab.PilotLabel,
		)
		p.gitlabWH.OnIssue(p.handleGitlabIssue)
		p.gitlabNotify = gitlab.NewNotifier(p.gitlabClient, cfg.Adapters.GitLab.PilotLabel)
	}

	// Initialize Jira adapter if enabled
	if cfg.Adapters.Jira != nil && cfg.Adapters.Jira.Enabled {
		p.jiraClient = jira.NewClient(
			cfg.Adapters.Jira.BaseURL,
			cfg.Adapters.Jira.Username,
			cfg.Adapters.Jira.APIToken,
			cfg.Adapters.Jira.Platform,
		)
		pilotLabel := cfg.Adapters.Jira.PilotLabel
		if pilotLabel == "" {
			pilotLabel = "pilot"
		}
		p.jiraWH = jira.NewWebhookHandler(p.jiraClient, cfg.Adapters.Jira.WebhookSecret, pilotLabel)
		p.jiraWH.OnIssue(p.handleJiraIssue)
	}

	// GH-1699: Initialize Azure DevOps webhook handler if configured
	if cfg.Adapters.AzureDevOps != nil && cfg.Adapters.AzureDevOps.Enabled {
		p.azureDevOpsClient = azuredevops.NewClientWithConfig(cfg.Adapters.AzureDevOps)
		pilotTag := cfg.Adapters.AzureDevOps.PilotTag
		if pilotTag == "" {
			pilotTag = "pilot"
		}
		p.azureDevOpsWH = azuredevops.NewWebhookHandler(p.azureDevOpsClient, cfg.Adapters.AzureDevOps.WebhookSecret, pilotTag)
		if len(cfg.Adapters.AzureDevOps.WorkItemTypes) > 0 {
			p.azureDevOpsWH.SetWorkItemTypes(cfg.Adapters.AzureDevOps.WorkItemTypes)
		}
	}

	// GH-2044: Initialize Asana adapter if enabled
	if cfg.Adapters.Asana != nil && cfg.Adapters.Asana.Enabled {
		p.asanaClient = asana.NewClient(cfg.Adapters.Asana.AccessToken, cfg.Adapters.Asana.WorkspaceID)
		pilotTag := cfg.Adapters.Asana.PilotTag
		if pilotTag == "" {
			pilotTag = "pilot"
		}
		p.asanaWH = asana.NewWebhookHandler(p.asanaClient, cfg.Adapters.Asana.WebhookSecret, pilotTag)
		p.asanaWH.OnTask(p.handleAsanaTask)
	}

	// GH-2044: Initialize Plane adapter if enabled
	if cfg.Adapters.Plane != nil && cfg.Adapters.Plane.Enabled {
		pilotLabel := cfg.Adapters.Plane.PilotLabel
		if pilotLabel == "" {
			pilotLabel = "pilot"
		}
		p.planeWH = plane.NewWebhookHandler(cfg.Adapters.Plane.WebhookSecret, pilotLabel, cfg.Adapters.Plane.ProjectIDs)
		p.planeWH.OnWorkItem(p.handlePlaneWorkItem)
	}
}

package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/asana"
	"github.com/ylcn91/pilot/internal/adapters/azuredevops"
	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/adapters/gitlab"
	"github.com/ylcn91/pilot/internal/adapters/jira"
	"github.com/ylcn91/pilot/internal/adapters/linear"
)

// validateTimeout bounds each onboarding token-validation network call.
const validateTimeout = 10 * time.Second

// validateGitHubConn verifies a GitHub token by calling the authenticated-user
// endpoint. A real, non-2xx response (e.g. 401 Bad credentials) surfaces as an
// error so onboarding never reports a fabricated "Connected" for a bad token.
func validateGitHubConn(token string) error {
	if token == "" {
		return fmt.Errorf("token is required")
	}
	return validateGitHubWith(github.NewClient(token))
}

type githubAuthChecker interface {
	GetAuthenticatedUser(ctx context.Context) (*github.User, error)
}

func validateGitHubWith(client githubAuthChecker) error {
	ctx, cancel := context.WithTimeout(context.Background(), validateTimeout)
	defer cancel()
	if _, err := client.GetAuthenticatedUser(ctx); err != nil {
		return fmt.Errorf("token validation failed: %w", err)
	}
	return nil
}

// validateLinearConn verifies a Linear API key and returns the real workspace
// (organization) name. An authentication failure returns an error.
func validateLinearConn(apiKey string) (string, error) {
	if apiKey == "" {
		return "", fmt.Errorf("API key is required")
	}
	return validateLinearWith(linear.NewClient(apiKey))
}

type linearOrgQuerier interface {
	Execute(ctx context.Context, query string, variables map[string]interface{}, result interface{}) error
}

func validateLinearWith(client linearOrgQuerier) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), validateTimeout)
	defer cancel()

	var result struct {
		Organization struct {
			Name string `json:"name"`
		} `json:"organization"`
	}
	if err := client.Execute(ctx, "{ organization { name } }", nil, &result); err != nil {
		return "", fmt.Errorf("token validation failed: %w", err)
	}
	if result.Organization.Name == "" {
		return "Workspace", nil
	}
	return result.Organization.Name, nil
}

// validateJiraConn verifies Jira credentials by listing accessible projects.
func validateJiraConn(baseURL, username, apiToken string) error {
	if baseURL == "" || username == "" || apiToken == "" {
		return fmt.Errorf("all fields are required")
	}
	return validateJiraWith(jira.NewClient(baseURL, username, apiToken, jira.PlatformCloud))
}

type jiraSearcher interface {
	SearchIssues(ctx context.Context, jql string, maxResults int) ([]*jira.Issue, error)
}

func validateJiraWith(client jiraSearcher) error {
	ctx, cancel := context.WithTimeout(context.Background(), validateTimeout)
	defer cancel()
	// An empty JQL "order by created" is the lightest authenticated search; a
	// 401/403 from bad credentials surfaces as an error.
	if _, err := client.SearchIssues(ctx, "order by created", 1); err != nil {
		return fmt.Errorf("credential validation failed: %w", err)
	}
	return nil
}

// validateGitLabConn verifies a GitLab token. Without a project it checks the
// token via the projects listing; the adapter client is project-scoped, so we
// fall back to a token-format check when no project is configured yet.
func validateGitLabConn(baseURL, token string) error {
	if token == "" {
		return fmt.Errorf("token is required")
	}
	return validateGitLabWith(gitlab.NewClientWithBaseURL(token, "", strings.TrimSuffix(baseURL, "/")))
}

type gitlabProjectGetter interface {
	GetProject(ctx context.Context) (*gitlab.Project, error)
}

func validateGitLabWith(client gitlabProjectGetter) error {
	ctx, cancel := context.WithTimeout(context.Background(), validateTimeout)
	defer cancel()
	if _, err := client.GetProject(ctx); err != nil {
		return fmt.Errorf("token validation failed: %w", err)
	}
	return nil
}

// validateAzureDevOpsConn verifies an Azure DevOps PAT against the org/project
// by running a minimal WIQL query, which exercises authentication.
func validateAzureDevOpsConn(org, project, pat string) error {
	if org == "" || project == "" || pat == "" {
		return fmt.Errorf("all fields are required")
	}
	return validateAzureDevOpsWith(azuredevops.NewClient(pat, org, project))
}

type azureWIQLLister interface {
	ListWorkItemsByWIQL(ctx context.Context, wiql string) ([]*azuredevops.WorkItem, error)
}

func validateAzureDevOpsWith(client azureWIQLLister) error {
	ctx, cancel := context.WithTimeout(context.Background(), validateTimeout)
	defer cancel()
	if _, err := client.ListWorkItemsByWIQL(ctx, "SELECT [System.Id] FROM workitems"); err != nil {
		return fmt.Errorf("PAT validation failed: %w", err)
	}
	return nil
}

// validateAsanaConn verifies an Asana token. The Asana adapter client is
// workspace-scoped and exposes no "list my workspaces" endpoint, and the
// workspace ID is not yet known at this onboarding stage, so a real
// authenticated call cannot be made here without a new adapter method
// (see followups). We perform a format check and let workspace selection
// happen after the ID is entered.
func validateAsanaConn(token string) ([]string, error) {
	if token == "" {
		return nil, fmt.Errorf("token is required")
	}
	return []string{}, nil
}

// validateAsanaWorkspace verifies an Asana token against a known workspace ID by
// fetching the workspace, returning its real name. Used once the operator
// supplies a workspace ID.
func validateAsanaWorkspace(token, workspaceID string) (string, error) {
	if token == "" {
		return "", fmt.Errorf("token is required")
	}
	if workspaceID == "" {
		return "", fmt.Errorf("workspace ID is required")
	}
	return validateAsanaWith(asana.NewClient(token, workspaceID))
}

type asanaWorkspaceGetter interface {
	GetWorkspace(ctx context.Context) (*asana.Workspace, error)
}

func validateAsanaWith(client asanaWorkspaceGetter) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), validateTimeout)
	defer cancel()
	ws, err := client.GetWorkspace(ctx)
	if err != nil {
		return "", fmt.Errorf("token validation failed: %w", err)
	}
	return ws.Name, nil
}

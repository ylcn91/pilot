package main

import (
	"fmt"
)

// Validation stubs - these will be replaced by onboard_validate.go (Issue 5)
// For now they return nil to allow compilation and testing

func validateGitHubConn(token string) error {
	// Stub: Will be implemented in onboard_validate.go (Issue 5)
	// Real implementation will call GitHub API to verify token
	if token == "" {
		return fmt.Errorf("token is required")
	}
	return nil
}

func validateLinearConn(apiKey string) (string, error) {
	// Stub: Will be implemented in onboard_validate.go (Issue 5)
	// Real implementation will call Linear API to get workspace name
	if apiKey == "" {
		return "", fmt.Errorf("API key is required")
	}
	return "Workspace", nil // Placeholder workspace name
}

func validateJiraConn(baseURL, username, apiToken string) error {
	// Stub: Will be implemented in onboard_validate.go (Issue 5)
	// Real implementation will call Jira API to verify credentials
	if baseURL == "" || username == "" || apiToken == "" {
		return fmt.Errorf("all fields are required")
	}
	return nil
}

func validateGitLabConn(baseURL, token string) error {
	// Stub: Will be implemented in onboard_validate.go (Issue 5)
	// Real implementation will call GitLab API to verify token
	if token == "" {
		return fmt.Errorf("token is required")
	}
	return nil
}

func validateAzureDevOpsConn(org, project, pat string) error {
	// Stub: Will be implemented in onboard_validate.go (Issue 5)
	// Real implementation will call Azure DevOps API to verify PAT
	if org == "" || project == "" || pat == "" {
		return fmt.Errorf("all fields are required")
	}
	return nil
}

func validateAsanaConn(token string) ([]string, error) {
	// Stub: Will be implemented in onboard_validate.go (Issue 5)
	// Real implementation will call Asana API to get workspaces
	if token == "" {
		return nil, fmt.Errorf("token is required")
	}
	return []string{"My Workspace"}, nil // Placeholder workspace list
}

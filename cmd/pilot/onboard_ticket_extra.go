package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/asana"
	"github.com/ylcn91/pilot/internal/adapters/azuredevops"
	"github.com/ylcn91/pilot/internal/adapters/gitlab"
)

// onboardGitLabTickets sets up GitLab Issues as a ticket source
func onboardGitLabTickets(state *OnboardState) error {
	fmt.Println()
	fmt.Println("GitLab Issues Setup")
	fmt.Println("─────────────────────────")

	// Initialize config if needed
	if state.Config.Adapters.GitLab == nil {
		state.Config.Adapters.GitLab = gitlab.DefaultConfig()
	}

	// Base URL
	fmt.Print("  Base URL [https://gitlab.com]: ")
	baseURL := readOnboardLine(state.Reader)
	if baseURL == "" {
		baseURL = "https://gitlab.com"
	}
	state.Config.Adapters.GitLab.BaseURL = strings.TrimSuffix(baseURL, "/")

	// Token
	fmt.Println()
	fmt.Println("  Create a token at: Settings > Access Tokens")
	fmt.Println("  Required scopes: api, read_repository")
	fmt.Print("  GitLab token: ")
	token := readOnboardLine(state.Reader)
	if token == "" {
		fmt.Println("  ○ Skipped - no token provided")
		return nil
	}
	state.Config.Adapters.GitLab.Token = token

	// Validate connection
	fmt.Print("  Validating... ")
	if err := validateGitLabConn(baseURL, token); err != nil {
		fmt.Printf("✗ %v\n", err)
		return handleValidationFailure(state, "GitLab", func() error {
			return onboardGitLabTickets(state)
		})
	}
	fmt.Println("✓ Connected")

	// Project path
	fmt.Println()
	fmt.Println("  Example: namespace/project")
	fmt.Print("  Project path: ")
	projectPath := readOnboardLine(state.Reader)
	if projectPath != "" {
		state.Config.Adapters.GitLab.Project = projectPath
	}

	// Label
	fmt.Print("  Pilot label [pilot]: ")
	label := readOnboardLine(state.Reader)
	if label == "" {
		label = "pilot"
	}
	state.Config.Adapters.GitLab.PilotLabel = label

	// Enable polling
	if state.Config.Adapters.GitLab.Polling == nil {
		state.Config.Adapters.GitLab.Polling = &gitlab.PollingConfig{
			Interval: 30 * time.Second,
			Label:    label,
		}
	}
	state.Config.Adapters.GitLab.Polling.Enabled = true
	state.Config.Adapters.GitLab.Polling.Label = label

	state.Config.Adapters.GitLab.Enabled = true
	fmt.Println("  ✓ GitLab Issues configured")

	return nil
}

// onboardAzureDevOpsTickets sets up Azure DevOps work items as a ticket source
func onboardAzureDevOpsTickets(state *OnboardState) error {
	fmt.Println()
	fmt.Println("Azure DevOps Setup")
	fmt.Println("─────────────────────────")

	// Initialize config if needed
	if state.Config.Adapters.AzureDevOps == nil {
		state.Config.Adapters.AzureDevOps = azuredevops.DefaultConfig()
	}

	// Organization
	fmt.Println("  Example: https://dev.azure.com/myorg → organization is 'myorg'")
	fmt.Print("  Organization: ")
	org := readOnboardLine(state.Reader)
	if org == "" {
		fmt.Println("  ○ Skipped - no organization provided")
		return nil
	}
	state.Config.Adapters.AzureDevOps.Organization = org

	// Project
	fmt.Print("  Project: ")
	project := readOnboardLine(state.Reader)
	if project == "" {
		fmt.Println("  ○ Skipped - no project provided")
		return nil
	}
	state.Config.Adapters.AzureDevOps.Project = project

	// Personal Access Token
	fmt.Println()
	fmt.Println("  Create a PAT at: User Settings > Personal Access Tokens")
	fmt.Println("  Required scopes: Work Items (Read & Write), Code (Read & Write)")
	fmt.Print("  Personal Access Token: ")
	pat := readOnboardLine(state.Reader)
	if pat == "" {
		fmt.Println("  ○ Skipped - no PAT provided")
		return nil
	}
	state.Config.Adapters.AzureDevOps.PAT = pat

	// Validate connection
	fmt.Print("  Validating... ")
	if err := validateAzureDevOpsConn(org, project, pat); err != nil {
		fmt.Printf("✗ %v\n", err)
		return handleValidationFailure(state, "Azure DevOps", func() error {
			return onboardAzureDevOpsTickets(state)
		})
	}
	fmt.Println("✓ Connected")

	// Tag (Azure uses tags, not labels)
	fmt.Print("  Pilot tag [pilot]: ")
	tag := readOnboardLine(state.Reader)
	if tag == "" {
		tag = "pilot"
	}
	state.Config.Adapters.AzureDevOps.PilotTag = tag

	// Enable polling
	if state.Config.Adapters.AzureDevOps.Polling == nil {
		state.Config.Adapters.AzureDevOps.Polling = &azuredevops.PollingConfig{
			Interval: 30 * time.Second,
		}
	}
	state.Config.Adapters.AzureDevOps.Polling.Enabled = true

	state.Config.Adapters.AzureDevOps.Enabled = true
	fmt.Println("  ✓ Azure DevOps configured")

	return nil
}

// onboardAsanaTickets sets up Asana as a ticket source
func onboardAsanaTickets(state *OnboardState) error {
	fmt.Println()
	fmt.Println("Asana Setup")
	fmt.Println("─────────────────────────")

	// Initialize config if needed
	if state.Config.Adapters.Asana == nil {
		state.Config.Adapters.Asana = asana.DefaultConfig()
	}

	// Access Token
	fmt.Println()
	fmt.Println("  Create a token at: https://app.asana.com/0/developer-console")
	fmt.Println("  Go to Personal access tokens > Create new token")
	fmt.Print("  Access token: ")
	token := readOnboardLine(state.Reader)
	if token == "" {
		fmt.Println("  ○ Skipped - no token provided")
		return nil
	}
	state.Config.Adapters.Asana.AccessToken = token

	// Basic token presence check; the authenticated check happens once the
	// workspace ID is known (Asana's API is workspace-scoped).
	if _, err := validateAsanaConn(token); err != nil {
		fmt.Printf("  ✗ %v\n", err)
		return handleValidationFailure(state, "Asana", func() error {
			return onboardAsanaTickets(state)
		})
	}

	// Prompt for workspace ID, then verify the token against it.
	fmt.Print("  Workspace ID (from URL): ")
	workspaceID := readOnboardLine(state.Reader)
	if workspaceID != "" {
		state.Config.Adapters.Asana.WorkspaceID = workspaceID
		fmt.Print("  Validating... ")
		wsName, err := validateAsanaWorkspace(token, workspaceID)
		if err != nil {
			fmt.Printf("✗ %v\n", err)
			return handleValidationFailure(state, "Asana", func() error {
				return onboardAsanaTickets(state)
			})
		}
		fmt.Printf("✓ Connected to %q\n", wsName)
	}

	// Tag
	fmt.Print("  Pilot tag [pilot]: ")
	tag := readOnboardLine(state.Reader)
	if tag == "" {
		tag = "pilot"
	}
	state.Config.Adapters.Asana.PilotTag = tag

	// Enable polling
	if state.Config.Adapters.Asana.Polling == nil {
		state.Config.Adapters.Asana.Polling = &asana.PollingConfig{
			Interval: 30 * time.Second,
		}
	}
	state.Config.Adapters.Asana.Polling.Enabled = true

	state.Config.Adapters.Asana.Enabled = true
	fmt.Println("  ✓ Asana configured")

	return nil
}

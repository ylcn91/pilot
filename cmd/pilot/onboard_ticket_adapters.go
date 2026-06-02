package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/adapters/jira"
	"github.com/ylcn91/pilot/internal/adapters/linear"
)

// onboardGitHubTickets sets up GitHub Issues as a ticket source
func onboardGitHubTickets(state *OnboardState) error {
	fmt.Println()
	fmt.Println("GitHub Issues Setup")
	fmt.Println("─────────────────────────")

	// Initialize config if needed
	if state.Config.Adapters.GitHub == nil {
		state.Config.Adapters.GitHub = github.DefaultConfig()
	}

	// Check for token from environment first
	token := os.Getenv("GITHUB_TOKEN")
	if token != "" {
		fmt.Println("  Found $GITHUB_TOKEN in environment")
		fmt.Print("  Use this token? [Y/n]: ")
		if readOnboardYesNo(state.Reader, true) {
			state.Config.Adapters.GitHub.Token = token
		} else {
			token = ""
		}
	}

	// Prompt for token if not set
	if token == "" {
		fmt.Println()
		fmt.Println("  Create a token at: https://github.com/settings/tokens")
		fmt.Println("  Required scopes: repo")
		fmt.Print("  GitHub token: ")
		token = readOnboardLine(state.Reader)
		if token == "" {
			fmt.Println("  ○ Skipped - no token provided")
			return nil
		}
		state.Config.Adapters.GitHub.Token = token
	}

	// Validate connection
	fmt.Print("  Validating... ")
	if err := validateGitHubConn(token); err != nil {
		fmt.Printf("✗ %v\n", err)
		return handleValidationFailure(state, "GitHub", func() error {
			return onboardGitHubTickets(state)
		})
	}
	fmt.Println("✓ Connected")

	// Pre-fill repo from project config if available
	defaultRepo := ""
	if len(state.Config.Projects) > 0 && state.Config.Projects[0].GitHub != nil {
		gh := state.Config.Projects[0].GitHub
		if gh.Owner != "" && gh.Repo != "" {
			defaultRepo = gh.Owner + "/" + gh.Repo
		}
	}

	// Prompt for repo
	if defaultRepo != "" {
		fmt.Printf("  Repository [%s]: ", defaultRepo)
	} else {
		fmt.Print("  Repository (owner/repo): ")
	}
	repo := readOnboardLine(state.Reader)
	if repo == "" {
		repo = defaultRepo
	}
	if repo != "" {
		state.Config.Adapters.GitHub.Repo = repo
	}

	// Prompt for label
	fmt.Print("  Pilot label [pilot]: ")
	label := readOnboardLine(state.Reader)
	if label == "" {
		label = "pilot"
	}
	state.Config.Adapters.GitHub.PilotLabel = label

	// Enable polling
	if state.Config.Adapters.GitHub.Polling == nil {
		state.Config.Adapters.GitHub.Polling = &github.PollingConfig{
			Interval: 30 * time.Second,
			Label:    label,
		}
	}
	state.Config.Adapters.GitHub.Polling.Enabled = true
	state.Config.Adapters.GitHub.Polling.Label = label

	state.Config.Adapters.GitHub.Enabled = true
	fmt.Println("  ✓ GitHub Issues configured")

	return nil
}

// onboardLinearTickets sets up Linear as a ticket source
func onboardLinearTickets(state *OnboardState) error {
	fmt.Println()
	fmt.Println("Linear Setup")
	fmt.Println("─────────────────────────")

	// Initialize config if needed
	if state.Config.Adapters.Linear == nil {
		state.Config.Adapters.Linear = linear.DefaultConfig()
	}

	// Check for API key from environment first
	apiKey := os.Getenv("LINEAR_API_KEY")
	if apiKey != "" {
		fmt.Println("  Found $LINEAR_API_KEY in environment")
		fmt.Print("  Use this key? [Y/n]: ")
		if readOnboardYesNo(state.Reader, true) {
			state.Config.Adapters.Linear.APIKey = apiKey
		} else {
			apiKey = ""
		}
	}

	// Prompt for API key if not set
	if apiKey == "" {
		fmt.Println()
		fmt.Println("  Get your API key at: https://linear.app/settings/api")
		fmt.Print("  Linear API key: ")
		apiKey = readOnboardLine(state.Reader)
		if apiKey == "" {
			fmt.Println("  ○ Skipped - no API key provided")
			return nil
		}
		state.Config.Adapters.Linear.APIKey = apiKey
	}

	// Validate connection
	fmt.Print("  Validating... ")
	workspaceName, err := validateLinearConn(apiKey)
	if err != nil {
		fmt.Printf("✗ %v\n", err)
		return handleValidationFailure(state, "Linear", func() error {
			return onboardLinearTickets(state)
		})
	}
	fmt.Printf("✓ Connected to %q\n", workspaceName)

	// Prompt for team ID (optional)
	fmt.Print("  Team ID (Enter for all): ")
	teamID := readOnboardLine(state.Reader)
	if teamID != "" {
		state.Config.Adapters.Linear.TeamID = teamID
	}

	// Prompt for label
	fmt.Print("  Pilot label [pilot]: ")
	label := readOnboardLine(state.Reader)
	if label == "" {
		label = "pilot"
	}
	state.Config.Adapters.Linear.PilotLabel = label

	// Enable polling
	if state.Config.Adapters.Linear.Polling == nil {
		state.Config.Adapters.Linear.Polling = &linear.PollingConfig{
			Interval: 30 * time.Second,
		}
	}
	state.Config.Adapters.Linear.Polling.Enabled = true

	state.Config.Adapters.Linear.Enabled = true
	fmt.Println("  ✓ Linear configured")

	// Offer to set up GitHub for PR creation if not configured
	if state.Config.Adapters.GitHub == nil || !state.Config.Adapters.GitHub.Enabled {
		fmt.Println()
		fmt.Print("  Set up GitHub for PR creation? [Y/n]: ")
		if readOnboardYesNo(state.Reader, true) {
			if err := onboardGitHubTickets(state); err != nil {
				return err
			}
		}
	}

	return nil
}

// onboardJiraTickets sets up Jira as a ticket source
func onboardJiraTickets(state *OnboardState) error {
	fmt.Println()
	fmt.Println("Jira Setup")
	fmt.Println("─────────────────────────")

	// Initialize config if needed
	if state.Config.Adapters.Jira == nil {
		state.Config.Adapters.Jira = jira.DefaultConfig()
	}

	// Platform selection
	fmt.Println("  Platform:")
	fmt.Println("    1  Jira Cloud")
	fmt.Println("    2  Jira Server/Data Center")
	fmt.Print("  ▸ ")
	platformChoice := readOnboardLine(state.Reader)
	if platformChoice == "2" {
		state.Config.Adapters.Jira.Platform = jira.PlatformServer
	} else {
		state.Config.Adapters.Jira.Platform = jira.PlatformCloud
	}

	// Base URL
	fmt.Println()
	fmt.Println("  Example: https://company.atlassian.net")
	fmt.Print("  Base URL: ")
	baseURL := readOnboardLine(state.Reader)
	if baseURL == "" {
		fmt.Println("  ○ Skipped - no base URL provided")
		return nil
	}
	state.Config.Adapters.Jira.BaseURL = strings.TrimSuffix(baseURL, "/")

	// Username
	if state.Config.Adapters.Jira.Platform == jira.PlatformCloud {
		fmt.Print("  Email address: ")
	} else {
		fmt.Print("  Username: ")
	}
	username := readOnboardLine(state.Reader)
	if username == "" {
		fmt.Println("  ○ Skipped - no username provided")
		return nil
	}
	state.Config.Adapters.Jira.Username = username

	// API Token
	fmt.Println()
	if state.Config.Adapters.Jira.Platform == jira.PlatformCloud {
		fmt.Println("  Create a token at: https://id.atlassian.com/manage-profile/security/api-tokens")
	} else {
		fmt.Println("  Create a Personal Access Token in Jira settings")
	}
	fmt.Print("  API token: ")
	apiToken := readOnboardLine(state.Reader)
	if apiToken == "" {
		fmt.Println("  ○ Skipped - no API token provided")
		return nil
	}
	state.Config.Adapters.Jira.APIToken = apiToken

	// Validate connection
	fmt.Print("  Validating... ")
	if err := validateJiraConn(state.Config.Adapters.Jira.BaseURL, username, apiToken); err != nil {
		fmt.Printf("✗ %v\n", err)
		return handleValidationFailure(state, "Jira", func() error {
			return onboardJiraTickets(state)
		})
	}
	fmt.Println("✓ Connected")

	// Project key
	fmt.Print("  Project key (e.g., PROJ): ")
	projectKey := readOnboardLine(state.Reader)
	if projectKey != "" {
		state.Config.Adapters.Jira.ProjectKey = projectKey
	}

	// Label
	fmt.Print("  Pilot label [pilot]: ")
	label := readOnboardLine(state.Reader)
	if label == "" {
		label = "pilot"
	}
	state.Config.Adapters.Jira.PilotLabel = label

	// Enable polling
	if state.Config.Adapters.Jira.Polling == nil {
		state.Config.Adapters.Jira.Polling = &jira.PollingConfig{
			Interval: 30 * time.Second,
		}
	}
	state.Config.Adapters.Jira.Polling.Enabled = true

	state.Config.Adapters.Jira.Enabled = true
	fmt.Println("  ✓ Jira configured")

	return nil
}

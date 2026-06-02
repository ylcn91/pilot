// Package main provides the onboard ticket source setup stage.
// GH-1240: Ticket source setup for pilot onboard command.
package main

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/ylcn91/pilot/internal/config"
)

// TicketSource represents an available ticket source adapter
type TicketSource struct {
	Name        string
	Description string
	SetupFunc   func(*OnboardState) error
	IsEnabled   func(*config.Config) bool
}

// getTicketSourcesForPersona returns available ticket sources based on persona
func getTicketSourcesForPersona(persona Persona) []TicketSource {
	allSources := []TicketSource{
		{
			Name:        "GitHub Issues",
			Description: "Track issues in GitHub repositories",
			SetupFunc:   onboardGitHubTickets,
			IsEnabled:   func(cfg *config.Config) bool { return cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Enabled },
		},
		{
			Name:        "Linear",
			Description: "Modern issue tracking for software teams",
			SetupFunc:   onboardLinearTickets,
			IsEnabled:   func(cfg *config.Config) bool { return cfg.Adapters.Linear != nil && cfg.Adapters.Linear.Enabled },
		},
		{
			Name:        "Jira",
			Description: "Atlassian's project management tool",
			SetupFunc:   onboardJiraTickets,
			IsEnabled:   func(cfg *config.Config) bool { return cfg.Adapters.Jira != nil && cfg.Adapters.Jira.Enabled },
		},
		{
			Name:        "GitLab Issues",
			Description: "Track issues in GitLab projects",
			SetupFunc:   onboardGitLabTickets,
			IsEnabled:   func(cfg *config.Config) bool { return cfg.Adapters.GitLab != nil && cfg.Adapters.GitLab.Enabled },
		},
		{
			Name:        "Azure DevOps",
			Description: "Microsoft's DevOps work items",
			SetupFunc:   onboardAzureDevOpsTickets,
			IsEnabled: func(cfg *config.Config) bool {
				return cfg.Adapters.AzureDevOps != nil && cfg.Adapters.AzureDevOps.Enabled
			},
		},
		{
			Name:        "Asana",
			Description: "Task and project management",
			SetupFunc:   onboardAsanaTickets,
			IsEnabled:   func(cfg *config.Config) bool { return cfg.Adapters.Asana != nil && cfg.Adapters.Asana.Enabled },
		},
	}

	switch persona {
	case PersonaSolo:
		// Solo: GitHub only
		return allSources[:1]
	case PersonaTeam:
		// Team: GitHub, Linear, Jira
		return allSources[:3]
	case PersonaEnterprise:
		// Enterprise: All sources
		return allSources
	default:
		return allSources[:3]
	}
}

// onboardTicketSetup runs the ticket source setup stage
func onboardTicketSetup(state *OnboardState) error {
	sources := getTicketSourcesForPersona(state.Persona)

	// Show already configured sources
	configuredSources := []string{}
	for _, src := range sources {
		if src.IsEnabled(state.Config) {
			configuredSources = append(configuredSources, src.Name)
		}
	}

	if len(configuredSources) > 0 {
		fmt.Println()
		fmt.Println("Already configured:")
		for _, name := range configuredSources {
			fmt.Printf("  ✓ %s\n", name)
		}
		fmt.Println()
	}

	// For Solo persona with GitHub already configured, skip
	if state.Persona == PersonaSolo {
		if state.Config.Adapters.GitHub != nil && state.Config.Adapters.GitHub.Enabled {
			fmt.Println("  GitHub Issues already configured")
			return nil
		}
		// Solo goes straight to GitHub setup
		return onboardGitHubTickets(state)
	}

	// For Team/Enterprise, show selection menu
	for {
		availableSources := []TicketSource{}
		for _, src := range sources {
			if !src.IsEnabled(state.Config) {
				availableSources = append(availableSources, src)
			}
		}

		if len(availableSources) == 0 {
			fmt.Println("  All ticket sources configured")
			break
		}

		fmt.Println()
		fmt.Println("Where do your tickets live?")
		fmt.Println()
		for i, src := range availableSources {
			fmt.Printf("  %d  %s\n", i+1, src.Name)
		}
		fmt.Println()

		fmt.Print("▸ ")
		choice := readOnboardLine(state.Reader)
		if choice == "" {
			break
		}

		// Parse choice
		idx := 0
		if _, err := fmt.Sscanf(choice, "%d", &idx); err != nil || idx < 1 || idx > len(availableSources) {
			fmt.Println("  Invalid selection")
			continue
		}

		// Run setup for selected source
		selected := availableSources[idx-1]
		if err := selected.SetupFunc(state); err != nil {
			return err
		}

		// Ask if they want to add another
		fmt.Println()
		fmt.Print("Add another ticket source? [y/N]: ")
		if !readOnboardYesNo(state.Reader, false) {
			break
		}
	}

	return nil
}

// handleValidationFailure presents options when validation fails
func handleValidationFailure(state *OnboardState, adapterName string, retryFunc func() error) error {
	fmt.Println()
	fmt.Println("  1  Re-enter credentials")
	fmt.Println("  2  Continue without validation")
	fmt.Printf("  3  Skip %s\n", adapterName)
	fmt.Print("  ▸ ")

	choice := readOnboardLine(state.Reader)
	switch choice {
	case "1":
		return retryFunc()
	case "2":
		fmt.Println("  ⚠ Continuing without validation")
		return nil
	default:
		fmt.Printf("  ○ Skipped %s\n", adapterName)
		return nil
	}
}

// Helper functions for onboarding - aliases to functions in setup.go/onboard_helpers.go

func readOnboardLine(reader interface{ ReadString(byte) (string, error) }) string {
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(line)
}

func readOnboardYesNo(reader interface{ ReadString(byte) (string, error) }, defaultYes bool) bool {
	return readYesNo(reader.(*bufioReader), defaultYes)
}

type bufioReader = bufio.Reader

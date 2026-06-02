package main

import (
	"bufio"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/banner"
	"github.com/ylcn91/pilot/internal/config"
)

// Persona represents the user's workflow persona
type Persona int

const (
	PersonaSolo Persona = iota + 1
	PersonaTeam
	PersonaEnterprise
)

// String returns the persona display name
func (p Persona) String() string {
	switch p {
	case PersonaSolo:
		return "Solo"
	case PersonaTeam:
		return "Team"
	case PersonaEnterprise:
		return "Enterprise"
	default:
		return "Unknown"
	}
}

// OnboardState holds the state during onboarding wizard
type OnboardState struct {
	Persona      Persona
	Config       *config.Config
	Reader       *bufio.Reader
	StagesTotal  int
	CurrentStage int
}

// Card dimensions for summary cards (21 chars wide, 3 columns)
const (
	summaryCardWidth      = 21
	summaryCardInnerWidth = 17 // cardWidth - 4 (borders)
)

func newOnboardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "onboard",
		Short: "Interactive onboarding wizard",
		Long: `Interactive wizard to configure Pilot for your workflow.

Guides you through:
  - Persona selection (Solo, Team, Enterprise)
  - Project setup
  - Ticket source configuration
  - Notification settings
  - Optional features (for Team/Enterprise)

Examples:
  pilot onboard    # Start interactive onboarding`,
		RunE: runOnboard,
	}

	return cmd
}

func runOnboard(cmd *cobra.Command, args []string) error {
	reader := bufio.NewReader(os.Stdin)

	// Load existing config or create new
	cfg, err := config.Load(config.DefaultConfigPath())
	if err != nil {
		cfg = config.DefaultConfig()
	}

	// Print welcome banner
	fmt.Println()
	banner.PrintWithVersion(version)

	// Check current configuration status
	hasProjects := len(cfg.Projects) > 0
	hasTickets := hasTicketSource(cfg)
	hasNotify := hasNotificationChannel(cfg)

	// Show current status if config exists
	if hasProjects || hasTickets || hasNotify {
		printCurrentStatus(cfg, hasProjects, hasTickets, hasNotify)

		// If all configured, ask to reconfigure
		if hasProjects && hasTickets && hasNotify {
			fmt.Println()
			fmt.Print("  Reconfigure? [y/N]: ")
			if !readYesNo(reader, false) {
				fmt.Println()
				fmt.Println("  Run 'pilot start' to begin")
				return nil
			}
		}
	}

	// Persona selection
	fmt.Println()
	persona := selectPersona(reader)

	// Create onboard state
	state := &OnboardState{
		Persona: persona,
		Config:  cfg,
		Reader:  reader,
	}

	// Set stage count based on persona
	// Stages: Project, Backend, Tickets, Notify, [Optional for Team/Enterprise]
	switch persona {
	case PersonaSolo:
		state.StagesTotal = 5
	case PersonaTeam, PersonaEnterprise:
		state.StagesTotal = 6
	}

	// Execute stages
	state.CurrentStage = 1
	if err := onboardProjectSetup(state); err != nil {
		return err
	}

	state.CurrentStage = 2
	if err := onboardBackendSetup(state); err != nil {
		return err
	}

	state.CurrentStage = 3
	if err := onboardTicketSetup(state); err != nil {
		return err
	}

	state.CurrentStage = 4
	if err := onboardNotifySetup(state); err != nil {
		return err
	}

	// Optional setup for Team/Enterprise
	if persona == PersonaTeam || persona == PersonaEnterprise {
		state.CurrentStage = 5
		if err := onboardOptionalSetup(state); err != nil {
			return err
		}
	}

	// Save config
	configPath := config.DefaultConfigPath()
	if err := config.Save(cfg, configPath); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	// Print summary
	fmt.Println()
	printOnboardSummary(state)

	return nil
}

func selectPersona(reader *bufio.Reader) Persona {
	options := []string{
		"Solo Developer — Personal projects (5 stages)",
		"Team — Shared repos, Slack notifications (6 stages)",
		"Enterprise — Full automation, approvals (6 stages)",
	}

	idx := selectOption(reader, "Select your workflow:", options)

	switch idx {
	case 1:
		return PersonaSolo
	case 2:
		return PersonaTeam
	case 3:
		return PersonaEnterprise
	default:
		return PersonaSolo
	}
}

// Note: onboardProjectSetup is implemented in onboard_project.go
// Note: onboardBackendSetup is implemented in onboard_backend.go
// Note: onboardTicketSetup is implemented in onboard_ticket.go
// Note: onboardNotifySetup is implemented in onboard_notify.go
// Note: onboardOptionalSetup is implemented in onboard_optional.go

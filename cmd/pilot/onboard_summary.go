package main

import (
	"fmt"
	"strings"

	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/executor"
)

// printOnboardSummary prints the final summary with 3-column cards
func printOnboardSummary(state *OnboardState) {
	cfg := state.Config

	printSectionDivider("SUMMARY")

	// Print summary cards in rows of 3
	cards := buildSummaryCards(cfg, state.Persona)

	for i := 0; i < len(cards); i += 3 {
		end := i + 3
		if end > len(cards) {
			end = len(cards)
		}
		printCardRow(cards[i:end])
		if end < len(cards) {
			fmt.Println()
		}
	}

	// Print "Get started" section
	fmt.Println()
	printSectionDivider("GET STARTED")

	printGetStartedCommands(cfg, state.Persona)
}

// SummaryCard represents a summary card
type SummaryCard struct {
	Title      string
	Value      string
	Line1      string
	Line2      string
	Configured bool
}

func buildSummaryCards(cfg *config.Config, persona Persona) []SummaryCard {
	cards := []SummaryCard{
		buildProjectCard(cfg),
		buildBackendCardFromConfig(cfg),
		buildTicketsCard(cfg),
		buildNotifyCard(cfg),
	}

	// Add additional cards for Team/Enterprise
	if persona == PersonaTeam || persona == PersonaEnterprise {
		cards = append(cards,
			buildPRsCard(cfg),
			buildAutopilotCard(cfg),
			buildBriefCard(cfg),
		)
	}

	return cards
}

// buildBackendCardFromConfig builds the summary card for backend selection
func buildBackendCardFromConfig(cfg *config.Config) SummaryCard {
	card := SummaryCard{Title: "BACKEND"}

	if cfg.Executor != nil && cfg.Executor.Type != "" {
		backendType := cfg.Executor.Type
		// Map type to display name
		switch backendType {
		case executor.BackendTypeCodexExec:
			card.Value = "Codex Exec"
		case "claude-code":
			card.Value = "Claude Code"
		case "qwen-code":
			card.Value = "Qwen Code"
		case "opencode":
			card.Value = "OpenCode"
		default:
			card.Value = backendType
		}
		card.Configured = true
	} else {
		card.Value = "Codex Exec"
		card.Line1 = "(default)"
		card.Configured = true
	}

	return card
}

func buildProjectCard(cfg *config.Config) SummaryCard {
	card := SummaryCard{Title: "PROJECT"}

	if len(cfg.Projects) > 0 {
		proj := cfg.Projects[0]
		card.Value = proj.Name
		if proj.GitHub != nil {
			card.Line1 = fmt.Sprintf("%s/%s", proj.GitHub.Owner, proj.GitHub.Repo)
		} else {
			card.Line1 = truncate(proj.Path, summaryCardInnerWidth)
		}
		if proj.Navigator {
			card.Line2 = "✓ Navigator"
		}
		card.Configured = true
	} else {
		card.Value = "—"
		card.Line1 = "not configured"
		card.Configured = false
	}

	return card
}

func buildTicketsCard(cfg *config.Config) SummaryCard {
	card := SummaryCard{Title: "TICKETS"}

	source := getTicketSourceName(cfg)
	if source != "" {
		card.Value = source
		if cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Enabled {
			label := "pilot"
			if cfg.Adapters.GitHub.PilotLabel != "" {
				label = cfg.Adapters.GitHub.PilotLabel
			}
			card.Line1 = fmt.Sprintf("label: %s", label)
			if cfg.Adapters.GitHub.Polling != nil && cfg.Adapters.GitHub.Polling.Enabled {
				card.Line2 = "polling: on"
			}
		}
		card.Configured = true
	} else {
		card.Value = "—"
		card.Line1 = "not configured"
		card.Configured = false
	}

	return card
}

func buildNotifyCard(cfg *config.Config) SummaryCard {
	card := SummaryCard{Title: "NOTIFY"}

	channel := getNotifyChannelName(cfg)
	if channel != "" {
		card.Value = channel
		if cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.Enabled {
			if cfg.Adapters.Telegram.ChatID != "" {
				card.Line1 = fmt.Sprintf("chat: %s", cfg.Adapters.Telegram.ChatID)
			}
		}
		card.Configured = true
	} else {
		card.Value = "—"
		card.Line1 = "not configured"
		card.Configured = false
	}

	return card
}

func buildPRsCard(cfg *config.Config) SummaryCard {
	card := SummaryCard{Title: "PRS"}
	card.Value = "auto-create"
	card.Line1 = "self-review: on"
	card.Configured = true
	return card
}

func buildAutopilotCard(cfg *config.Config) SummaryCard {
	card := SummaryCard{Title: "AUTOPILOT"}

	if cfg.Orchestrator != nil && cfg.Orchestrator.Autopilot != nil {
		env := string(cfg.Orchestrator.Autopilot.Environment)
		if env == "" {
			env = "dev"
		}
		card.Value = env
		card.Line1 = "CI monitor: on"
		card.Configured = true
	} else {
		card.Value = "dev"
		card.Line1 = "CI monitor: off"
		card.Configured = false
	}

	return card
}

func buildBriefCard(cfg *config.Config) SummaryCard {
	card := SummaryCard{Title: "BRIEF"}

	if cfg.Orchestrator != nil && cfg.Orchestrator.DailyBrief != nil && cfg.Orchestrator.DailyBrief.Enabled {
		card.Value = "daily"
		card.Line1 = cfg.Orchestrator.DailyBrief.Schedule
		card.Configured = true
	} else {
		card.Value = "—"
		card.Line1 = "not configured"
		card.Configured = false
	}

	return card
}

func printGetStartedCommands(cfg *config.Config, persona Persona) {
	fmt.Println("  Start Pilot:")
	fmt.Println()

	var flags []string
	if cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Enabled {
		flags = append(flags, "--github")
	}
	if cfg.Adapters.Linear != nil && cfg.Adapters.Linear.Enabled {
		flags = append(flags, "--linear")
	}
	if cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.Enabled {
		flags = append(flags, "--telegram")
	}
	if cfg.Adapters.Slack != nil && cfg.Adapters.Slack.Enabled {
		flags = append(flags, "--slack")
	}

	// Add autopilot for team/enterprise
	if persona == PersonaTeam || persona == PersonaEnterprise {
		flags = append(flags, "--env=stage")
	}

	cmd := "pilot start"
	if len(flags) > 0 {
		cmd = cmd + " " + strings.Join(flags, " ")
	}

	fmt.Printf("    %s\n", onboardValueStyle.Render(cmd))
	fmt.Println()

	// Suggested first commands
	fmt.Println("  Try these commands:")
	fmt.Println()
	fmt.Printf("    %s   # Check system health\n", onboardDimStyle.Render("pilot doctor"))
	fmt.Printf("    %s   # Check status\n", onboardDimStyle.Render("pilot status"))

	if cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Enabled {
		fmt.Printf("    %s   # List queued issues\n", onboardDimStyle.Render("gh issue list --label pilot"))
	}

	fmt.Println()
}

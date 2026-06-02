package main

import (
	"fmt"
	"strings"

	"github.com/ylcn91/pilot/internal/config"
)

func hasTicketSource(cfg *config.Config) bool {
	if cfg.Adapters == nil {
		return false
	}
	if cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Enabled {
		return true
	}
	if cfg.Adapters.Linear != nil && cfg.Adapters.Linear.Enabled {
		return true
	}
	if cfg.Adapters.Jira != nil && cfg.Adapters.Jira.Enabled {
		return true
	}
	if cfg.Adapters.Asana != nil && cfg.Adapters.Asana.Enabled {
		return true
	}
	return false
}

func hasNotificationChannel(cfg *config.Config) bool {
	if cfg.Adapters == nil {
		return false
	}
	if cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.Enabled {
		return true
	}
	if cfg.Adapters.Slack != nil && cfg.Adapters.Slack.Enabled {
		return true
	}
	return false
}

func printCurrentStatus(cfg *config.Config, hasProjects, hasTickets, hasNotify bool) {
	fmt.Println("  Current Configuration:")
	fmt.Println()

	if hasProjects {
		fmt.Printf("    %s Projects: %d configured\n",
			onboardSuccessStyle.Render("✓"),
			len(cfg.Projects))
	} else {
		fmt.Printf("    %s Projects: not configured\n",
			onboardDimStyle.Render("○"))
	}

	if hasTickets {
		source := getTicketSourceName(cfg)
		fmt.Printf("    %s Tickets: %s\n",
			onboardSuccessStyle.Render("✓"),
			source)
	} else {
		fmt.Printf("    %s Tickets: not configured\n",
			onboardDimStyle.Render("○"))
	}

	if hasNotify {
		channel := getNotifyChannelName(cfg)
		fmt.Printf("    %s Notifications: %s\n",
			onboardSuccessStyle.Render("✓"),
			channel)
	} else {
		fmt.Printf("    %s Notifications: not configured\n",
			onboardDimStyle.Render("○"))
	}
}

func getTicketSourceName(cfg *config.Config) string {
	var sources []string
	if cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Enabled {
		sources = append(sources, "GitHub")
	}
	if cfg.Adapters.Linear != nil && cfg.Adapters.Linear.Enabled {
		sources = append(sources, "Linear")
	}
	if cfg.Adapters.Jira != nil && cfg.Adapters.Jira.Enabled {
		sources = append(sources, "Jira")
	}
	if cfg.Adapters.Asana != nil && cfg.Adapters.Asana.Enabled {
		sources = append(sources, "Asana")
	}
	return strings.Join(sources, ", ")
}

func getNotifyChannelName(cfg *config.Config) string {
	var channels []string
	if cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.Enabled {
		channels = append(channels, "Telegram")
	}
	if cfg.Adapters.Slack != nil && cfg.Adapters.Slack.Enabled {
		channels = append(channels, "Slack")
	}
	return strings.Join(channels, ", ")
}

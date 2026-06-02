package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/adapters/discord"
	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/adapters/linear"
	"github.com/ylcn91/pilot/internal/adapters/plane"
	"github.com/ylcn91/pilot/internal/adapters/slack"
	"github.com/ylcn91/pilot/internal/adapters/telegram"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/tunnel"
)

// validateAdapterFlags returns an error when an adapter flag is set but the
// corresponding adapter block is missing or disabled in config. This prevents
// `pilot start --github` from silently auto-enabling a blank adapter and
// launching a no-op poller (GH-2361).
func validateAdapterFlags(cfg *config.Config, cmd *cobra.Command) error {
	type adapterCheck struct {
		flag    string
		enabled bool
		exists  bool
	}
	adapters := []adapterCheck{
		{"github", cfg.Adapters != nil && cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Enabled, cfg.Adapters != nil && cfg.Adapters.GitHub != nil},
		{"linear", cfg.Adapters != nil && cfg.Adapters.Linear != nil && cfg.Adapters.Linear.Enabled, cfg.Adapters != nil && cfg.Adapters.Linear != nil},
		{"plane", cfg.Adapters != nil && cfg.Adapters.Plane != nil && cfg.Adapters.Plane.Enabled, cfg.Adapters != nil && cfg.Adapters.Plane != nil},
		{"discord", cfg.Adapters != nil && cfg.Adapters.Discord != nil && cfg.Adapters.Discord.Enabled, cfg.Adapters != nil && cfg.Adapters.Discord != nil},
	}
	for _, a := range adapters {
		if !cmd.Flags().Changed(a.flag) {
			continue
		}
		if a.enabled {
			continue
		}
		if a.exists {
			return fmt.Errorf("--%s flag set but adapters.%s.enabled is false in config.\nFix: set adapters.%s.enabled: true, or run 'pilot setup'",
				a.flag, a.flag, a.flag)
		}
		return fmt.Errorf("--%s flag set but adapters.%s block is missing in config.\nFix: add adapters.%s block, or run 'pilot setup'",
			a.flag, a.flag, a.flag)
	}
	return nil
}

// applyInputOverrides applies CLI flag overrides to config
// Uses cmd.Flags().Changed() to only apply flags that were explicitly set
func applyInputOverrides(cfg *config.Config, cmd *cobra.Command, telegramFlag, githubFlag, linearFlag, slackFlag, tunnelFlag, planeFlag, discordFlag bool) {
	if cmd.Flags().Changed("telegram") {
		if cfg.Adapters.Telegram == nil {
			cfg.Adapters.Telegram = telegram.DefaultConfig()
		}
		cfg.Adapters.Telegram.Enabled = telegramFlag
		cfg.Adapters.Telegram.Polling = telegramFlag
	}
	if cmd.Flags().Changed("github") {
		if cfg.Adapters.GitHub == nil {
			cfg.Adapters.GitHub = github.DefaultConfig()
		}
		cfg.Adapters.GitHub.Enabled = githubFlag
		if cfg.Adapters.GitHub.Polling == nil {
			cfg.Adapters.GitHub.Polling = &github.PollingConfig{}
		}
		cfg.Adapters.GitHub.Polling.Enabled = githubFlag
	}
	if cmd.Flags().Changed("linear") {
		if cfg.Adapters.Linear == nil {
			cfg.Adapters.Linear = linear.DefaultConfig()
		}
		cfg.Adapters.Linear.Enabled = linearFlag
	}
	if cmd.Flags().Changed("slack") {
		if cfg.Adapters.Slack == nil {
			cfg.Adapters.Slack = slack.DefaultConfig()
		}
		cfg.Adapters.Slack.Enabled = slackFlag
		cfg.Adapters.Slack.SocketMode = slackFlag
	}
	if cmd.Flags().Changed("tunnel") {
		if cfg.Tunnel == nil {
			cfg.Tunnel = tunnel.DefaultConfig()
		}
		cfg.Tunnel.Enabled = tunnelFlag
	}
	if cmd.Flags().Changed("plane") {
		if cfg.Adapters.Plane == nil {
			cfg.Adapters.Plane = plane.DefaultConfig()
		}
		cfg.Adapters.Plane.Enabled = planeFlag
		if cfg.Adapters.Plane.Polling == nil {
			cfg.Adapters.Plane.Polling = &plane.PollingConfig{}
		}
		cfg.Adapters.Plane.Polling.Enabled = planeFlag
	}
	if cmd.Flags().Changed("discord") {
		if cfg.Adapters.Discord == nil {
			cfg.Adapters.Discord = discord.DefaultConfig()
		}
		cfg.Adapters.Discord.Enabled = discordFlag
	}
}

// applyTeamOverrides applies --team and --team-member CLI flag overrides to config (GH-635).
// When --team is set, enables team-based project access scoping.
func applyTeamOverrides(cfg *config.Config, cmd *cobra.Command, teamID, teamMember string) {
	if !cmd.Flags().Changed("team") {
		return
	}
	if cfg.Team == nil {
		cfg.Team = &config.TeamConfig{}
	}
	cfg.Team.Enabled = true
	cfg.Team.TeamID = teamID
	if cmd.Flags().Changed("team-member") {
		cfg.Team.MemberEmail = teamMember
	}
}

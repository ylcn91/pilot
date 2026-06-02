package health

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/ylcn91/pilot/internal/config"
)

// issueSourceAdapters lists config paths to adapters that can ingest issues
// for Pilot to execute. At least one must be enabled for the daemon to
// actually pick up work — otherwise `pilot start` launches a no-op poller.
func enabledIssueSources(cfg *config.Config) []string {
	if cfg.Adapters == nil {
		return nil
	}
	var enabled []string
	if cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Enabled {
		enabled = append(enabled, "github")
	}
	if cfg.Adapters.GitLab != nil && cfg.Adapters.GitLab.Enabled {
		enabled = append(enabled, "gitlab")
	}
	if cfg.Adapters.Linear != nil && cfg.Adapters.Linear.Enabled {
		enabled = append(enabled, "linear")
	}
	if cfg.Adapters.Jira != nil && cfg.Adapters.Jira.Enabled {
		enabled = append(enabled, "jira")
	}
	if cfg.Adapters.Asana != nil && cfg.Adapters.Asana.Enabled {
		enabled = append(enabled, "asana")
	}
	if cfg.Adapters.AzureDevOps != nil && cfg.Adapters.AzureDevOps.Enabled {
		enabled = append(enabled, "azure_devops")
	}
	if cfg.Adapters.Plane != nil && cfg.Adapters.Plane.Enabled {
		enabled = append(enabled, "plane")
	}
	return enabled
}

func checkIssueSourceAdapter(cfg *config.Config) ConfigCheck {
	enabled := enabledIssueSources(cfg)
	if len(enabled) == 0 {
		return ConfigCheck{
			Name:    "adapters",
			Status:  StatusWarning,
			Message: "no issue source enabled",
			Fix:     "Enable at least one of: github, gitlab, linear, jira, asana, azure_devops, plane. Run 'pilot setup'.",
		}
	}
	return ConfigCheck{
		Name:    "adapters",
		Status:  StatusOK,
		Message: strings.Join(enabled, ", "),
	}
}

// checkConfig validates configuration
func checkConfig(cfg *config.Config) []ConfigCheck {
	checks := []ConfigCheck{}

	// Check config file exists
	configPath := config.DefaultConfigPath()
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		checks = append(checks, ConfigCheck{
			Name:    "config file",
			Status:  StatusWarning,
			Message: "using defaults",
			Fix:     "pilot init",
		})
	} else {
		checks = append(checks, ConfigCheck{
			Name:    "config file",
			Status:  StatusOK,
			Message: configPath,
		})
	}

	// Check Telegram config
	if cfg.Adapters != nil && cfg.Adapters.Telegram != nil {
		if cfg.Adapters.Telegram.Enabled {
			if cfg.Adapters.Telegram.BotToken != "" {
				checks = append(checks, ConfigCheck{
					Name:    "telegram.bot_token",
					Status:  StatusOK,
					Message: "configured",
				})
			} else {
				checks = append(checks, ConfigCheck{
					Name:    "telegram.bot_token",
					Status:  StatusError,
					Message: "missing",
					Fix:     "Get token from @BotFather and add to config",
				})
			}

			// Check transcription config
			if cfg.Adapters.Telegram.Transcription != nil {
				if cfg.Adapters.Telegram.Transcription.OpenAIAPIKey != "" {
					checks = append(checks, ConfigCheck{
						Name:    "transcription.openai_api_key",
						Status:  StatusOK,
						Message: "configured (voice enabled)",
					})
				} else {
					checks = append(checks, ConfigCheck{
						Name:    "transcription.openai_api_key",
						Status:  StatusWarning,
						Message: "missing (voice disabled)",
						Fix:     "export OPENAI_API_KEY=\"sk-...\" or add to config",
					})
				}
			}
		}
	}

	// Check Slack config
	if cfg.Adapters != nil && cfg.Adapters.Slack != nil && cfg.Adapters.Slack.Enabled {
		if cfg.Adapters.Slack.BotToken != "" {
			checks = append(checks, ConfigCheck{
				Name:    "slack.bot_token",
				Status:  StatusOK,
				Message: "configured",
			})
		} else {
			checks = append(checks, ConfigCheck{
				Name:    "slack.bot_token",
				Status:  StatusError,
				Message: "enabled but token missing",
				Fix:     "Add xoxb-... token to config",
			})
		}
	}

	// Check that at least one issue-source adapter is enabled when projects
	// are configured. Prevents silent "no poller running" when a user has
	// `projects:` set but no `adapters:` block (GH-2361).
	if len(cfg.Projects) > 0 {
		checks = append(checks, checkIssueSourceAdapter(cfg))
	}

	// Check projects
	if len(cfg.Projects) == 0 {
		checks = append(checks, ConfigCheck{
			Name:    "projects",
			Status:  StatusWarning,
			Message: "none configured",
			Fix:     "Add projects to config.yaml",
		})
	} else {
		validProjects := 0
		for _, p := range cfg.Projects {
			path := expandPath(p.Path)
			if _, err := os.Stat(path); err == nil {
				validProjects++
			}
		}
		if validProjects == len(cfg.Projects) {
			checks = append(checks, ConfigCheck{
				Name:    "projects",
				Status:  StatusOK,
				Message: fmt.Sprintf("%d configured", len(cfg.Projects)),
			})
		} else {
			checks = append(checks, ConfigCheck{
				Name:    "projects",
				Status:  StatusWarning,
				Message: fmt.Sprintf("%d/%d valid paths", validProjects, len(cfg.Projects)),
				Fix:     "Check project paths in config.yaml",
			})
		}
	}

	// Check GitHub config
	if cfg.Adapters != nil && cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Enabled {
		hasToken := cfg.Adapters.GitHub.Token != ""
		// Fallback: check if gh CLI is authenticated
		if !hasToken {
			err := exec.Command("gh", "auth", "status").Run()
			hasToken = err == nil
		}

		if !hasToken {
			checks = append(checks, ConfigCheck{
				Name:    "github.token",
				Status:  StatusError,
				Message: "enabled but token missing",
				Fix:     "Add github.token to config or run: gh auth login",
			})
		} else {
			// Token present — check repo configuration for polling mode
			hasDefaultRepo := cfg.Adapters.GitHub.Repo != ""
			hasProjectRepos := false
			for _, p := range cfg.Projects {
				if p.GitHub != nil && p.GitHub.Owner != "" && p.GitHub.Repo != "" {
					hasProjectRepos = true
					break
				}
			}
			pollingEnabled := cfg.Adapters.GitHub.Polling != nil && cfg.Adapters.GitHub.Polling.Enabled

			if pollingEnabled && !hasDefaultRepo && !hasProjectRepos {
				checks = append(checks, ConfigCheck{
					Name:    "github.repos",
					Status:  StatusWarning,
					Message: "polling enabled but no repos configured",
					Fix:     "Set adapters.github.repo (\"owner/repo\") or add github.owner/repo to each project",
				})
			} else {
				checks = append(checks, ConfigCheck{
					Name:    "github",
					Status:  StatusOK,
					Message: "configured",
				})
			}
		}
	}

	// Detect approval/env mismatch: an env has require_approval=true but the
	// approval.pre_merge stage is disabled → every PR in that env deadlocks.
	if cfg.Orchestrator != nil && cfg.Orchestrator.Autopilot != nil &&
		cfg.Orchestrator.Autopilot.Environments != nil {
		preMergeEnabled := cfg.Approval != nil &&
			cfg.Approval.Enabled &&
			cfg.Approval.PreMerge != nil &&
			cfg.Approval.PreMerge.Enabled
		for envName, envCfg := range cfg.Orchestrator.Autopilot.Environments {
			if envCfg != nil && envCfg.RequireApproval && !preMergeEnabled {
				checks = append(checks, ConfigCheck{
					Name:    "approval-misconfig",
					Status:  StatusError,
					Message: fmt.Sprintf("env %q has require_approval=true but approval.pre_merge.enabled=false → all PRs will deadlock until enabled or env is changed", envName),
					Fix:     "Set approval.enabled: true + approval.pre_merge.enabled: true + add an approver, or set require_approval: false for the environment",
				})
				break // one diagnostic per run is enough
			}
		}
	}

	// Check daily brief schedule
	if cfg.Orchestrator != nil && cfg.Orchestrator.DailyBrief != nil {
		if cfg.Orchestrator.DailyBrief.Enabled {
			if cfg.Orchestrator.DailyBrief.Schedule == "" {
				checks = append(checks, ConfigCheck{
					Name:    "daily_brief.schedule",
					Status:  StatusWarning,
					Message: "enabled but no schedule set",
					Fix:     "Add schedule: \"0 9 * * 1-5\" to config",
				})
			} else {
				checks = append(checks, ConfigCheck{
					Name:    "daily_brief",
					Status:  StatusOK,
					Message: cfg.Orchestrator.DailyBrief.Schedule,
				})
			}
		}
	}

	return checks
}

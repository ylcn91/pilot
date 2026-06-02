package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/ylcn91/pilot/internal/alerts"
	"github.com/ylcn91/pilot/internal/budget"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/logging"
	"github.com/ylcn91/pilot/internal/pilot"
	"github.com/ylcn91/pilot/internal/teams"
)

// wireGatewayBudget creates the gateway-mode budget enforcer and wires per-task
// token/duration limits into the executor stream (GH-539). It sets gw.Enforcer
// when budget enforcement is enabled and a store exists; otherwise it logs why
// enforcement is disabled (GH-1019). Behavior is identical to the original
// inline block.
func wireGatewayBudget(gw *gatewayInfra, cfg *config.Config) {
	if cfg.Budget != nil && cfg.Budget.Enabled && gw.Store != nil {
		gw.Enforcer = budget.NewEnforcer(cfg.Budget, gw.Store)
		if gw.AlertsEngine != nil {
			gw.Enforcer.OnAlert(func(alertType, message, severity string) {
				gw.AlertsEngine.ProcessEvent(alerts.Event{
					Type:      alerts.EventTypeBudgetWarning,
					Error:     message,
					Metadata:  map[string]string{"alert_type": alertType, "severity": severity},
					Timestamp: time.Now(),
				})
			})
		}
		logging.WithComponent("start").Info("budget enforcement enabled (gateway mode)",
			slog.Float64("daily_limit", cfg.Budget.DailyLimit),
			slog.Float64("monthly_limit", cfg.Budget.MonthlyLimit),
		)
		// GH-539: Wire per-task token/duration limits into executor stream (gateway mode)
		maxTokens, maxDuration := gw.Enforcer.GetPerTaskLimits()
		if gw.Runner != nil && (maxTokens > 0 || maxDuration > 0) {
			var gwTaskLimiters sync.Map
			gw.Runner.SetTokenLimitCheck(func(taskID string, deltaInput, deltaOutput int64) bool {
				val, _ := gwTaskLimiters.LoadOrStore(taskID, budget.NewTaskLimiter(maxTokens, maxDuration))
				limiter := val.(*budget.TaskLimiter)
				totalDelta := deltaInput + deltaOutput
				if totalDelta > 0 {
					if !limiter.AddTokens(totalDelta) {
						return false
					}
				}
				if !limiter.CheckDuration() {
					return false
				}
				return true
			})
			logging.WithComponent("start").Info("per-task budget limits enabled (gateway mode)",
				slog.Int64("max_tokens", maxTokens),
				slog.Duration("max_duration", maxDuration),
			)
		}
	} else {
		// GH-1019: Log why budget is disabled for debugging
		logging.WithComponent("start").Debug("budget enforcement disabled (gateway mode)",
			slog.Bool("config_nil", cfg.Budget == nil),
			slog.Bool("enabled", cfg.Budget != nil && cfg.Budget.Enabled),
			slog.Bool("store_nil", gw.Store == nil),
		)
	}
}

// wireGatewayTeams initializes the teams service when --team is provided
// (GH-633) and appends pilot.WithTeamsService to pilotOpts. It returns the
// opened *sql.DB so the caller can close it at shutdown. On any failure it
// closes the DB it opened (matching the original early returns) and returns a
// non-nil error for the caller to propagate verbatim. cfg.TeamID may be
// rewritten from a team name to its resolved ID, exactly as before.
func wireGatewayTeams(cfg *config.Config, pilotOpts *[]pilot.Option) (*sql.DB, error) {
	if cfg.TeamID == "" {
		return nil, nil
	}
	dbPath := filepath.Join(cfg.Memory.Path, "pilot.db")
	teamsDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open teams database: %w", err)
	}
	teamsStore, storeErr := teams.NewStore(teamsDB)
	if storeErr != nil {
		_ = teamsDB.Close()
		return nil, fmt.Errorf("failed to create teams store: %w", storeErr)
	}
	teamsSvc := teams.NewService(teamsStore)

	// Verify team exists
	team, teamErr := teamsSvc.GetTeam(cfg.TeamID)
	if teamErr != nil || team == nil {
		// Try by name
		team, teamErr = teamsSvc.GetTeamByName(cfg.TeamID)
		if teamErr != nil || team == nil {
			_ = teamsDB.Close()
			return nil, fmt.Errorf("team %q not found — create it with: pilot team create <name> --owner <email>", cfg.TeamID)
		}
		// Resolve name to ID
		cfg.TeamID = team.ID
	}

	*pilotOpts = append(*pilotOpts, pilot.WithTeamsService(teamsSvc))
	logging.WithComponent("start").Info("teams service initialized",
		slog.String("team_id", team.ID),
		slog.String("team_name", team.Name))
	return teamsDB, nil
}

// printAdapterStatus prints the per-adapter startup status lines for headless
// gateway mode (GH-349, GH-350, GH-393, GH-652, GH-2045). The condition checks
// and output strings are unchanged from the original inline block.
func printAdapterStatus(cfg *config.Config, hasTelegram, hasGithubPolling, hasSlack bool) {
	// Show Telegram status in gateway mode (GH-349)
	if hasTelegram && cfg.Adapters.Telegram.Polling {
		fmt.Println("📱 Telegram polling active")
	}

	// Show GitHub status in gateway mode (GH-350)
	if hasGithubPolling && cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Enabled &&
		cfg.Adapters.GitHub.Polling != nil && cfg.Adapters.GitHub.Polling.Enabled {
		fmt.Printf("🐙 GitHub polling: %s\n", cfg.Adapters.GitHub.Repo)
	}

	// Show Slack status in gateway mode (GH-652)
	if hasSlack {
		fmt.Println("💬 Slack Socket Mode active")
	}

	// Show Linear status in gateway mode (GH-393)
	if cfg.Adapters.Linear != nil && cfg.Adapters.Linear.Enabled &&
		cfg.Adapters.Linear.Polling != nil && cfg.Adapters.Linear.Polling.Enabled {
		workspaces := cfg.Adapters.Linear.GetWorkspaces()
		for _, ws := range workspaces {
			fmt.Printf("📊 Linear polling: %s/%s\n", ws.Name, ws.TeamID)
		}
	}

	// Show GitLab status (GH-2045)
	if cfg.Adapters.GitLab != nil && cfg.Adapters.GitLab.Enabled {
		if cfg.Adapters.GitLab.Polling != nil && cfg.Adapters.GitLab.Polling.Enabled {
			fmt.Println("🦊 GitLab polling active")
		} else {
			fmt.Println("🦊 GitLab webhooks enabled")
		}
	}

	// Show Jira status (GH-2045)
	if cfg.Adapters.Jira != nil && cfg.Adapters.Jira.Enabled {
		if cfg.Adapters.Jira.Polling != nil && cfg.Adapters.Jira.Polling.Enabled {
			fmt.Println("🎫 Jira polling active")
		} else {
			fmt.Println("🎫 Jira webhooks enabled")
		}
	}

	// Show Asana status (GH-2045)
	if cfg.Adapters.Asana != nil && cfg.Adapters.Asana.Enabled {
		if cfg.Adapters.Asana.Polling != nil && cfg.Adapters.Asana.Polling.Enabled {
			fmt.Println("📋 Asana polling active")
		} else {
			fmt.Println("📋 Asana webhooks enabled")
		}
	}

	// Show Azure DevOps status (GH-2045)
	if cfg.Adapters.AzureDevOps != nil && cfg.Adapters.AzureDevOps.Enabled {
		if cfg.Adapters.AzureDevOps.Polling != nil && cfg.Adapters.AzureDevOps.Polling.Enabled {
			fmt.Println("🔷 Azure DevOps polling active")
		} else {
			fmt.Println("🔷 Azure DevOps webhooks enabled")
		}
	}

	// Show Plane status (GH-2045)
	if cfg.Adapters.Plane != nil && cfg.Adapters.Plane.Enabled {
		if cfg.Adapters.Plane.Polling != nil && cfg.Adapters.Plane.Polling.Enabled {
			fmt.Println("✈️  Plane polling active")
		} else {
			fmt.Println("✈️  Plane webhooks enabled")
		}
	}

	// Show Discord status (GH-2045)
	if cfg.Adapters.Discord != nil && cfg.Adapters.Discord.Enabled {
		fmt.Println("🎮 Discord gateway enabled")
	}
}

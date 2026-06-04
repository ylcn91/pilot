package main

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/logging"
)

// resolveGitHubExecutionParams resolves the execution mode and timing
// parameters for GitHub polling from the orchestrator execution config.
func resolveGitHubExecutionParams(cfg *config.Config) (execMode github.ExecutionMode, waitForMerge bool, pollInterval, prTimeout time.Duration, modeStr string) {
	// Determine execution mode from config
	execMode = github.ExecutionModeSequential // Default to sequential
	waitForMerge = true
	pollInterval = 30 * time.Second
	prTimeout = 1 * time.Hour

	if cfg.Orchestrator != nil && cfg.Orchestrator.Execution != nil {
		execCfg := cfg.Orchestrator.Execution
		if execCfg.Mode == "parallel" {
			execMode = github.ExecutionModeParallel
		}
		waitForMerge = execCfg.WaitForMerge
		if execCfg.PollInterval > 0 {
			pollInterval = execCfg.PollInterval
		}
		if execCfg.PRTimeout > 0 {
			prTimeout = execCfg.PRTimeout
		}
	}

	modeStr = "sequential"
	if execMode == github.ExecutionModeParallel {
		modeStr = "parallel"
	}

	return execMode, waitForMerge, pollInterval, prTimeout, modeStr
}

func (p *pollingRuntime) startGitHubPolling() {
	cfg := p.cfg
	projectPath := p.projectPath
	autopilotControllers := p.autopilotControllers

	// GH-929: Start GitHub polling for multiple repos if enabled
	var ghPollers []*github.Poller
	polledRepos := make(map[string]bool) // Track repos already polled to avoid duplicates

	if cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Enabled &&
		cfg.Adapters.GitHub.Polling != nil && cfg.Adapters.GitHub.Polling.Enabled {

		token := cfg.Adapters.GitHub.Token
		if token == "" {
			token = os.Getenv("GITHUB_TOKEN")
		}

		if token != "" {
			client := github.NewClient(token)
			// GH-30: route non-2xx GitHub API errors into autopilot metrics so
			// api_errors_total / api_error_rate are non-zero and the
			// api_error_rate_high alert can fire.
			if p.autopilotController != nil {
				client.WithAPIErrorRecorder(p.autopilotController.Metrics())
			}
			label := cfg.Adapters.GitHub.Polling.Label
			if label == "" {
				label = cfg.Adapters.GitHub.PilotLabel
			}
			interval := cfg.Adapters.GitHub.Polling.Interval
			if interval == 0 {
				interval = 30 * time.Second
			}

			execMode, waitForMerge, pollInterval, prTimeout, modeStr := resolveGitHubExecutionParams(cfg)

			// Create poller for default repo (adapters.github.repo)
			if cfg.Adapters.GitHub.Repo != "" {
				polledRepos[cfg.Adapters.GitHub.Repo] = true
				poller, err := p.createRepoPoller(cfg.Adapters.GitHub.Repo, projectPath, client, label, interval, execMode, waitForMerge, pollInterval, prTimeout)
				if err != nil {
					if !p.dashboardMode {
						fmt.Printf("⚠️  GitHub polling disabled for %s: %v\n", cfg.Adapters.GitHub.Repo, err)
					}
				} else {
					ghPollers = append(ghPollers, poller)
					if !p.dashboardMode {
						fmt.Printf("🐙 GitHub polling enabled: %s (every %s, mode: %s)\n", cfg.Adapters.GitHub.Repo, interval, modeStr)
					}
				}
			}

			// GH-929: Create pollers for each project with GitHub config
			for _, proj := range cfg.Projects {
				if proj.GitHub == nil || proj.GitHub.Owner == "" || proj.GitHub.Repo == "" {
					continue
				}
				repoFullName := fmt.Sprintf("%s/%s", proj.GitHub.Owner, proj.GitHub.Repo)
				if polledRepos[repoFullName] {
					continue // Skip duplicates
				}
				polledRepos[repoFullName] = true

				projPath := proj.Path
				if projPath == "" {
					projPath = projectPath // Fall back to default project path
				}

				poller, err := p.createRepoPoller(repoFullName, projPath, client, label, interval, execMode, waitForMerge, pollInterval, prTimeout)
				if err != nil {
					logging.WithComponent("github").Warn("Failed to create poller for project",
						slog.String("project", proj.Name),
						slog.String("repo", repoFullName),
						slog.Any("error", err))
					continue
				}
				ghPollers = append(ghPollers, poller)
				if !p.dashboardMode {
					fmt.Printf("🐙 GitHub polling enabled: %s (project: %s, every %s, mode: %s)\n", repoFullName, proj.Name, interval, modeStr)
				}
			}

			// Start all pollers
			for _, poller := range ghPollers {
				go poller.Start(p.ctx)
			}
			p.ghPollers = ghPollers

			if len(ghPollers) == 0 {
				logging.WithComponent("github").Warn("GitHub polling enabled but no repos configured — set adapters.github.repo or add project-level github.owner/github.repo",
					slog.Int("pollers", 0))
				// GH-3050: surface second silent autopilot gate. Controllers
				// were created but will not Start because there are no pollers.
				if len(autopilotControllers) > 0 {
					logging.WithComponent("autopilot").Warn(
						"autopilot controllers created but no GitHub pollers configured — autopilot will not start",
						slog.Int("controllers", len(autopilotControllers)),
					)
				}
			}

			if len(ghPollers) > 0 {
				p.startAutopilotLoops(client, execMode, waitForMerge, pollInterval, prTimeout)
			}

			p.startStaleLabelCleanup(client)
		}
	}
}

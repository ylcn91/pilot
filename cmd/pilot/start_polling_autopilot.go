package main

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/approval"
	"github.com/ylcn91/pilot/internal/autopilot"
	"github.com/ylcn91/pilot/internal/logging"
)

func (p *pollingRuntime) setupAutopilotControllers() {
	cfg := p.cfg
	approvalMgr := p.approvalMgr
	projectPath := p.projectPath

	// GH-929: Create autopilot controllers map (one per repo) if enabled
	autopilotControllers := make(map[string]*autopilot.Controller)
	var autopilotController *autopilot.Controller // Default controller for backwards compat
	if cfg.Orchestrator.Autopilot != nil && cfg.Orchestrator.Autopilot.Enabled {
		// Need GitHub client for autopilot
		ghToken := ""
		if cfg.Adapters.GitHub != nil {
			ghToken = cfg.Adapters.GitHub.Token
			if ghToken == "" {
				ghToken = os.Getenv("GITHUB_TOKEN")
			}
		}
		if ghToken == "" {
			// GH-3050: surface silent autopilot disable when token is missing.
			// Without this warning, --env=<...> appears accepted but autopilot
			// never starts because controller creation is skipped here.
			logging.WithComponent("autopilot").Warn(
				"autopilot enabled but no GitHub token resolved — autopilot will not start (set adapters.github.token or GITHUB_TOKEN)",
				slog.String("env", string(cfg.Orchestrator.Autopilot.Environment)),
			)
		}
		if ghToken != "" {
			ghClient := github.NewClient(ghToken)

			// GH-1870: Build board sync option for autopilot controllers.
			var autopilotBoardOpts []autopilot.ControllerOption
			if cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.ProjectBoard != nil && cfg.Adapters.GitHub.ProjectBoard.Enabled {
				owner := ""
				if parts := strings.SplitN(cfg.Adapters.GitHub.Repo, "/", 2); len(parts) == 2 {
					owner = parts[0]
				}
				bs := github.NewProjectBoardSync(ghClient, cfg.Adapters.GitHub.ProjectBoard, owner)
				statuses := cfg.Adapters.GitHub.ProjectBoard.GetStatuses()
				autopilotBoardOpts = append(autopilotBoardOpts, autopilot.WithProjectBoardSync(bs, statuses.Done, statuses.Failed, statuses.Review))
			}

			// Create controller for default repo (adapters.github.repo)
			if cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Repo != "" {
				parts := strings.SplitN(cfg.Adapters.GitHub.Repo, "/", 2)
				if len(parts) == 2 {
					// Register GitHub approval handler if enabled
					if cfg.Adapters.GitHub.Approval != nil && cfg.Adapters.GitHub.Approval.Enabled {
						pollInterval := cfg.Adapters.GitHub.Approval.PollInterval
						if pollInterval == 0 {
							pollInterval = 30 * time.Second
						}
						ghApprovalHandler := approval.NewGitHubHandler(ghClient, &approval.GitHubHandlerConfig{
							Owner: parts[0], Repo: parts[1], PollInterval: pollInterval,
						})
						approvalMgr.RegisterHandler(ghApprovalHandler)
						logging.WithComponent("start").Info("registered GitHub approval handler",
							slog.String("repo", cfg.Adapters.GitHub.Repo))
					}

					// TASK-352: scope self-heal to the project's fs path. Fresh slice so
					// the per-project loop below does not alias this controller's option.
					ctrlOpts := append(append([]autopilot.ControllerOption{}, autopilotBoardOpts...), autopilot.WithProjectPath(projectPath))
					controller := autopilot.NewController(
						cfg.Orchestrator.Autopilot,
						ghClient,
						approvalMgr,
						parts[0],
						parts[1],
						ctrlOpts...,
					)
					maybeAttachGuardrails(controller, cfg, ghClient, parts[0], parts[1], projectPath)
					autopilotControllers[cfg.Adapters.GitHub.Repo] = controller
					autopilotController = controller // Default for backwards compat
				}
			}

			// GH-929: Create controllers for each project with GitHub config
			for _, proj := range cfg.Projects {
				if proj.GitHub == nil || proj.GitHub.Owner == "" || proj.GitHub.Repo == "" {
					continue
				}
				repoFullName := fmt.Sprintf("%s/%s", proj.GitHub.Owner, proj.GitHub.Repo)
				if _, exists := autopilotControllers[repoFullName]; exists {
					continue // Skip duplicates
				}
				// TASK-352: scope self-heal to this project's fs path (matches
				// executions.project_path). Fresh slice to avoid aliasing the shared opts.
				ctrlOpts := append(append([]autopilot.ControllerOption{}, autopilotBoardOpts...), autopilot.WithProjectPath(proj.Path))
				controller := autopilot.NewController(
					cfg.Orchestrator.Autopilot,
					ghClient,
					approvalMgr,
					proj.GitHub.Owner,
					proj.GitHub.Repo,
					ctrlOpts...,
				)
				maybeAttachGuardrails(controller, cfg, ghClient, proj.GitHub.Owner, proj.GitHub.Repo, proj.Path)
				autopilotControllers[repoFullName] = controller
				logging.WithComponent("autopilot").Info("created controller for project",
					slog.String("project", proj.Name),
					slog.String("repo", repoFullName),
				)
			}
		}
	}

	// GH-2685: wire all controllers as the approval state writer so async approval
	// decisions update the correct in-memory PRState across multi-repo deployments.
	if len(autopilotControllers) > 0 {
		var allControllers []*autopilot.Controller
		for _, c := range autopilotControllers {
			allControllers = append(allControllers, c)
		}
		approvalMgr.WithStateWriter(autopilot.NewMultiControllerStateWriter(allControllers...))
	}

	p.autopilotControllers = autopilotControllers
	p.autopilotController = autopilotController
}

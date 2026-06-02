package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/autopilot"
	"github.com/ylcn91/pilot/internal/banner"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/logging"
	"github.com/ylcn91/pilot/internal/pilot"
	"github.com/ylcn91/pilot/internal/quality"
	"github.com/ylcn91/pilot/internal/tunnel"
)

func newStartCmd() *cobra.Command {
	f := &startFlags{}

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start Pilot with config-driven inputs",
		Long: `Start Pilot with inputs enabled based on config or flags.

By default, reads enabled adapters from ~/.pilot/config.yaml.
Use flags to override config values.

Examples:
  pilot start                          # Config-driven
  pilot start --telegram               # Enable Telegram polling
  pilot start --github                 # Enable GitHub polling
  pilot start --slack                  # Enable Slack Socket Mode
  pilot start --telegram --github      # Enable both
  pilot start --dashboard              # With TUI dashboard
  pilot start --no-gateway             # Polling only (no HTTP server)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load config
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// GH-2361: Fail loudly when an adapter flag is used but the
			// corresponding adapter block is missing from config. Previously,
			// `pilot start --github` would silently auto-enable a defaulted
			// adapter with no token/repo and poll nothing.
			if err := validateAdapterFlags(cfg, cmd); err != nil {
				return err
			}

			// Apply flag overrides to config
			applyInputOverrides(cfg, cmd, f.enableTelegram, f.enableGithub, f.enableLinear, f.enableSlack, f.enableTunnel, f.enablePlane, f.enableDiscord)

			// Apply team ID override if flag provided
			if f.teamID != "" {
				cfg.TeamID = f.teamID
			}

			// Apply team flag overrides (GH-635)
			applyTeamOverrides(cfg, cmd, f.teamID, f.teamMember)

			// Initialize logging with config (GH-847)
			// Apply log-format flag override if set
			if cmd.Flags().Changed("log-format") {
				if cfg.Logging == nil {
					cfg.Logging = logging.DefaultConfig()
				}
				cfg.Logging.Format = f.logFormat
			}
			if cfg.Logging != nil {
				if err := logging.Init(cfg.Logging); err != nil {
					return fmt.Errorf("failed to initialize logging: %w", err)
				}
			}

			// GH-879: Log config reload on hot upgrade
			// After syscall.Exec, the new binary starts fresh and re-reads config from disk
			if os.Getenv("PILOT_RESTARTED") == "1" {
				logging.WithComponent("config").Info("config reloaded from disk after hot upgrade",
					"path", configPath)
			}

			// GH-710: Validate Slack Socket Mode config — degrade gracefully if app_token missing
			if cfg.Adapters.Slack != nil && cfg.Adapters.Slack.SocketMode && cfg.Adapters.Slack.AppToken == "" {
				logging.WithComponent("slack").Warn("socket_mode enabled but app_token not configured, skipping Slack Socket Mode")
				cfg.Adapters.Slack.SocketMode = false
			}

			// Stamp build version into executor config for feature matrix updates (GH-1388)
			if cfg.Executor == nil {
				cfg.Executor = executor.DefaultBackendConfig()
			}
			cfg.Executor.Version = version

			// Resolve project path: flag > config default > cwd
			projectPath := f.projectPath
			if projectPath == "" {
				if defaultProj := cfg.GetDefaultProject(); defaultProj != nil {
					projectPath = defaultProj.Path
				}
			}
			if projectPath == "" {
				cwd, _ := os.Getwd()
				projectPath = cwd
			}
			if strings.HasPrefix(projectPath, "~") {
				home, _ := os.UserHomeDir()
				projectPath = strings.Replace(projectPath, "~", home, 1)
			}

			// Clean stale pilot hooks on startup (GH-1883)
			cleanStartupHooks(cfg, projectPath)

			// Determine mode based on what's enabled
			hasTelegram := cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.Enabled
			hasGithubPolling := cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Enabled &&
				cfg.Adapters.GitHub.Polling != nil && cfg.Adapters.GitHub.Polling.Enabled
			hasSlack := cfg.Adapters.Slack != nil && cfg.Adapters.Slack.Enabled && cfg.Adapters.Slack.SocketMode

			// Apply execution mode override from CLI flags
			if f.sequential {
				if cfg.Orchestrator.Execution == nil {
					cfg.Orchestrator.Execution = config.DefaultExecutionConfig()
				}
				cfg.Orchestrator.Execution.Mode = "sequential"
			}

			// Override autopilot config if flag provided
			if f.envFlag != "" {
				if cfg.Orchestrator.Autopilot == nil {
					cfg.Orchestrator.Autopilot = autopilot.DefaultConfig()
				}
				cfg.Orchestrator.Autopilot.Enabled = true

				// Use SetActiveEnvironment to validate and resolve environment
				if err := cfg.Orchestrator.Autopilot.SetActiveEnvironment(f.envFlag); err != nil {
					// Show helpful error with available environments
					availableEnvs := []string{"dev", "stage", "prod"}
					if cfg.Orchestrator.Autopilot.Environments != nil {
						for name := range cfg.Orchestrator.Autopilot.Environments {
							availableEnvs = append(availableEnvs, name)
						}
					}
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					fmt.Fprintf(os.Stderr, "Available environments: %v\n", availableEnvs)
					fmt.Fprintf(os.Stderr, "\nTo add a custom environment, add to autopilot.environments in config.yaml:\n")
					fmt.Fprintf(os.Stderr, "autopilot:\n  environments:\n    my-env:\n      branch: main\n      require_approval: true\n")
					return err
				}
			}

			// GH-394: Polling mode is the default when any polling adapter is enabled.
			// Previously, having linear.enabled=true would force gateway mode even when
			// only using GitHub/Telegram polling. Now polling adapters work independently.
			//
			// Mode selection:
			// - noGateway flag: always use polling mode (user override)
			// - Polling adapters enabled: use polling mode (Telegram, GitHub)
			// - Only webhook adapters (Linear, Jira): use gateway mode
			//
			// Note: Linear/Jira webhooks require gateway but don't block polling adapters.
			// When both are needed, gateway starts in background within polling mode.
			// Splash screen removed — caused alt-screen flicker between
			// splash exit and dashboard start (GH-2459 follow-up).

			hasPollingAdapter := hasTelegram || hasGithubPolling
			if f.noGateway || hasPollingAdapter {
				return runPollingMode(cmd, cfg, projectPath, f.replace, f.dashboardMode, f.noGateway)
			}

			// Full daemon mode with gateway
			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("invalid config: %w", err)
			}

			// Suppress logging in dashboard mode BEFORE initialization (GH-351)
			if f.dashboardMode {
				logging.Suppress()
			}

			// Build Pilot options for gateway mode (GH-349)
			var pilotOpts []pilot.Option

			// Serve embedded React dashboard at /dashboard/ if available (GH-1612)
			if dashboardEmbedded {
				pilotOpts = append(pilotOpts, pilot.WithDashboardFS(dashboardFS))
			}

			// GH-392: Create shared infrastructure for polling adapters in gateway mode
			// This allows GitHub polling to work alongside Linear/Jira webhooks
			telegramFlagSet := cmd.Flags().Changed("telegram")
			githubFlagSet := cmd.Flags().Changed("github")
			slackFlagSet := cmd.Flags().Changed("slack")
			// GH-2232: Check if any adapter-registry poller is enabled (GitLab, Linear, Jira, etc.)
			adapterPollerEnabled := false
			for _, reg := range adapterPollerRegistrations() {
				if reg.Enabled(cfg) {
					adapterPollerEnabled = true
					break
				}
			}
			needsPollingInfra := (telegramFlagSet && hasTelegram && cfg.Adapters.Telegram.Polling) ||
				(githubFlagSet && hasGithubPolling && cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Enabled &&
					cfg.Adapters.GitHub.Polling != nil && cfg.Adapters.GitHub.Polling.Enabled) ||
				(slackFlagSet && hasSlack) ||
				adapterPollerEnabled

			// Shared infrastructure for polling adapters
			gw := &gatewayInfra{}

			if needsPollingInfra {
				var infraErr error
				var gwTeamCleanup func()
				gw, gwTeamCleanup, infraErr = buildGatewayInfra(cfg, cmd, projectPath, f.dashboardMode)
				if infraErr != nil {
					return fmt.Errorf("failed to create executor runner: %w", infraErr)
				}
				// Set up team project access checker cleanup if configured (GH-635).
				// Deferred here — in the RunE closure — so it fires at the outer
				// function's return, exactly as the original inline defer did.
				if gwTeamCleanup != nil {
					defer gwTeamCleanup()
				}
			}

			// Enable Telegram polling in gateway mode only if --telegram flag was explicitly passed (GH-351)
			if telegramFlagSet && hasTelegram && cfg.Adapters.Telegram.Polling {
				pilotOpts = append(pilotOpts, pilot.WithTelegramHandler(gw.Runner, projectPath))
				// GH-634: Wire team member resolver for Telegram RBAC in gateway mode
				if teamAdapter != nil {
					pilotOpts = append(pilotOpts, pilot.WithTelegramMemberResolver(teamAdapter))
				}
				// GH-2651: Wire approval handler so approve:/reject: button taps are dispatched
				if gw.TgApprovalHandler != nil {
					pilotOpts = append(pilotOpts, pilot.WithTelegramApprovalHandler(gw.TgApprovalHandler))
				}
				logging.WithComponent("start").Info("Telegram polling enabled in gateway mode")
			}

			// Enable Slack Socket Mode in gateway mode only if --slack flag was explicitly passed (GH-652)
			if slackFlagSet && hasSlack {
				pilotOpts = append(pilotOpts, pilot.WithSlackHandler(gw.Runner, projectPath))
				// GH-786: Wire team member resolver for Slack RBAC in gateway mode
				if teamAdapter != nil {
					pilotOpts = append(pilotOpts, pilot.WithSlackMemberResolver(teamAdapter))
				}
				logging.WithComponent("start").Info("Slack Socket Mode enabled in gateway mode")
			}

			// GH-539: Create budget enforcer for gateway mode
			// GH-1019: Debug logging for budget state visibility
			wireGatewayBudget(gw, cfg)

			// Enable GitHub polling in gateway mode only if --github flag was explicitly passed (GH-350, GH-351)
			// GH-392: Now actually processes issues instead of no-op
			if githubFlagSet && hasGithubPolling && cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Enabled &&
				cfg.Adapters.GitHub.Polling != nil && cfg.Adapters.GitHub.Polling.Enabled {
				if err := wireGatewayGitHubPolling(gw, cfg, projectPath, &pilotOpts); err != nil {
					return err
				}
			}

			// GH-1847: Start adapter pollers via registry pattern (gateway mode)
			gwPollerDeps := &PollerDeps{
				Cfg:                 cfg,
				ProjectPath:         projectPath,
				Dispatcher:          gw.Dispatcher,
				Runner:              gw.Runner,
				Monitor:             gw.Monitor,
				Program:             gw.Program,
				AlertsEngine:        gw.AlertsEngine,
				Enforcer:            gw.Enforcer,
				AutopilotController: gw.AutopilotController,
				AutopilotStateStore: gw.AutopilotStateStore,
			}
			StartAdapterPollers(context.Background(), gwPollerDeps, adapterPollerRegistrations())

			// Wire teams service if --team flag provided (GH-633)
			teamsDB, err := wireGatewayTeams(cfg, &pilotOpts)
			if err != nil {
				return err
			}

			// Create and start Pilot
			p, err := pilot.New(cfg, pilotOpts...)
			if err != nil {
				return fmt.Errorf("failed to create Pilot: %w", err)
			}

			// Set up quality gates if configured (GH-207) - for orchestrator/webhook mode
			if cfg.Quality != nil && cfg.Quality.Enabled {
				p.SetQualityCheckerFactory(func(taskID, taskProjectPath string) executor.QualityChecker {
					return &qualityCheckerWrapper{
						executor: quality.NewExecutor(&quality.ExecutorConfig{
							Config:      cfg.Quality,
							ProjectPath: taskProjectPath,
							TaskID:      taskID,
						}),
					}
				})
				logging.WithComponent("start").Info("quality gates enabled for webhook mode")
			}

			// GH-1585/GH-1609/GH-1633/GH-1935: Wire gateway providers, stores,
			// git graph, and the learning system onto the Pilot instance.
			wireGatewayPilot(p, gw, cfg, projectPath)

			if err := p.Start(); err != nil {
				return fmt.Errorf("failed to start Pilot: %w", err)
			}

			// Start tunnel if enabled
			if cfg.Tunnel != nil && cfg.Tunnel.Enabled {
				if cfg.Tunnel.Port == 0 {
					cfg.Tunnel.Port = cfg.Gateway.Port
				}
				tunnelMgr, tunnelErr := tunnel.NewManager(cfg.Tunnel, logging.WithComponent("tunnel"))
				if tunnelErr != nil {
					logging.WithComponent("start").Warn("failed to create tunnel", slog.Any("error", tunnelErr))
				} else if setupErr := tunnelMgr.Setup(context.Background()); setupErr != nil {
					logging.WithComponent("start").Warn("tunnel setup failed", slog.Any("error", setupErr))
				} else if publicURL, startErr := tunnelMgr.Start(context.Background()); startErr != nil {
					logging.WithComponent("start").Warn("failed to start tunnel", slog.Any("error", startErr))
				} else {
					fmt.Printf("🌐 Public tunnel: %s\n", publicURL)
					fmt.Printf("   Webhooks: %s/webhooks/{linear,github,gitlab,jira}\n", publicURL)
					defer tunnelMgr.Stop() //nolint:errcheck
				}
			}

			// Check for updates in background (non-blocking)
			go checkForUpdates()

			if f.dashboardMode {
				// GH-2291: Pass adapter poller infrastructure so the dashboard
				// merges task states from both adapter pollers and gateway webhooks.
				return runDashboardMode(p, cfg, gw.Program, gw.Monitor, gw.Runner)
			}

			// Show startup banner (headless mode)
			gatewayURL := fmt.Sprintf("http://%s:%d", cfg.Gateway.Host, cfg.Gateway.Port)
			banner.StartupBanner(version, gatewayURL)

			// Show per-adapter startup status lines (GH-349/350/393/652/2045)
			printAdapterStatus(cfg, hasTelegram, hasGithubPolling, hasSlack)

			// Wait for shutdown signal
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

			<-sigCh
			fmt.Println("\n🛑 Shutting down...")

			// Close teams DB if opened (GH-633)
			if teamsDB != nil {
				_ = teamsDB.Close()
			}

			return p.Stop()
		},
	}

	registerStartFlags(cmd, f)

	return cmd
}

package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/adapters/slack"
	"github.com/ylcn91/pilot/internal/adapters/telegram"
	"github.com/ylcn91/pilot/internal/alerts"
	"github.com/ylcn91/pilot/internal/approval"
	"github.com/ylcn91/pilot/internal/autopilot"
	"github.com/ylcn91/pilot/internal/banner"
	"github.com/ylcn91/pilot/internal/budget"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/dashboard"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/logging"
	"github.com/ylcn91/pilot/internal/memory"
	"github.com/ylcn91/pilot/internal/pilot"
	"github.com/ylcn91/pilot/internal/quality"
	"github.com/ylcn91/pilot/internal/teams"
	"github.com/ylcn91/pilot/internal/tunnel"
)

func newStartCmd() *cobra.Command {
	var (
		dashboardMode bool
		projectPath   string
		replace       bool
		// Input adapter flags (override config) - use bool with "changed" check
		enableTelegram bool
		enableGithub   bool
		enableLinear   bool
		enableSlack    bool
		enablePlane    bool
		enableDiscord  bool
		// Mode flags
		noGateway    bool   // Lightweight mode: polling only, no HTTP gateway
		sequential   bool   // Sequential execution mode (one issue at a time)
		envFlag      string // Environment name: dev, stage, prod, or custom configured name
		enableTunnel bool   // Enable public tunnel (Cloudflare/ngrok)
		teamID       string // Optional team ID for scoping execution
		teamMember   string // Member email for project access scoping
		logFormat    string // Log output format: text or json (GH-847)
	)

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
			applyInputOverrides(cfg, cmd, enableTelegram, enableGithub, enableLinear, enableSlack, enableTunnel, enablePlane, enableDiscord)

			// Apply team ID override if flag provided
			if teamID != "" {
				cfg.TeamID = teamID
			}

			// Apply team flag overrides (GH-635)
			applyTeamOverrides(cfg, cmd, teamID, teamMember)

			// Initialize logging with config (GH-847)
			// Apply log-format flag override if set
			if cmd.Flags().Changed("log-format") {
				if cfg.Logging == nil {
					cfg.Logging = logging.DefaultConfig()
				}
				cfg.Logging.Format = logFormat
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
			if sequential {
				if cfg.Orchestrator.Execution == nil {
					cfg.Orchestrator.Execution = config.DefaultExecutionConfig()
				}
				cfg.Orchestrator.Execution.Mode = "sequential"
			}

			// Override autopilot config if flag provided
			if envFlag != "" {
				if cfg.Orchestrator.Autopilot == nil {
					cfg.Orchestrator.Autopilot = autopilot.DefaultConfig()
				}
				cfg.Orchestrator.Autopilot.Enabled = true

				// Use SetActiveEnvironment to validate and resolve environment
				if err := cfg.Orchestrator.Autopilot.SetActiveEnvironment(envFlag); err != nil {
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
			if noGateway || hasPollingAdapter {
				return runPollingMode(cmd, cfg, projectPath, replace, dashboardMode, noGateway)
			}

			// Full daemon mode with gateway
			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("invalid config: %w", err)
			}

			// Suppress logging in dashboard mode BEFORE initialization (GH-351)
			if dashboardMode {
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
			var gwRunner *executor.Runner
			var gwStore *memory.Store
			var gwDispatcher *executor.Dispatcher
			var gwMonitor *executor.Monitor
			var gwProgram *tea.Program
			var gwAutopilotController *autopilot.Controller
			var gwAutopilotStateStore *autopilot.StateStore
			var gwAlertsEngine *alerts.Engine
			var gwTgApprovalHandler *approval.TelegramHandler

			if needsPollingInfra {
				// Create shared runner with config (GH-956: enables worktree isolation)
				var runnerErr error
				gwRunner, runnerErr = executor.NewRunnerWithConfig(cfg.Executor)
				if runnerErr != nil {
					return fmt.Errorf("failed to create executor runner: %w", runnerErr)
				}
				// TASK-286 / GH-3027: refuse sub-issue creation on unmanaged repos.
				gwRunner.SetRepoAllowlist(newConfigRepoAllowlist(cfg))

				// Set up quality gates on runner if configured
				if cfg.Quality != nil && cfg.Quality.Enabled {
					gwRunner.SetQualityCheckerFactory(func(taskID, taskProjectPath string) executor.QualityChecker {
						return &qualityCheckerWrapper{
							executor: quality.NewExecutor(&quality.ExecutorConfig{
								Config:      cfg.Quality,
								ProjectPath: taskProjectPath,
								TaskID:      taskID,
							}),
						}
					})
				}

				// Set up team project access checker if configured (GH-635)
				if gwTeamCleanup := wireProjectAccessChecker(gwRunner, cfg); gwTeamCleanup != nil {
					defer gwTeamCleanup()
				}

				// GH-962: Clean up orphaned worktree directories from previous crashed executions
				if cfg.Executor != nil && cfg.Executor.UseWorktree {
					if err := executor.CleanupOrphanedWorktrees(context.Background(), projectPath); err != nil {
						// Log the cleanup but don't fail startup - this is best-effort cleanup
						logging.WithComponent("start").Info("worktree cleanup completed", slog.String("result", err.Error()))
					} else {
						logging.WithComponent("start").Debug("worktree cleanup scan completed, no orphans found")
					}
				}

				// Create memory store for dispatcher
				var storeErr error
				gwStore, storeErr = memory.NewStore(cfg.Memory.Path)
				if storeErr != nil {
					logging.WithComponent("start").Warn("Failed to open memory store for gateway polling", slog.Any("error", storeErr))
				}

				// Create dispatcher if store available
				if gwStore != nil {
					gwDispatcher = executor.NewDispatcher(gwStore, gwRunner, nil)
					if dispErr := gwDispatcher.Start(context.Background()); dispErr != nil {
						logging.WithComponent("start").Warn("Failed to start dispatcher for gateway polling", slog.Any("error", dispErr))
						gwDispatcher = nil
					}
				}

				// GH-634: Initialize teams service for RBAC enforcement in gateway mode
				if gwStore != nil {
					teamStore, teamErr := teams.NewStore(gwStore.DB())
					if teamErr != nil {
						logging.WithComponent("teams").Warn("Failed to initialize team store for gateway", slog.Any("error", teamErr))
					} else {
						teamSvc := teams.NewService(teamStore)
						teamAdapter = teams.NewServiceAdapter(teamSvc)
						gwRunner.SetTeamChecker(teamAdapter)
						logging.WithComponent("teams").Info("team RBAC enforcement enabled for gateway mode")
					}
				}

				// GH-1027: Initialize knowledge store for experiential memories (gateway mode)
				if gwStore != nil {
					knowledgeStore := memory.NewKnowledgeStore(gwStore.DB())
					if err := knowledgeStore.InitSchema(); err != nil {
						logging.WithComponent("knowledge").Warn("Failed to initialize knowledge store schema (gateway)", slog.Any("error", err))
					} else {
						gwRunner.SetKnowledgeStore(knowledgeStore)
						logging.WithComponent("knowledge").Debug("Knowledge store initialized for gateway mode")
					}
				}

				// GH-1599: Wire log store for execution milestone entries (gateway mode)
				if gwStore != nil {
					gwRunner.SetLogStore(gwStore)
				}

				// Create approval manager for autopilot
				approvalMgr := approval.NewManager(cfg.Approval)

				// Register Telegram approval handler if enabled
				if cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.Enabled && cfg.Adapters.Telegram.BotToken != "" &&
					(cfg.Adapters.Telegram.Approval == nil || cfg.Adapters.Telegram.Approval.Enabled) {
					tgClient := telegram.NewClient(cfg.Adapters.Telegram.BotToken)
					gwTgApprovalHandler = approval.NewTelegramHandler(&telegramApprovalAdapter{client: tgClient}, cfg.Adapters.Telegram.ChatID)
					if gwStore != nil {
						gwTgApprovalHandler.WithStore(gwStore)
						if rErr := gwTgApprovalHandler.Rehydrate(context.Background()); rErr != nil {
							logging.WithComponent("approval").Warn("telegram approval rehydrate failed", slog.Any("error", rErr))
						}
					}
					approvalMgr.RegisterHandler(gwTgApprovalHandler)
				}

				// Register Slack approval handler if enabled
				if cfg.Adapters.Slack != nil && cfg.Adapters.Slack.Enabled && cfg.Adapters.Slack.BotToken != "" {
					if cfg.Adapters.Slack.Approval != nil && cfg.Adapters.Slack.Approval.Enabled {
						slackClient := slack.NewClient(cfg.Adapters.Slack.BotToken)
						slackAdapter := slack.NewSlackClientAdapter(slackClient)
						slackChannel := cfg.Adapters.Slack.Approval.Channel
						if slackChannel == "" {
							slackChannel = cfg.Adapters.Slack.Channel
						}
						slackApprovalHandler := approval.NewSlackHandler(&slackApprovalClientAdapter{adapter: slackAdapter}, slackChannel)
						approvalMgr.RegisterHandler(slackApprovalHandler)
					}
				}

				// Create autopilot controller if enabled
				if cfg.Orchestrator.Autopilot != nil && cfg.Orchestrator.Autopilot.Enabled {
					ghToken := ""
					if cfg.Adapters.GitHub != nil {
						ghToken = cfg.Adapters.GitHub.Token
						if ghToken == "" {
							ghToken = os.Getenv("GITHUB_TOKEN")
						}
					}
					if ghToken != "" && cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Repo != "" {
						parts := strings.SplitN(cfg.Adapters.GitHub.Repo, "/", 2)
						if len(parts) == 2 {
							ghClient := github.NewClient(ghToken)

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
							}

							// GH-1870: Board sync option for gateway autopilot controller.
							var gwBoardOpts []autopilot.ControllerOption
							if cfg.Adapters.GitHub.ProjectBoard != nil && cfg.Adapters.GitHub.ProjectBoard.Enabled {
								bs := github.NewProjectBoardSync(ghClient, cfg.Adapters.GitHub.ProjectBoard, parts[0])
								statuses := cfg.Adapters.GitHub.ProjectBoard.GetStatuses()
								gwBoardOpts = append(gwBoardOpts, autopilot.WithProjectBoardSync(bs, statuses.Done, statuses.Failed, statuses.Review, statuses.InProgress))
							}
							// TASK-352: scope self-heal to the project's fs path (matches
							// executions.project_path) so merged work flips failed→completed.
							gwBoardOpts = append(gwBoardOpts, autopilot.WithProjectPath(projectPath))
							gwAutopilotController = autopilot.NewController(
								cfg.Orchestrator.Autopilot,
								ghClient,
								approvalMgr,
								parts[0],
								parts[1],
								gwBoardOpts...,
							)
							// GH-2685: wire the controller as the approval state writer so
							// async approval decisions update the in-memory PRState.
							approvalMgr.WithStateWriter(gwAutopilotController)
						}
					}
				}

				// GH-726: Initialize autopilot state store for gateway mode
				if gwStore != nil && gwAutopilotController != nil {
					gwAutopilotController.SetMemoryStore(gwStore)

					var gwStoreErr error
					gwAutopilotStateStore, gwStoreErr = autopilot.NewStateStore(gwStore.DB())
					if gwStoreErr != nil {
						logging.WithComponent("autopilot").Warn("Failed to initialize state store (gateway)", slog.Any("error", gwStoreErr))
					} else {
						gwAutopilotController.SetStateStore(gwAutopilotStateStore)
						restored, restoreErr := gwAutopilotController.RestoreState()
						if restoreErr != nil {
							logging.WithComponent("autopilot").Warn("Failed to restore state from SQLite (gateway)", slog.Any("error", restoreErr))
						} else if restored > 0 {
							logging.WithComponent("autopilot").Info("Restored autopilot PR states from SQLite (gateway)", slog.Int("count", restored))
						}
					}
				}

				// Create alerts engine if configured
				alertsCfg := getAlertsConfig(cfg)
				if alertsCfg != nil && alertsCfg.Enabled {
					alertsMetrics := alerts.NewAlertMetrics()
					alertsDispatcher := alerts.NewDispatcher(alertsCfg, alerts.WithDispatcherMetrics(alertsMetrics))

					// Register Slack channel if configured
					if cfg.Adapters.Slack != nil && cfg.Adapters.Slack.Enabled && cfg.Adapters.Slack.BotToken != "" {
						slackClient := slack.NewClient(cfg.Adapters.Slack.BotToken)
						for _, ch := range alertsCfg.Channels {
							if ch.Type == "slack" && ch.Slack != nil {
								slackChannel := alerts.NewSlackChannel(ch.Name, slackClient, ch.Slack.Channel)
								alertsDispatcher.RegisterChannel(slackChannel)
							}
						}
					}

					// Register Telegram channel if configured
					if cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.Enabled && cfg.Adapters.Telegram.BotToken != "" {
						telegramClient := telegram.NewClient(cfg.Adapters.Telegram.BotToken)
						for _, ch := range alertsCfg.Channels {
							if ch.Type == "telegram" && ch.Telegram != nil {
								telegramChannel := alerts.NewTelegramChannel(ch.Name, telegramClient, ch.Telegram.ChatID)
								alertsDispatcher.RegisterChannel(telegramChannel)
							}
						}
					}

					// Register webhook channels
					for _, ch := range alertsCfg.Channels {
						if ch.Type == "webhook" && ch.Enabled && ch.Webhook != nil {
							webhookChannel := alerts.NewWebhookChannel(ch.Name, &alerts.WebhookChannelConfig{
								URL:     ch.Webhook.URL,
								Method:  ch.Webhook.Method,
								Headers: ch.Webhook.Headers,
								Secret:  ch.Webhook.Secret,
							})
							alertsDispatcher.RegisterChannel(webhookChannel)
						}
					}

					// Register email channels
					for _, ch := range alertsCfg.Channels {
						if ch.Type == "email" && ch.Enabled && ch.Email != nil && ch.Email.SMTPHost != "" {
							sender := alerts.NewSMTPSender(ch.Email.SMTPHost, ch.Email.SMTPPort, ch.Email.From, ch.Email.Username, ch.Email.Password)
							emailChannel := alerts.NewEmailChannel(ch.Name, sender, ch.Email)
							alertsDispatcher.RegisterChannel(emailChannel)
						}
					}

					// Register PagerDuty channels
					for _, ch := range alertsCfg.Channels {
						if ch.Type == "pagerduty" && ch.Enabled && ch.PagerDuty != nil {
							pdChannel := alerts.NewPagerDutyChannel(ch.Name, ch.PagerDuty)
							alertsDispatcher.RegisterChannel(pdChannel)
						}
					}

					ctx := context.Background()
					gwAlertsEngine = alerts.NewEngine(alertsCfg, alerts.WithDispatcher(alertsDispatcher), alerts.WithAlertMetrics(alertsMetrics))
					if alertErr := gwAlertsEngine.Start(ctx); alertErr != nil {
						logging.WithComponent("start").Warn("failed to start alerts engine for gateway polling", slog.Any("error", alertErr))
						gwAlertsEngine = nil
					}
				}

				// Create monitor and TUI program for dashboard mode
				if dashboardMode {
					gwRunner.SuppressProgressLogs(true)
					gwMonitor = executor.NewMonitor()
					gwRunner.SetMonitor(gwMonitor)
					// GH-1336: Wire monitor to autopilot controller so dashboard shows "done" after merge
					if gwAutopilotController != nil {
						gwAutopilotController.SetMonitor(gwMonitor)
					}
					model := dashboard.NewModelWithOptions(version, gwStore, gwAutopilotController, nil)
					model.SetProjectPath(projectPath)
					applyDashboardBannerMeta(&model, cfg, cmd)
					model.EnableSplash(resolvedConfigPath())
					gwProgram = tea.NewProgram(model,
						tea.WithAltScreen(),
						tea.WithInput(os.Stdin),
						tea.WithOutput(os.Stdout),
					)
					// GH-2291: Progress/token callbacks are registered by runDashboardMode
					// which merges task states from both adapter pollers and gateway webhooks.
				}
			}

			// Enable Telegram polling in gateway mode only if --telegram flag was explicitly passed (GH-351)
			if telegramFlagSet && hasTelegram && cfg.Adapters.Telegram.Polling {
				pilotOpts = append(pilotOpts, pilot.WithTelegramHandler(gwRunner, projectPath))
				// GH-634: Wire team member resolver for Telegram RBAC in gateway mode
				if teamAdapter != nil {
					pilotOpts = append(pilotOpts, pilot.WithTelegramMemberResolver(teamAdapter))
				}
				// GH-2651: Wire approval handler so approve:/reject: button taps are dispatched
				if gwTgApprovalHandler != nil {
					pilotOpts = append(pilotOpts, pilot.WithTelegramApprovalHandler(gwTgApprovalHandler))
				}
				logging.WithComponent("start").Info("Telegram polling enabled in gateway mode")
			}

			// Enable Slack Socket Mode in gateway mode only if --slack flag was explicitly passed (GH-652)
			if slackFlagSet && hasSlack {
				pilotOpts = append(pilotOpts, pilot.WithSlackHandler(gwRunner, projectPath))
				// GH-786: Wire team member resolver for Slack RBAC in gateway mode
				if teamAdapter != nil {
					pilotOpts = append(pilotOpts, pilot.WithSlackMemberResolver(teamAdapter))
				}
				logging.WithComponent("start").Info("Slack Socket Mode enabled in gateway mode")
			}

			// GH-539: Create budget enforcer for gateway mode
			// GH-1019: Debug logging for budget state visibility
			var gwEnforcer *budget.Enforcer
			if cfg.Budget != nil && cfg.Budget.Enabled && gwStore != nil {
				gwEnforcer = budget.NewEnforcer(cfg.Budget, gwStore)
				if gwAlertsEngine != nil {
					gwEnforcer.OnAlert(func(alertType, message, severity string) {
						gwAlertsEngine.ProcessEvent(alerts.Event{
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
				maxTokens, maxDuration := gwEnforcer.GetPerTaskLimits()
				if gwRunner != nil && (maxTokens > 0 || maxDuration > 0) {
					var gwTaskLimiters sync.Map
					gwRunner.SetTokenLimitCheck(func(taskID string, deltaInput, deltaOutput int64) bool {
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
					slog.Bool("store_nil", gwStore == nil),
				)
			}

			// Enable GitHub polling in gateway mode only if --github flag was explicitly passed (GH-350, GH-351)
			// GH-392: Now actually processes issues instead of no-op
			if githubFlagSet && hasGithubPolling && cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Enabled &&
				cfg.Adapters.GitHub.Polling != nil && cfg.Adapters.GitHub.Polling.Enabled {

				token := cfg.Adapters.GitHub.Token
				if token == "" {
					token = os.Getenv("GITHUB_TOKEN")
				}

				if token != "" && cfg.Adapters.GitHub.Repo != "" {
					client := github.NewClient(token)
					label := cfg.Adapters.GitHub.Polling.Label
					if label == "" {
						label = cfg.Adapters.GitHub.PilotLabel
					}
					interval := cfg.Adapters.GitHub.Polling.Interval
					if interval == 0 {
						interval = 30 * time.Second
					}

					// Determine execution mode from config
					execMode := github.ExecutionModeSequential
					waitForMerge := true
					pollInterval := 30 * time.Second
					prTimeout := 1 * time.Hour

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

					var pollerOpts []github.PollerOption
					pollerOpts = append(pollerOpts, github.WithExecutionMode(execMode))

					// Wire autopilot OnPRCreated callback if controller initialized
					if gwAutopilotController != nil {
						pollerOpts = append(pollerOpts, github.WithOnPRCreated(gwAutopilotController.OnPRCreated))
						// Wire sub-issue PR callback so epic sub-PRs are tracked by autopilot (GH-594)
						gwRunner.SetOnSubIssuePRCreated(gwAutopilotController.OnPRCreated)
					}

					// Wire sub-issue merge-wait so epic sub-issues block until their PR merges (GH-2179)
					if waitForMerge {
						gwRepoParts := strings.SplitN(cfg.Adapters.GitHub.Repo, "/", 2)
						if len(gwRepoParts) == 2 {
							mergeWaiter := github.NewMergeWaiter(client, gwRepoParts[0], gwRepoParts[1], &github.MergeWaiterConfig{
								PollInterval: pollInterval,
								Timeout:      prTimeout,
							})
							gwRunner.SetSubIssueMergeWait(func(ctx context.Context, prNumber int) error {
								_, err := mergeWaiter.WaitForMerge(ctx, prNumber)
								return err
							})
						}
					}

					// GH-2211: Wire native sub-issue linker so epic children get proper parent→child links
					gwRunner.SetSubIssueLinker(client)

					// GH-726: Wire processed issue persistence for gateway poller
					if gwAutopilotStateStore != nil {
						pollerOpts = append(pollerOpts, github.WithProcessedStore(gwAutopilotStateStore))
					}

					// GH-2201: Wire task checker for retry grace period (gateway mode)
					if gwStore != nil {
						pollerOpts = append(pollerOpts, github.WithTaskChecker(storeTaskChecker{store: gwStore}))
					}

					// GH-2242: Wire execution checker to prevent re-dispatch of completed tasks (gateway mode)
					if gwStore != nil {
						pollerOpts = append(pollerOpts, github.WithExecutionChecker(gwStore, projectPath))
					}

					// Wire issue metrics recorder for rate-limit tracking.
					if gwAutopilotController != nil {
						pollerOpts = append(pollerOpts, github.WithIssueMetricsRecorder(gwAutopilotController.Metrics()))
					}

					// GH-2802: Wire pre-flight judge when enabled (GH-2817: uses CC subprocess, no API key)
					if cfg.Executor != nil && cfg.Executor.PreFlightJudge != nil && cfg.Executor.PreFlightJudge.Enabled {
						claudeCmd := ""
						if cfg.Executor.ClaudeCode != nil {
							claudeCmd = cfg.Executor.ClaudeCode.Command
						}
						if claudeCmd == "" {
							claudeCmd = "claude"
						}
						if _, err := exec.LookPath(claudeCmd); err != nil {
							slog.Warn("Pre-flight judge disabled: claude binary not found", slog.String("command", claudeCmd))
						} else {
							pfJudge := executor.NewIntentJudge(claudeCmd)
							pollerOpts = append(pollerOpts, github.WithPreFlightJudge(preFlightJudgeShim{judge: pfJudge}))
							if gwStore != nil {
								pollerOpts = append(pollerOpts, github.WithExecutionSaver(storeExecutionSaver{store: gwStore}))
							}
						}
					}

					// Create rate limit retry scheduler
					repoParts := strings.Split(cfg.Adapters.GitHub.Repo, "/")
					if len(repoParts) != 2 {
						return fmt.Errorf("invalid repo format: %s", cfg.Adapters.GitHub.Repo)
					}
					repoOwner, repoName := repoParts[0], repoParts[1]
					gwSourceRepo := cfg.Adapters.GitHub.Repo // GH-929: Capture for closure

					rateLimitScheduler := executor.NewScheduler(executor.DefaultSchedulerConfig(), nil)
					rateLimitScheduler.SetRetryCallback(func(retryCtx context.Context, pendingTask *executor.PendingTask) error {
						var issueNum int
						if _, err := fmt.Sscanf(pendingTask.Task.ID, "GH-%d", &issueNum); err != nil {
							return fmt.Errorf("invalid task ID format: %s", pendingTask.Task.ID)
						}

						issue, err := client.GetIssue(retryCtx, repoOwner, repoName, issueNum)
						if err != nil {
							return fmt.Errorf("failed to fetch issue for retry: %w", err)
						}

						logging.WithComponent("scheduler").Info("Retrying rate-limited issue",
							slog.Int("issue", issueNum),
							slog.Int("attempt", pendingTask.Attempts),
						)

						var result *github.IssueResult
						if execMode == github.ExecutionModeSequential {
							result, err = handleGitHubIssueWithResult(retryCtx, cfg, client, issue, projectPath, gwSourceRepo, gwDispatcher, gwRunner, gwMonitor, gwProgram, gwAlertsEngine, gwEnforcer)
						} else {
							result, err = handleGitHubIssueWithResult(retryCtx, cfg, client, issue, projectPath, gwSourceRepo, gwDispatcher, gwRunner, gwMonitor, gwProgram, gwAlertsEngine, gwEnforcer)
						}

						// GH-797: Call OnPRCreated for retried issues so autopilot tracks their PRs
						if result != nil && result.PRNumber > 0 && gwAutopilotController != nil {
							gwAutopilotController.OnPRCreated(result.PRNumber, result.PRURL, issue.Number, result.HeadSHA, result.BranchName, issue.NodeID)
						}

						return err
					})
					rateLimitScheduler.SetExpiredCallback(func(expiredCtx context.Context, pendingTask *executor.PendingTask) {
						logging.WithComponent("scheduler").Error("Task exceeded max retry attempts",
							slog.String("task_id", pendingTask.Task.ID),
							slog.Int("attempts", pendingTask.Attempts),
						)
					})
					ctx := context.Background()
					if schErr := rateLimitScheduler.Start(ctx); schErr != nil {
						logging.WithComponent("start").Warn("Failed to start rate limit scheduler", slog.Any("error", schErr))
					}

					// GH-3228: Wire board source when source_enabled=true.
					if cfg.Adapters.GitHub.ProjectBoard != nil && cfg.Adapters.GitHub.ProjectBoard.SourceEnabled {
						boardSrc := github.NewProjectBoardSource(client, cfg.Adapters.GitHub.ProjectBoard, repoOwner, repoName)
						pollerOpts = append(pollerOpts, github.WithProjectBoardSource(boardSrc))
						// TASK-319: complete the loop — move the card Todo→In Progress on
						// confirmed pickup. WithBoardSync is a no-op when InProgress is "".
						boardWB := github.NewProjectBoardSync(client, cfg.Adapters.GitHub.ProjectBoard, repoOwner)
						pollerOpts = append(pollerOpts, github.WithBoardSync(boardWB, cfg.Adapters.GitHub.ProjectBoard.GetStatuses().InProgress))
					}

					// GH-392: Configure with actual issue processing callbacks (same as polling mode)
					if execMode == github.ExecutionModeSequential {
						pollerOpts = append(pollerOpts,
							github.WithSequentialConfig(waitForMerge, pollInterval, prTimeout),
							github.WithScheduler(rateLimitScheduler),
							github.WithOnIssueWithResult(func(issueCtx context.Context, issue *github.Issue) (*github.IssueResult, error) {
								return handleGitHubIssueWithResult(issueCtx, cfg, client, issue, projectPath, gwSourceRepo, gwDispatcher, gwRunner, gwMonitor, gwProgram, gwAlertsEngine, gwEnforcer)
							}),
						)
					} else {
						pollerOpts = append(pollerOpts,
							github.WithScheduler(rateLimitScheduler),
							github.WithMaxConcurrent(cfg.Orchestrator.MaxConcurrent),
							github.WithOnIssueWithResult(func(issueCtx context.Context, issue *github.Issue) (*github.IssueResult, error) {
								return handleGitHubIssueWithResult(issueCtx, cfg, client, issue, projectPath, gwSourceRepo, gwDispatcher, gwRunner, gwMonitor, gwProgram, gwAlertsEngine, gwEnforcer)
							}),
						)
					}

					ghPoller, err := github.NewPoller(client, cfg.Adapters.GitHub.Repo, label, interval, pollerOpts...)
					if err != nil {
						logging.WithComponent("start").Warn("GitHub polling disabled in gateway mode", slog.Any("error", err))
					} else {
						pilotOpts = append(pilotOpts, pilot.WithGitHubPoller(ghPoller))
						logging.WithComponent("start").Info("GitHub polling enabled in gateway mode",
							slog.String("repo", cfg.Adapters.GitHub.Repo),
							slog.Duration("interval", interval),
							slog.String("mode", string(execMode)),
						)

						// Start autopilot processing loop if controller initialized
						if gwAutopilotController != nil {
							ctx := context.Background()
							// Scan for existing PRs created by Pilot
							if scanErr := gwAutopilotController.ScanExistingPRs(ctx); scanErr != nil {
								logging.WithComponent("autopilot").Warn("failed to scan existing PRs",
									slog.Any("error", scanErr),
								)
							}

							// Scan for recently merged PRs that may need release (GH-416)
							if scanErr := gwAutopilotController.ScanRecentlyMergedPRs(ctx); scanErr != nil {
								logging.WithComponent("autopilot").Warn("failed to scan merged PRs",
									slog.Any("error", scanErr),
								)
							}

							logging.WithComponent("start").Info("autopilot enabled in gateway mode",
								slog.String("environment", string(cfg.Orchestrator.Autopilot.Environment)),
							)
							go func() {
								if runErr := gwAutopilotController.Run(ctx); runErr != nil && runErr != context.Canceled {
									logging.WithComponent("autopilot").Error("autopilot controller stopped",
										slog.Any("error", runErr),
									)
								}
							}()
						}
					}
				}
			}

			// GH-1847: Start adapter pollers via registry pattern (gateway mode)
			gwPollerDeps := &PollerDeps{
				Cfg:                 cfg,
				ProjectPath:         projectPath,
				Dispatcher:          gwDispatcher,
				Runner:              gwRunner,
				Monitor:             gwMonitor,
				Program:             gwProgram,
				AlertsEngine:        gwAlertsEngine,
				Enforcer:            gwEnforcer,
				AutopilotController: gwAutopilotController,
				AutopilotStateStore: gwAutopilotStateStore,
			}
			StartAdapterPollers(context.Background(), gwPollerDeps, adapterPollerRegistrations())

			// Wire teams service if --team flag provided (GH-633)
			var teamsDB *sql.DB
			if cfg.TeamID != "" {
				dbPath := filepath.Join(cfg.Memory.Path, "pilot.db")
				teamsDB, err = sql.Open("sqlite", dbPath)
				if err != nil {
					return fmt.Errorf("failed to open teams database: %w", err)
				}
				teamsStore, storeErr := teams.NewStore(teamsDB)
				if storeErr != nil {
					_ = teamsDB.Close()
					return fmt.Errorf("failed to create teams store: %w", storeErr)
				}
				teamsSvc := teams.NewService(teamsStore)

				// Verify team exists
				team, teamErr := teamsSvc.GetTeam(cfg.TeamID)
				if teamErr != nil || team == nil {
					// Try by name
					team, teamErr = teamsSvc.GetTeamByName(cfg.TeamID)
					if teamErr != nil || team == nil {
						_ = teamsDB.Close()
						return fmt.Errorf("team %q not found — create it with: pilot team create <name> --owner <email>", cfg.TeamID)
					}
					// Resolve name to ID
					cfg.TeamID = team.ID
				}

				pilotOpts = append(pilotOpts, pilot.WithTeamsService(teamsSvc))
				logging.WithComponent("start").Info("teams service initialized",
					slog.String("team_id", team.ID),
					slog.String("team_name", team.Name))
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

			// GH-1585: Wire autopilot provider to gateway so /api/v1/autopilot returns live PR data
			if gwAutopilotController != nil {
				p.Gateway().SetAutopilotProvider(&autopilotProviderAdapter{controller: gwAutopilotController})
				p.Gateway().SetMetricsSource(gwAutopilotController.Metrics())
				// GH-2855: wire token/cost/execution counters into executor
				if gwRunner != nil {
					gwRunner.SetMetricsRecorder(gwAutopilotController.Metrics())
				}
			}
			// TASK-332: Wire alert metrics into the Prometheus exporter
			if gwAlertsEngine != nil {
				p.Gateway().SetAlertsMetricsSource(gwAlertsEngine)
			}
			if gwAutopilotController != nil {

				// GH-2080: Wire PR review events to autopilot controller
				p.SetOnPRReview(func(ctx context.Context, prNumber int, action, state, reviewer string, repo *github.Repository) error {
					if action == "submitted" {
						gwAutopilotController.OnReviewRequested(prNumber, action, state, reviewer)
					}
					return nil
				})
			}

			// GH-1609: Wire dashboard store to gateway so /api/v1/{metrics,queue,history,logs} return 200
			if gwStore != nil {
				p.Gateway().SetDashboardStore(gwStore)
				p.Gateway().SetLogStreamStore(gwStore)
			}

			// GH-1633: Wire git graph fetcher to gateway so /api/v1/gitgraph returns live git data
			p.Gateway().SetGitGraphFetcher(func(path string, limit int) interface{} {
				return dashboard.FetchGitGraph(path, limit)
			})
			p.Gateway().SetGitGraphPath(projectPath)

			// GH-1935: Wire learning system into gateway mode (mirrors polling-mode wiring)
			if gwStore != nil && (cfg.Memory.Learning == nil || cfg.Memory.Learning.Enabled) {
				gwPatternStore, gwPatternErr := memory.NewGlobalPatternStore(cfg.Memory.Path)
				if gwPatternErr != nil {
					logging.WithComponent("learning").Warn("Failed to create pattern store, learning disabled (gateway mode)", slog.Any("error", gwPatternErr))
				} else {
					gwExtractor := memory.NewPatternExtractor(gwPatternStore, gwStore)
					gwLearningLoop := memory.NewLearningLoop(gwStore, gwExtractor, nil)
					gwPatternContext := executor.NewPatternContext(gwStore)

					gwRunner.SetLearningLoop(gwLearningLoop)
					gwRunner.SetPatternContext(gwPatternContext)
					gwRunner.SetSelfReviewExtractor(gwExtractor)

					if gwAutopilotController != nil {
						gwAutopilotController.SetLearningLoop(gwLearningLoop)
					}

					// GH-1991: Wire outcome tracker for model escalation (gateway mode)
					gwOutcomeTracker := memory.NewModelOutcomeTracker(gwStore)
					gwRunner.SetOutcomeTracker(gwOutcomeTracker)
					if gwRunner.HasModelRouter() {
						gwRunner.ModelRouter().SetOutcomeTracker(gwOutcomeTracker)
					}

					// GH-2016: Wire knowledge graph into gateway runner
					gwKG, gwKGErr := memory.NewKnowledgeGraph(cfg.Memory.Path)
					if gwKGErr != nil {
						logging.WithComponent("learning").Warn("Failed to create knowledge graph (gateway mode)", slog.Any("error", gwKGErr))
					} else {
						gwRunner.SetKnowledgeGraph(gwKG)
						logging.WithComponent("learning").Info("Knowledge graph initialized (gateway mode)")
					}

					logging.WithComponent("learning").Info("Learning system initialized (gateway mode)")
				}
			}

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

			if dashboardMode {
				// GH-2291: Pass adapter poller infrastructure so the dashboard
				// merges task states from both adapter pollers and gateway webhooks.
				return runDashboardMode(p, cfg, gwProgram, gwMonitor, gwRunner)
			}

			// Show startup banner (headless mode)
			gatewayURL := fmt.Sprintf("http://%s:%d", cfg.Gateway.Host, cfg.Gateway.Port)
			banner.StartupBanner(version, gatewayURL)

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

	cmd.Flags().BoolVar(&dashboardMode, "dashboard", false, "Show TUI dashboard for real-time task monitoring")
	cmd.Flags().StringVarP(&projectPath, "project", "p", "", "Project path (default: config default or cwd)")
	cmd.Flags().BoolVar(&replace, "replace", false, "Kill existing bot instance before starting")
	cmd.Flags().BoolVar(&noGateway, "no-gateway", false, "Run polling adapters only (no HTTP gateway)")
	cmd.Flags().BoolVar(&sequential, "sequential", false, "Sequential execution: wait for PR merge before next issue")
	cmd.Flags().StringVar(&envFlag, "env", "",
		"Environment name: dev, stage, prod, or custom configured environment")
	// Keep --autopilot as hidden deprecated alias
	cmd.Flags().StringVar(&envFlag, "autopilot", "",
		"DEPRECATED: Use --env instead")
	_ = cmd.Flags().MarkHidden("autopilot")

	// Input adapter flags - standard bool flags
	cmd.Flags().BoolVar(&enableTelegram, "telegram", false, "Enable Telegram polling (overrides config)")
	cmd.Flags().BoolVar(&enableGithub, "github", false, "Enable GitHub polling (overrides config)")
	cmd.Flags().BoolVar(&enableLinear, "linear", false, "Enable Linear webhooks (overrides config)")
	cmd.Flags().BoolVar(&enableSlack, "slack", false, "Enable Slack Socket Mode (overrides config)")
	cmd.Flags().BoolVar(&enablePlane, "plane", false, "Enable Plane.so polling (overrides config)")
	cmd.Flags().BoolVar(&enableDiscord, "discord", false, "Enable Discord bot (overrides config)")
	cmd.Flags().BoolVar(&enableTunnel, "tunnel", false, "Enable public tunnel for webhook ingress (Cloudflare/ngrok)")
	cmd.Flags().StringVar(&teamID, "team", "", "Team ID or name for project access scoping (overrides config)")
	cmd.Flags().StringVar(&teamMember, "team-member", "", "Member email for team access scoping (overrides config)")
	cmd.Flags().StringVar(&logFormat, "log-format", "text", "Log output format: text or json (for log aggregation systems)")

	return cmd
}

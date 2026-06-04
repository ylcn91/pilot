package main

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/adapters/slack"
	"github.com/ylcn91/pilot/internal/adapters/telegram"
	"github.com/ylcn91/pilot/internal/alerts"
	"github.com/ylcn91/pilot/internal/approval"
	"github.com/ylcn91/pilot/internal/architect"
	"github.com/ylcn91/pilot/internal/autopilot"
	"github.com/ylcn91/pilot/internal/budget"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/dashboard"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/logging"
	"github.com/ylcn91/pilot/internal/memory"
	"github.com/ylcn91/pilot/internal/quality"
	"github.com/ylcn91/pilot/internal/teams"
)

// gatewayInfra bundles the shared infrastructure created for polling adapters
// running inside gateway mode. The fields mirror the gw* locals previously
// declared inline in newStartCmd; nil fields indicate the corresponding
// component was not initialized (same conditions as before).
type gatewayInfra struct {
	Runner              *executor.Runner
	Store               *memory.Store
	Dispatcher          *executor.Dispatcher
	Monitor             *executor.Monitor
	Program             *tea.Program
	AutopilotController *autopilot.Controller
	AutopilotStateStore *autopilot.StateStore
	AlertsEngine        *alerts.Engine
	TgApprovalHandler   *approval.TelegramHandler
	Enforcer            *budget.Enforcer
	TeamAdapter         *teams.ServiceAdapter // GH-634: RBAC lookups, threaded instead of a package global
	ArchitectStore      *architect.FindingsStore
	ArchitectScheduler  *architect.Scheduler
}

// buildGatewayInfra constructs the shared runner, store, dispatcher, monitor,
// autopilot controller, alerts engine, and TUI program used by polling
// adapters in gateway mode. It returns the assembled infrastructure plus an
// optional cleanup function (the project-access-checker teardown). The cleanup
// MUST be deferred by the caller at the outer RunE return — it is returned
// rather than deferred here so it fires at the same point as the original
// inline `defer gwTeamCleanup()`.
//
// On a fatal error (runner creation) it returns a non-nil error and the caller
// reproduces the original early return.
func buildGatewayInfra(cfg *config.Config, cmd *cobra.Command, projectPath string, dashboardMode bool) (*gatewayInfra, func(), error) {
	gw := &gatewayInfra{}

	// Create shared runner with config (GH-956: enables worktree isolation)
	runner, runnerErr := executor.NewRunnerWithConfig(cfg.Executor)
	if runnerErr != nil {
		return nil, nil, runnerErr
	}
	gw.Runner = runner
	// TASK-286 / GH-3027: refuse sub-issue creation on unmanaged repos.
	gw.Runner.SetRepoAllowlist(newConfigRepoAllowlist(cfg))

	// Set up quality gates on runner if configured
	if cfg.Quality != nil && cfg.Quality.Enabled {
		gw.Runner.SetQualityCheckerFactory(func(taskID, taskProjectPath string) executor.QualityChecker {
			return &qualityCheckerWrapper{
				executor: quality.NewExecutor(&quality.ExecutorConfig{
					Config:      cfg.Quality,
					ProjectPath: taskProjectPath,
					TaskID:      taskID,
				}),
			}
		})
	}

	// Set up team project access checker if configured (GH-635). The returned
	// cleanup is handed back to the caller so it can be deferred at the outer
	// RunE return, matching the original lexical defer scope.
	cleanup := wireProjectAccessChecker(gw.Runner, cfg)

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
	gw.Store, storeErr = memory.NewStore(startMemoryPath(cfg))
	if storeErr != nil {
		logging.WithComponent("start").Warn("Failed to open memory store for gateway polling", slog.Any("error", storeErr))
	}

	// Create dispatcher if store available
	if gw.Store != nil {
		gw.Dispatcher = executor.NewDispatcher(gw.Store, gw.Runner, nil)
		if dispErr := gw.Dispatcher.Start(context.Background()); dispErr != nil {
			logging.WithComponent("start").Warn("Failed to start dispatcher for gateway polling", slog.Any("error", dispErr))
			gw.Dispatcher = nil
		}
	}

	// GH-634: Initialize teams service for RBAC enforcement in gateway mode
	if gw.Store != nil {
		teamStore, teamErr := teams.NewStore(gw.Store.DB())
		if teamErr != nil {
			logging.WithComponent("teams").Warn("Failed to initialize team store for gateway", slog.Any("error", teamErr))
		} else {
			teamSvc := teams.NewService(teamStore)
			gw.TeamAdapter = teams.NewServiceAdapter(teamSvc)
			gw.Runner.SetTeamChecker(gw.TeamAdapter)
			logging.WithComponent("teams").Info("team RBAC enforcement enabled for gateway mode")
		}
	}

	// GH-1027: Initialize knowledge store for experiential memories (gateway mode)
	if gw.Store != nil {
		knowledgeStore := memory.NewKnowledgeStore(gw.Store.DB())
		if err := knowledgeStore.InitSchema(); err != nil {
			logging.WithComponent("knowledge").Warn("Failed to initialize knowledge store schema (gateway)", slog.Any("error", err))
		} else {
			gw.Runner.SetKnowledgeStore(knowledgeStore)
			logging.WithComponent("knowledge").Debug("Knowledge store initialized for gateway mode")
		}
	}

	// GH-1599: Wire log store for execution milestone entries (gateway mode)
	if gw.Store != nil {
		gw.Runner.SetLogStore(gw.Store)
	}

	// Create approval manager and register the configured approval handlers.
	approvalMgr := buildGatewayApprovalManager(gw, cfg)

	// Create autopilot controller and state store if enabled.
	buildGatewayAutopilot(gw, cfg, approvalMgr, projectPath)

	// Create alerts engine if configured
	buildGatewayAlertsEngine(gw, cfg)

	// Create monitor and TUI program for dashboard mode
	if dashboardMode {
		gw.Runner.SuppressProgressLogs(true)
		gw.Monitor = executor.NewMonitor()
		gw.Runner.SetMonitor(gw.Monitor)
		// GH-1336: Wire monitor to autopilot controller so dashboard shows "done" after merge
		if gw.AutopilotController != nil {
			gw.AutopilotController.SetMonitor(gw.Monitor)
		}
		model := dashboard.NewModelWithOptions(version, gw.Store, gw.AutopilotController, nil)
		model.SetProjectPath(projectPath)
		applyDashboardBannerMeta(&model, cfg, cmd)
		model.EnableSplash(resolvedConfigPath())
		gw.Program = tea.NewProgram(model,
			tea.WithAltScreen(),
			tea.WithInput(os.Stdin),
			tea.WithOutput(os.Stdout),
		)
		// GH-2291: Progress/token callbacks are registered by runDashboardMode
		// which merges task states from both adapter pollers and gateway webhooks.
	}

	return gw, cleanup, nil
}

// buildGatewayApprovalManager creates the approval manager for gateway-mode
// autopilot and registers the Telegram and Slack approval handlers when their
// adapters are enabled. The Telegram handler is also stashed on gw so the
// caller can surface it; the GitHub approval handler is registered later inside
// buildGatewayAutopilot because it needs the GitHub client built there.
func buildGatewayApprovalManager(gw *gatewayInfra, cfg *config.Config) *approval.Manager {
	approvalMgr := approval.NewManager(cfg.Approval)

	// Register Telegram approval handler if enabled
	if cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.Enabled && cfg.Adapters.Telegram.BotToken != "" &&
		(cfg.Adapters.Telegram.Approval == nil || cfg.Adapters.Telegram.Approval.Enabled) {
		tgClient := telegram.NewClient(cfg.Adapters.Telegram.BotToken)
		gw.TgApprovalHandler = approval.NewTelegramHandler(&telegramApprovalAdapter{client: tgClient}, cfg.Adapters.Telegram.ChatID)
		if gw.Store != nil {
			gw.TgApprovalHandler.WithStore(gw.Store)
			if rErr := gw.TgApprovalHandler.Rehydrate(context.Background()); rErr != nil {
				logging.WithComponent("approval").Warn("telegram approval rehydrate failed", slog.Any("error", rErr))
			}
		}
		approvalMgr.RegisterHandler(gw.TgApprovalHandler)
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

	return approvalMgr
}

// buildGatewayAutopilot creates the autopilot controller (and its SQLite state
// store) for gateway mode when autopilot is enabled and a GitHub repo is
// configured. It registers the GitHub approval handler, wires board sync and
// guardrails, and sets approvalMgr's state writer to the controller. On any
// non-fatal failure it logs and leaves the corresponding gw field nil, matching
// the original inline behavior.
func buildGatewayAutopilot(gw *gatewayInfra, cfg *config.Config, approvalMgr *approval.Manager, projectPath string) {
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
				gw.AutopilotController = autopilot.NewController(
					cfg.Orchestrator.Autopilot,
					ghClient,
					approvalMgr,
					parts[0],
					parts[1],
					gwBoardOpts...,
				)
				// GH-30: feed non-2xx GitHub API errors into autopilot metrics so
				// api_errors_total / api_error_rate become non-zero and the
				// api_error_rate_high alert can fire in gateway mode.
				ghClient.WithAPIErrorRecorder(gw.AutopilotController.Metrics())
				maybeAttachGuardrails(gw.AutopilotController, cfg, ghClient, parts[0], parts[1], projectPath)
				// GH-2685: wire the controller as the approval state writer so
				// async approval decisions update the in-memory PRState.
				approvalMgr.WithStateWriter(gw.AutopilotController)
			}
		}
	}

	// GH-726: Initialize autopilot state store for gateway mode
	if gw.Store != nil && gw.AutopilotController != nil {
		gw.AutopilotController.SetMemoryStore(gw.Store)

		var gwStoreErr error
		gw.AutopilotStateStore, gwStoreErr = autopilot.NewStateStore(gw.Store.DB())
		if gwStoreErr != nil {
			logging.WithComponent("autopilot").Warn("Failed to initialize state store (gateway)", slog.Any("error", gwStoreErr))
		} else {
			gw.AutopilotController.SetStateStore(gw.AutopilotStateStore)
			restored, restoreErr := gw.AutopilotController.RestoreState()
			if restoreErr != nil {
				logging.WithComponent("autopilot").Warn("Failed to restore state from SQLite (gateway)", slog.Any("error", restoreErr))
			} else if restored > 0 {
				logging.WithComponent("autopilot").Info("Restored autopilot PR states from SQLite (gateway)", slog.Int("count", restored))
			}
		}
	}
}

// buildGatewayAlertsEngine creates and starts the alerts engine for gateway
// mode if alerts are configured and enabled, registering Slack, Telegram,
// webhook, email, and PagerDuty channels. On start failure it logs and leaves
// gw.AlertsEngine nil, matching the original inline behavior.
func buildGatewayAlertsEngine(gw *gatewayInfra, cfg *config.Config) {
	alertsCfg := getAlertsConfig(cfg)
	if alertsCfg == nil || !alertsCfg.Enabled {
		return
	}
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
	gw.AlertsEngine = alerts.NewEngine(alertsCfg, alerts.WithDispatcher(alertsDispatcher), alerts.WithAlertMetrics(alertsMetrics))
	if alertErr := gw.AlertsEngine.Start(ctx); alertErr != nil {
		logging.WithComponent("start").Warn("failed to start alerts engine for gateway polling", slog.Any("error", alertErr))
		gw.AlertsEngine = nil
	}
}

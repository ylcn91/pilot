package main

import (
	"context"
	"errors"
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
	"github.com/ylcn91/pilot/internal/briefs"
	"github.com/ylcn91/pilot/internal/budget"
	"github.com/ylcn91/pilot/internal/comms"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/dashboard"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/gateway"
	"github.com/ylcn91/pilot/internal/intent"
	"github.com/ylcn91/pilot/internal/logging"
	"github.com/ylcn91/pilot/internal/memory"
	"github.com/ylcn91/pilot/internal/quality"
	"github.com/ylcn91/pilot/internal/teams"
	"github.com/ylcn91/pilot/internal/upgrade"
)

// runPollingMode runs lightweight polling-only mode.
// When noGateway is false, the HTTP gateway starts in the background so the
// desktop app (and any other client hitting /health) can reach the daemon.
func runPollingMode(cmd *cobra.Command, cfg *config.Config, projectPath string, replace, dashboardMode, noGateway bool) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Check Telegram config if enabled
	hasTelegram := cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.Enabled
	if hasTelegram && cfg.Adapters.Telegram.BotToken == "" {
		return fmt.Errorf("telegram enabled but bot_token not configured")
	}

	// GH-710: Validate Slack Socket Mode config — degrade gracefully if app_token missing
	if cfg.Adapters.Slack != nil && cfg.Adapters.Slack.SocketMode && cfg.Adapters.Slack.AppToken == "" {
		logging.WithComponent("slack").Warn("socket_mode enabled but app_token not configured, skipping Slack Socket Mode")
		cfg.Adapters.Slack.SocketMode = false
	}

	// Suppress logging BEFORE creating runner in dashboard mode (GH-190)
	// Runner caches its logger at creation time, so suppression must happen first
	if dashboardMode {
		logging.Suppress()
	}

	// Create runner with config (GH-956: enables worktree isolation, decomposer, model routing)
	runner, err := executor.NewRunnerWithConfig(cfg.Executor)
	if err != nil {
		return fmt.Errorf("failed to create executor runner: %w", err)
	}
	// TASK-286 / GH-3027: refuse sub-issue creation on unmanaged repos.
	runner.SetRepoAllowlist(newConfigRepoAllowlist(cfg))

	// Set up quality gates if configured (GH-207)
	if cfg.Quality != nil && cfg.Quality.Enabled {
		runner.SetQualityCheckerFactory(func(taskID, taskProjectPath string) executor.QualityChecker {
			return &qualityCheckerWrapper{
				executor: quality.NewExecutor(&quality.ExecutorConfig{
					Config:      cfg.Quality,
					ProjectPath: taskProjectPath,
					TaskID:      taskID,
				}),
			}
		})
		logging.WithComponent("start").Info("quality gates enabled for polling mode")
	}

	// Set up team project access checker if configured (GH-635)
	if teamCleanup := wireProjectAccessChecker(runner, cfg); teamCleanup != nil {
		defer teamCleanup()
	}

	// GH-962: Clean up orphaned worktree directories from previous crashed executions
	if cfg.Executor != nil && cfg.Executor.UseWorktree {
		if err := executor.CleanupOrphanedWorktrees(ctx, projectPath); err != nil {
			// Log the cleanup but don't fail startup - this is best-effort cleanup
			logging.WithComponent("start").Info("worktree cleanup completed", slog.String("result", err.Error()))
		} else {
			logging.WithComponent("start").Debug("worktree cleanup scan completed, no orphans found")
		}
	}

	// Create approval manager
	approvalMgr := approval.NewManager(cfg.Approval)

	// Register Telegram approval handler if enabled
	var tgApprovalHandler telegram.ApprovalCallbackHandler
	var tgApprovalHandlerImpl *approval.TelegramHandler
	if cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.Enabled && cfg.Adapters.Telegram.BotToken != "" &&
		(cfg.Adapters.Telegram.Approval == nil || cfg.Adapters.Telegram.Approval.Enabled) {
		tgApprovalClient := telegram.NewClient(cfg.Adapters.Telegram.BotToken)
		tgApprovalHandlerImpl = approval.NewTelegramHandler(&telegramApprovalAdapter{client: tgApprovalClient}, cfg.Adapters.Telegram.ChatID)
		approvalMgr.RegisterHandler(tgApprovalHandlerImpl)
		tgApprovalHandler = tgApprovalHandlerImpl
		logging.WithComponent("start").Info("registered Telegram approval handler")
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
			logging.WithComponent("start").Info("registered Slack approval handler",
				slog.String("channel", slackChannel))
		}
	}

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
				autopilotBoardOpts = append(autopilotBoardOpts, autopilot.WithProjectBoardSync(bs, statuses.Done, statuses.Failed, statuses.Review, statuses.InProgress))
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

	// Initialize memory store early for dashboard persistence (GH-367)
	store, err := memory.NewStore(cfg.Memory.Path)
	if err != nil {
		logging.WithComponent("start").Warn("Failed to open memory store", slog.Any("error", err))
		store = nil
	} else {
		defer func() {
			if store != nil {
				_ = store.Close()
			}
		}()
	}

	// Attach persistence store and rehydrate pending approvals after restart.
	if store != nil && tgApprovalHandlerImpl != nil {
		tgApprovalHandlerImpl.WithStore(store)
		if rErr := tgApprovalHandlerImpl.Rehydrate(ctx); rErr != nil {
			logging.WithComponent("approval").Warn("telegram approval rehydrate failed", slog.Any("error", rErr))
		}
	}

	// GH-726: Initialize autopilot state store for crash recovery
	var autopilotStateStore *autopilot.StateStore
	if store != nil && len(autopilotControllers) > 0 {
		// GH-2712: Wire memory store for approval_request_id / approval_decision persistence.
		for _, controller := range autopilotControllers {
			controller.SetMemoryStore(store)
		}

		var storeErr error
		autopilotStateStore, storeErr = autopilot.NewStateStore(store.DB())
		if storeErr != nil {
			logging.WithComponent("autopilot").Warn("Failed to initialize state store", slog.Any("error", storeErr))
		} else {
			// GH-929: Wire state store to all controllers
			for repoName, controller := range autopilotControllers {
				controller.SetStateStore(autopilotStateStore)
				restored, restoreErr := controller.RestoreState()
				if restoreErr != nil {
					logging.WithComponent("autopilot").Warn("Failed to restore state from SQLite",
						slog.String("repo", repoName),
						slog.Any("error", restoreErr))
				} else if restored > 0 {
					logging.WithComponent("autopilot").Info("Restored autopilot PR states from SQLite",
						slog.String("repo", repoName),
						slog.Int("count", restored))
				}
			}
		}
	}

	// GH-634: Initialize teams service for RBAC enforcement
	if store != nil {
		teamStore, teamErr := teams.NewStore(store.DB())
		if teamErr != nil {
			logging.WithComponent("teams").Warn("Failed to initialize team store", slog.Any("error", teamErr))
		} else {
			teamSvc := teams.NewService(teamStore)
			teamAdapter = teams.NewServiceAdapter(teamSvc)
			runner.SetTeamChecker(teamAdapter)
			logging.WithComponent("teams").Info("team RBAC enforcement enabled for polling mode")
		}
	}

	// GH-1027: Initialize knowledge store for experiential memories.
	// Hoisted so the pattern-maintenance ticker below can call SyncToFiles.
	var knowledgeStore *memory.KnowledgeStore
	if store != nil {
		knowledgeStore = memory.NewKnowledgeStore(store.DB())
		if err := knowledgeStore.InitSchema(); err != nil {
			logging.WithComponent("knowledge").Warn("Failed to initialize knowledge store schema", slog.Any("error", err))
			knowledgeStore = nil
		} else {
			runner.SetKnowledgeStore(knowledgeStore)
			logging.WithComponent("knowledge").Debug("Knowledge store initialized for polling mode")
		}
	}

	// GH-1599: Wire log store for execution milestone entries
	if store != nil {
		runner.SetLogStore(store)
	}

	// GH-1814: Initialize learning system
	if store != nil && (cfg.Memory.Learning == nil || cfg.Memory.Learning.Enabled) {
		patternStore, patternErr := memory.NewGlobalPatternStore(cfg.Memory.Path)
		if patternErr != nil {
			logging.WithComponent("learning").Warn("Failed to create pattern store, learning disabled", slog.Any("error", patternErr))
		} else {
			extractor := memory.NewPatternExtractor(patternStore, store)
			learningLoop := memory.NewLearningLoop(store, extractor, nil)
			patternContext := executor.NewPatternContext(store)

			runner.SetLearningLoop(learningLoop)
			runner.SetPatternContext(patternContext)
			runner.SetSelfReviewExtractor(extractor)

			// GH-1823: Wire review learning into autopilot controllers
			for _, ctrl := range autopilotControllers {
				ctrl.SetLearningLoop(learningLoop)
			}

			logging.WithComponent("learning").Info("Learning system initialized")

			// GH-1991: Wire outcome tracker for model escalation
			outcomeTracker := memory.NewModelOutcomeTracker(store)
			runner.SetOutcomeTracker(outcomeTracker)
			if runner.HasModelRouter() {
				runner.ModelRouter().SetOutcomeTracker(outcomeTracker)
			}
			logging.WithComponent("learning").Info("Model outcome tracker initialized")

			// GH-2016: Wire knowledge graph into runner
			kg, kgErr := memory.NewKnowledgeGraph(cfg.Memory.Path)
			if kgErr != nil {
				logging.WithComponent("learning").Warn("Failed to create knowledge graph", slog.Any("error", kgErr))
			} else {
				runner.SetKnowledgeGraph(kg)
				logging.WithComponent("learning").Info("Knowledge graph initialized")
			}

			// Pattern maintenance — decay and cleanup every 24h
			go func() {
				ticker := time.NewTicker(24 * time.Hour)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						if n, decayErr := learningLoop.ApplyDecay(ctx); decayErr != nil {
							logging.WithComponent("learning").Warn("Pattern decay failed", slog.Any("error", decayErr))
						} else if n > 0 {
							logging.WithComponent("learning").Info("Applied pattern decay", slog.Int("patterns_decayed", n))
						}
						minConfidence := 0.1
						if cfg.Memory.Learning != nil && cfg.Memory.Learning.MinConfidence > 0 {
							minConfidence = cfg.Memory.Learning.MinConfidence
						}
						if n, depErr := learningLoop.DeprecateLowConfidencePatterns(ctx, minConfidence); depErr != nil {
							logging.WithComponent("learning").Warn("Pattern deprecation failed", slog.Any("error", depErr))
						} else if n > 0 {
							logging.WithComponent("learning").Info("Deprecated low-confidence patterns", slog.Int("deprecated", n))
						}

						// Opt-in (default off): SyncToFiles writes hash-named files under
						// .agent/knowledge/memories/{type}s/ — the same tree Navigator
						// manages with slug-named files, so enabling is explicit to avoid
						// surprising that layout.
						if knowledgeStore != nil && cfg.Memory != nil && cfg.Memory.SyncToFiles {
							agentPath := filepath.Join(projectPath, ".agent")
							if syncErr := knowledgeStore.SyncToFiles(agentPath); syncErr != nil {
								logging.WithComponent("knowledge").Warn("Knowledge sync to files failed", slog.Any("error", syncErr))
							} else {
								logging.WithComponent("knowledge").Info("Synced knowledge memories to files", slog.String("agent_path", agentPath))
							}
						}
					}
				}
			}()
		}
	}

	// GH-1662: Start gateway in background so desktop app can reach /health
	var gwServer *gateway.Server // hoisted so TASK-332 alert-metrics wiring can run after alerts engine is created
	if !noGateway && cfg.Gateway != nil {
		gwServer = gateway.NewServer(cfg.Gateway)
		if autopilotController != nil {
			gwServer.SetAutopilotProvider(&autopilotProviderAdapter{controller: autopilotController})
			gwServer.SetMetricsSource(autopilotController.Metrics())
			// GH-2855: wire token/cost/execution counters into executor
			runner.SetMetricsRecorder(autopilotController.Metrics())
		}

		// GH-2080: Wire PR review webhook events to autopilot controller in polling mode
		if autopilotController != nil && cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Enabled {
			capturedController := autopilotController
			token := cfg.Adapters.GitHub.Token
			if token == "" {
				token = os.Getenv("GITHUB_TOKEN")
			}
			if token != "" {
				ghClient := github.NewClient(token)
				ghWH := github.NewWebhookHandler(ghClient, cfg.Adapters.GitHub.WebhookSecret, cfg.Adapters.GitHub.PilotLabel)
				ghWH.OnPRReview(func(ctx context.Context, prNumber int, action, state, reviewer string, repo *github.Repository) error {
					if action == "submitted" {
						capturedController.OnReviewRequested(prNumber, action, state, reviewer)
					}
					return nil
				})
				gwServer.Router().RegisterWebhookHandler("github", func(payload map[string]interface{}) {
					eventType, _ := payload["_event_type"].(string)
					if err := ghWH.Handle(context.Background(), eventType, payload); err != nil {
						logging.WithComponent("pilot").Error("GitHub webhook error (polling mode)", slog.Any("error", err))
					}
				})
			}
		}
		if store != nil {
			gwServer.SetDashboardStore(store)
			gwServer.SetLogStreamStore(store)
		}
		gwServer.SetGitGraphFetcher(func(path string, limit int) interface{} {
			return dashboard.FetchGitGraph(path, limit)
		})
		gwServer.SetGitGraphPath(projectPath)
		go func() {
			addr := fmt.Sprintf("%s:%d", cfg.Gateway.Host, cfg.Gateway.Port)
			logging.WithComponent("gateway").Info("gateway started in background", "addr", addr)
			if err := gwServer.Start(ctx); err != nil && ctx.Err() == nil {
				logging.WithComponent("gateway").Error("gateway background error", "error", err)
			}
		}()
	}

	// Create monitor and TUI program for dashboard mode
	var monitor *executor.Monitor
	var program *tea.Program
	var upgradeRequestCh chan struct{} // Channel for hot upgrade requests (GH-369)
	if dashboardMode {
		runner.SuppressProgressLogs(true)

		monitor = executor.NewMonitor()
		runner.SetMonitor(monitor)
		// GH-1336: Wire monitor to autopilot controllers so dashboard shows "done" after merge
		for _, ctrl := range autopilotControllers {
			ctrl.SetMonitor(monitor)
		}
		upgradeRequestCh = make(chan struct{}, 1)
		model := dashboard.NewModelWithOptions(version, store, autopilotController, upgradeRequestCh)
		model.SetProjectPath(projectPath)
		applyDashboardBannerMeta(&model, cfg, cmd)
		model.EnableSplash(resolvedConfigPath())
		program = tea.NewProgram(model,
			tea.WithAltScreen(),
			tea.WithInput(os.Stdin),
			tea.WithOutput(os.Stdout),
		)

		// Wire runner progress updates to dashboard using named callback
		// This uses AddProgressCallback instead of OnProgress to prevent Telegram handler
		// from overwriting the dashboard callback (GH-149 fix)
		// GH-1220: Throttle progress callbacks to 200ms to prevent message flooding
		var lastDashboardUpdate time.Time
		var dashboardMu sync.Mutex
		runner.AddProgressCallback("dashboard", func(taskID, phase string, progress int, message string) {
			monitor.UpdateProgress(taskID, phase, progress, message)

			dashboardMu.Lock()
			if time.Since(lastDashboardUpdate) < 200*time.Millisecond {
				dashboardMu.Unlock()
				return // Skip — periodic ticker will catch it
			}
			lastDashboardUpdate = time.Now()
			dashboardMu.Unlock()

			tasks := convertTaskStatesToDisplay(monitor.GetAll())
			program.Send(dashboard.UpdateTasks(tasks)())

			logMsg := fmt.Sprintf("[%s] %s: %s (%d%%)", taskID, phase, message, progress)
			program.Send(dashboard.AddLog(logMsg)())
		})

		// Wire token usage updates to dashboard (GH-156 fix)
		runner.AddTokenCallback("dashboard", func(taskID string, inputTokens, outputTokens int64, modelName string) {
			program.Send(dashboard.UpdateTokens(int(inputTokens), int(outputTokens), modelName)())
		})
	}

	// Initialize Telegram handler if enabled
	var tgHandler *telegram.Handler
	if hasTelegram {
		var allowedIDs []int64
		// Include explicitly configured allowed IDs
		allowedIDs = append(allowedIDs, cfg.Adapters.Telegram.AllowedIDs...)
		// Also include ChatID so user can message their own bot
		if cfg.Adapters.Telegram.ChatID != "" {
			if id, err := parseInt64(cfg.Adapters.Telegram.ChatID); err == nil {
				allowedIDs = append(allowedIDs, id)
			}
		}

		tgClient := telegram.NewClient(cfg.Adapters.Telegram.BotToken)
		tgMessenger := telegram.NewMessenger(tgClient, cfg.Adapters.Telegram.PlainTextMode)

		// Build LLM classifier + conversation store for comms.Handler
		var tgLLMClassifier intent.Classifier
		var tgConvStore *intent.ConversationStore
		if cfg.Adapters.Telegram.LLMClassifier != nil && cfg.Adapters.Telegram.LLMClassifier.Enabled {
			apiKey := cfg.Adapters.Telegram.LLMClassifier.APIKey
			if apiKey == "" {
				apiKey = os.Getenv("ANTHROPIC_API_KEY")
			}
			if apiKey != "" {
				client := intent.NewAnthropicClient(apiKey)
				if cfg.Executor != nil {
					if cfg.Executor.DefaultModel != "" {
						client.SetModel(cfg.Executor.DefaultModel)
					}
					if cfg.Executor.APIBaseURL != "" {
						client.SetAPIURL(cfg.Executor.APIBaseURL + "/v1/messages")
					}
				}
				tgLLMClassifier = client
				historySize := 10
				if cfg.Adapters.Telegram.LLMClassifier.HistorySize > 0 {
					historySize = cfg.Adapters.Telegram.LLMClassifier.HistorySize
				}
				historyTTL := 30 * time.Minute
				if cfg.Adapters.Telegram.LLMClassifier.HistoryTTL > 0 {
					historyTTL = cfg.Adapters.Telegram.LLMClassifier.HistoryTTL
				}
				tgConvStore = intent.NewConversationStore(historySize, historyTTL)
			}
		}

		// Build comms.MemberResolver wrapper (GH-634)
		var tgMemberResolver comms.MemberResolver
		if teamAdapter != nil {
			tgMemberResolver = &telegram.MemberResolverAdapter{Inner: teamAdapter}
		}

		tgCommsHandler := comms.NewHandler(&comms.HandlerConfig{
			Messenger:      tgMessenger,
			Runner:         runner,
			Projects:       config.NewProjectSource(cfg),
			ProjectPath:    projectPath,
			RateLimit:      cfg.Adapters.Telegram.RateLimit,
			LLMClassifier:  tgLLMClassifier,
			ConvStore:      tgConvStore,
			MemberResolver: tgMemberResolver,
			Store:          store,
			TaskIDPrefix:   "TG",
		})

		tgConfig := &telegram.HandlerConfig{
			Client:          tgClient,
			CommsHandler:    tgCommsHandler,
			ProjectPath:     projectPath,
			Projects:        config.NewProjectSource(cfg),
			AllowedIDs:      allowedIDs,
			Transcription:   cfg.Adapters.Telegram.Transcription,
			Store:           store,
			ApprovalHandler: tgApprovalHandler,
		}
		tgHandler = telegram.NewHandler(tgConfig, runner)

		// Security warning if no allowed IDs configured
		if len(allowedIDs) == 0 {
			logging.WithComponent("telegram").Warn("SECURITY: allowed_ids is empty - ALL users can interact with the bot!")
		}

		// Check for existing instance
		if err := tgHandler.CheckSingleton(ctx); err != nil {
			if errors.Is(err, telegram.ErrConflict) {
				if replace {
					fmt.Println("🔄 Stopping existing bot instance...")
					if err := killExistingTelegramBot(); err != nil {
						return fmt.Errorf("failed to stop existing instance: %w", err)
					}
					fmt.Print("   Waiting for Telegram to release connection")
					maxRetries := 10
					var lastErr error
					for i := 0; i < maxRetries; i++ {
						delay := time.Duration(500+i*500) * time.Millisecond
						time.Sleep(delay)
						fmt.Print(".")
						if err := tgHandler.CheckSingleton(ctx); err == nil {
							fmt.Println(" ✓")
							fmt.Println("   ✓ Existing instance stopped")
							fmt.Println()
							lastErr = nil
							break
						} else {
							lastErr = err
						}
					}
					if lastErr != nil {
						fmt.Println(" ✗")
						return fmt.Errorf("timeout waiting for Telegram to release connection")
					}
				} else {
					fmt.Println()
					fmt.Println("❌ Another bot instance is already running")
					fmt.Println()
					fmt.Println("   Options:")
					fmt.Println("   • Kill it manually:  pkill -f 'pilot start'")
					fmt.Println("   • Auto-replace:      pilot start --replace")
					fmt.Println()
					return fmt.Errorf("conflict: another bot instance is running")
				}
			} else {
				return fmt.Errorf("singleton check failed: %w", err)
			}
		}
	}

	// Show startup banner (skip in dashboard mode to avoid corrupting TUI)
	if !dashboardMode {
		banner.StartupTelegram(version, projectPath, cfg.Adapters.Telegram.ChatID, cfg)
	}

	// Log autopilot status
	if cfg.Orchestrator.Autopilot != nil && cfg.Orchestrator.Autopilot.Enabled {
		logging.WithComponent("start").Info("autopilot enabled",
			slog.String("environment", string(cfg.Orchestrator.Autopilot.Environment)),
			slog.Bool("auto_merge", cfg.Orchestrator.Autopilot.AutoMerge),
			slog.Bool("auto_review", cfg.Orchestrator.Autopilot.AutoReview),
		)
	}

	// Initialize alerts engine for outbound notifications (GH-337)
	var alertsEngine *alerts.Engine
	alertsCfg := getAlertsConfig(cfg)
	if alertsCfg != nil && alertsCfg.Enabled {
		// Create dispatcher and register channels
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

		alertsEngine = alerts.NewEngine(alertsCfg, alerts.WithDispatcher(alertsDispatcher), alerts.WithAlertMetrics(alertsMetrics))
		if err := alertsEngine.Start(ctx); err != nil {
			logging.WithComponent("start").Warn("failed to start alerts engine", slog.Any("error", err))
			alertsEngine = nil
		} else {
			logging.WithComponent("start").Info("alerts engine started",
				slog.Int("channels", len(alertsDispatcher.ListChannels())),
			)
		}
	}

	// TASK-332: Wire alert metrics into the Prometheus exporter (polling mode)
	if gwServer != nil && alertsEngine != nil {
		gwServer.SetAlertsMetricsSource(alertsEngine)
	}

	// Initialize dispatcher for task queue (uses store created earlier)
	var dispatcher *executor.Dispatcher
	if store != nil {
		dispatcher = executor.NewDispatcher(store, runner, nil)
		if err := dispatcher.Start(ctx); err != nil {
			logging.WithComponent("start").Warn("Failed to start dispatcher", slog.Any("error", err))
			dispatcher = nil
		} else {
			logging.WithComponent("start").Info("Task dispatcher started")
		}
	}

	// GH-539: Create budget enforcer if configured
	var enforcer *budget.Enforcer
	if cfg.Budget != nil && cfg.Budget.Enabled && store != nil {
		enforcer = budget.NewEnforcer(cfg.Budget, store)
		// Wire alert callback to alerts engine
		if alertsEngine != nil {
			enforcer.OnAlert(func(alertType, message, severity string) {
				alertsEngine.ProcessEvent(alerts.Event{
					Type:      alerts.EventTypeBudgetWarning,
					Error:     message,
					Metadata:  map[string]string{"alert_type": alertType, "severity": severity},
					Timestamp: time.Now(),
				})
			})
		}
		logging.WithComponent("start").Info("budget enforcement enabled",
			slog.Float64("daily_limit", cfg.Budget.DailyLimit),
			slog.Float64("monthly_limit", cfg.Budget.MonthlyLimit),
		)

		// GH-539: Wire per-task token/duration limits into executor stream
		maxTokens, maxDuration := enforcer.GetPerTaskLimits()
		if maxTokens > 0 || maxDuration > 0 {
			var taskLimiters sync.Map // map[taskID]*budget.TaskLimiter
			runner.SetTokenLimitCheck(func(taskID string, deltaInput, deltaOutput int64) bool {
				// Get or create limiter for this task
				val, _ := taskLimiters.LoadOrStore(taskID, budget.NewTaskLimiter(maxTokens, maxDuration))
				limiter := val.(*budget.TaskLimiter)

				// Feed token deltas into the limiter
				totalDelta := deltaInput + deltaOutput
				if totalDelta > 0 {
					if !limiter.AddTokens(totalDelta) {
						return false
					}
				}

				// Also check duration on every event
				if !limiter.CheckDuration() {
					return false
				}

				return true
			})
			logging.WithComponent("start").Info("per-task budget limits enabled",
				slog.Int64("max_tokens", maxTokens),
				slog.Duration("max_duration", maxDuration),
			)
		}

		if !dashboardMode {
			fmt.Printf("💰 Budget enforcement enabled: $%.2f/day, $%.2f/month\n",
				cfg.Budget.DailyLimit, cfg.Budget.MonthlyLimit)
		}
	} else {
		// GH-1019: Log why budget is disabled for debugging
		logging.WithComponent("start").Debug("budget enforcement disabled",
			slog.Bool("config_nil", cfg.Budget == nil),
			slog.Bool("enabled", cfg.Budget != nil && cfg.Budget.Enabled),
			slog.Bool("store_nil", store == nil),
		)
	}

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
			label := cfg.Adapters.GitHub.Polling.Label
			if label == "" {
				label = cfg.Adapters.GitHub.PilotLabel
			}
			interval := cfg.Adapters.GitHub.Polling.Interval
			if interval == 0 {
				interval = 30 * time.Second
			}

			// Determine execution mode from config
			execMode := github.ExecutionModeSequential // Default to sequential
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

			modeStr := "sequential"
			if execMode == github.ExecutionModeParallel {
				modeStr = "parallel"
			}

			// Helper to create poller for a repo with its project path
			createPollerForRepo := func(repoFullName, projPath string) (*github.Poller, error) {
				repoParts := strings.Split(repoFullName, "/")
				if len(repoParts) != 2 {
					return nil, fmt.Errorf("invalid repo format: %s", repoFullName)
				}
				repoOwner, repoName := repoParts[0], repoParts[1]

				// GH-386: Validate repo/project match at startup
				if err := executor.ValidateRepoProjectMatch(repoFullName, projPath); err != nil {
					logging.WithComponent("github").Warn("repo/project mismatch detected",
						slog.String("repo", repoFullName),
						slog.String("project_path", projPath),
						slog.String("expected_project", executor.ExtractRepoName(repoFullName)),
					)
				}

				var pollerOpts []github.PollerOption

				// Wire autopilot callback to the correct controller for this repo
				controller := autopilotControllers[repoFullName]
				if controller != nil {
					pollerOpts = append(pollerOpts,
						github.WithOnPRCreated(controller.OnPRCreated),
					)
				}

				// GH-726: Wire processed issue persistence
				if autopilotStateStore != nil {
					pollerOpts = append(pollerOpts, github.WithProcessedStore(autopilotStateStore))
				}

				// GH-2201: Wire task checker for retry grace period
				pollerOpts = append(pollerOpts, github.WithTaskChecker(storeTaskChecker{store: store}))

				// GH-2242: Wire execution checker to prevent re-dispatch of completed tasks
				if store != nil {
					pollerOpts = append(pollerOpts, github.WithExecutionChecker(store, projPath))
				}

				// Wire issue metrics recorder for rate-limit tracking.
				if controller != nil {
					pollerOpts = append(pollerOpts, github.WithIssueMetricsRecorder(controller.Metrics()))
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
						if store != nil {
							pollerOpts = append(pollerOpts, github.WithExecutionSaver(storeExecutionSaver{store: store}))
						}
					}
				}

				// Capture variables for closures
				sourceRepo := repoFullName
				projPathCapture := projPath
				controllerCapture := controller

				// Create rate limit retry scheduler for this repo
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
						slog.String("repo", sourceRepo),
						slog.Int("issue", issueNum),
						slog.Int("attempt", pendingTask.Attempts),
					)

					result, err := handleGitHubIssueWithResult(retryCtx, cfg, client, issue, projPathCapture, sourceRepo, dispatcher, runner, monitor, program, alertsEngine, enforcer)

					if result != nil && result.PRNumber > 0 && controllerCapture != nil {
						controllerCapture.OnPRCreated(result.PRNumber, result.PRURL, issue.Number, result.HeadSHA, result.BranchName, issue.NodeID)
					}

					return err
				})
				rateLimitScheduler.SetExpiredCallback(func(expiredCtx context.Context, pendingTask *executor.PendingTask) {
					logging.WithComponent("scheduler").Error("Task exceeded max retry attempts",
						slog.String("task_id", pendingTask.Task.ID),
						slog.Int("attempts", pendingTask.Attempts),
					)
				})
				if err := rateLimitScheduler.Start(ctx); err != nil {
					logging.WithComponent("start").Warn("Failed to start rate limit scheduler",
						slog.String("repo", repoFullName),
						slog.Any("error", err))
				}

				// GH-3228: Wire board source for the adapter-level repo when source_enabled=true.
				if cfg.Adapters.GitHub.ProjectBoard != nil && cfg.Adapters.GitHub.ProjectBoard.SourceEnabled &&
					repoFullName == cfg.Adapters.GitHub.Repo {
					boardSrc := github.NewProjectBoardSource(client, cfg.Adapters.GitHub.ProjectBoard, repoOwner, repoName)
					pollerOpts = append(pollerOpts, github.WithProjectBoardSource(boardSrc))
					// TASK-319: complete the loop — move the card Todo→In Progress on
					// confirmed pickup. WithBoardSync is a no-op when InProgress is "".
					boardWB := github.NewProjectBoardSync(client, cfg.Adapters.GitHub.ProjectBoard, repoOwner)
					pollerOpts = append(pollerOpts, github.WithBoardSync(boardWB, cfg.Adapters.GitHub.ProjectBoard.GetStatuses().InProgress))
				}

				// Configure based on execution mode
				if execMode == github.ExecutionModeSequential {
					pollerOpts = append(pollerOpts,
						github.WithExecutionMode(github.ExecutionModeSequential),
						github.WithSequentialConfig(waitForMerge, pollInterval, prTimeout),
						github.WithScheduler(rateLimitScheduler),
						github.WithOnIssueWithResult(func(issueCtx context.Context, issue *github.Issue) (*github.IssueResult, error) {
							return handleGitHubIssueWithResult(issueCtx, cfg, client, issue, projPathCapture, sourceRepo, dispatcher, runner, monitor, program, alertsEngine, enforcer)
						}),
					)
				} else {
					pollerOpts = append(pollerOpts,
						github.WithExecutionMode(github.ExecutionModeParallel),
						github.WithScheduler(rateLimitScheduler),
						github.WithMaxConcurrent(cfg.Orchestrator.MaxConcurrent),
						github.WithOnIssueWithResult(func(issueCtx context.Context, issue *github.Issue) (*github.IssueResult, error) {
							return handleGitHubIssueWithResult(issueCtx, cfg, client, issue, projPathCapture, sourceRepo, dispatcher, runner, monitor, program, alertsEngine, enforcer)
						}),
					)
				}

				return github.NewPoller(client, repoFullName, label, interval, pollerOpts...)
			}

			// Create poller for default repo (adapters.github.repo)
			if cfg.Adapters.GitHub.Repo != "" {
				polledRepos[cfg.Adapters.GitHub.Repo] = true
				poller, err := createPollerForRepo(cfg.Adapters.GitHub.Repo, projectPath)
				if err != nil {
					if !dashboardMode {
						fmt.Printf("⚠️  GitHub polling disabled for %s: %v\n", cfg.Adapters.GitHub.Repo, err)
					}
				} else {
					ghPollers = append(ghPollers, poller)
					if !dashboardMode {
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

				poller, err := createPollerForRepo(repoFullName, projPath)
				if err != nil {
					logging.WithComponent("github").Warn("Failed to create poller for project",
						slog.String("project", proj.Name),
						slog.String("repo", repoFullName),
						slog.Any("error", err))
					continue
				}
				ghPollers = append(ghPollers, poller)
				if !dashboardMode {
					fmt.Printf("🐙 GitHub polling enabled: %s (project: %s, every %s, mode: %s)\n", repoFullName, proj.Name, interval, modeStr)
				}
			}

			// Start all pollers
			for _, poller := range ghPollers {
				go poller.Start(ctx)
			}

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
				if !dashboardMode && execMode == github.ExecutionModeSequential && waitForMerge {
					fmt.Printf("   ⏳ Sequential mode: waiting for PR merge before next issue (timeout: %s)\n", prTimeout)
				}

				// Start autopilot processing loops for all controllers
				for repoName, controller := range autopilotControllers {
					// Scan for existing PRs
					if err := controller.ScanExistingPRs(ctx); err != nil {
						logging.WithComponent("autopilot").Warn("failed to scan existing PRs",
							slog.String("repo", repoName),
							slog.Any("error", err),
						)
					}

					// Scan for recently merged PRs (GH-416)
					if err := controller.ScanRecentlyMergedPRs(ctx); err != nil {
						logging.WithComponent("autopilot").Warn("failed to scan merged PRs",
							slog.String("repo", repoName),
							slog.Any("error", err),
						)
					}

					// GH-2970: startup recovery sweep for stale parent issues
					controller.Start(ctx)

					// Start controller run loop
					go func(c *autopilot.Controller, repo string) {
						if err := c.Run(ctx); err != nil && err != context.Canceled {
							logging.WithComponent("autopilot").Error("autopilot controller stopped",
								slog.String("repo", repo),
								slog.Any("error", err),
							)
						}
					}(controller, repoName)
				}

				if len(autopilotControllers) > 0 && !dashboardMode {
					fmt.Printf("🤖 Autopilot enabled: %s environment (%d repos)\n", cfg.Orchestrator.Autopilot.Environment, len(autopilotControllers))
				}

				// Start metrics alerter for default controller (GH-728)
				if alertsEngine != nil && autopilotController != nil {
					metricsAlerter := autopilot.NewMetricsAlerter(autopilotController, alertsEngine)
					go metricsAlerter.Run(ctx)
				}

				// Start metrics persister for default controller (GH-728)
				if store != nil && autopilotController != nil {
					metricsPersister := autopilot.NewMetricsPersister(autopilotController, store)
					go metricsPersister.Run(ctx)
				}

				// Wire sub-issue PR callback for default controller (GH-594)
				if autopilotController != nil {
					runner.SetOnSubIssuePRCreated(autopilotController.OnPRCreated)
				}

				// GH-3240: mark epic-created sub-issues as processed in all pollers so
				// findOldestUnprocessedIssue does not re-dispatch them.
				runner.SetSubIssuePollerSkip(func(n int) {
					for _, p := range ghPollers {
						p.MarkProcessed(n)
					}
				})

				// GH-3271: when autopilot marks an issue done after PR-merge, immediately
				// re-mark it processed in all pollers so a poll tick during the
				// merge→pilot-done label propagation window cannot re-dispatch it.
				for _, ctrl := range autopilotControllers {
					ctrl.SetOnIssueDone(func(n int) {
						for _, p := range ghPollers {
							p.MarkProcessed(n)
						}
					})
				}

				// Wire sub-issue merge-wait so epic sub-issues block until their PR merges (GH-2179)
				if waitForMerge && cfg.Adapters.GitHub.Repo != "" {
					parts := strings.SplitN(cfg.Adapters.GitHub.Repo, "/", 2)
					if len(parts) == 2 {
						mergeWaiter := github.NewMergeWaiter(client, parts[0], parts[1], &github.MergeWaiterConfig{
							PollInterval: pollInterval,
							Timeout:      prTimeout,
						})
						runner.SetSubIssueMergeWait(func(ctx context.Context, prNumber int) error {
							_, err := mergeWaiter.WaitForMerge(ctx, prNumber)
							return err
						})
					}
				}
			}

			// Start stale label cleanup for default repo if enabled
			if cfg.Adapters.GitHub.Repo != "" && cfg.Adapters.GitHub.StaleLabelCleanup != nil && cfg.Adapters.GitHub.StaleLabelCleanup.Enabled {
				if store != nil {
					cleanerOpts := []github.CleanerOption{}
					// Wire callback to clear processed map when pilot-failed labels are removed
					if len(ghPollers) > 0 {
						cleanerOpts = append(cleanerOpts, github.WithOnFailedCleaned(func(issueNumber int) {
							for _, p := range ghPollers {
								p.ClearProcessed(issueNumber)
							}
						}))
						// GH-2402: Same wiring for pilot-blocked so removal allows re-dispatch.
						cleanerOpts = append(cleanerOpts, github.WithOnBlockedCleaned(func(issueNumber int) {
							for _, p := range ghPollers {
								p.ClearProcessed(issueNumber)
							}
						}))
						// GH-2589: On startup recovery, clear the persistent processed store
						// so the issue is re-dispatched on the next poll cycle.
						cleanerOpts = append(cleanerOpts, github.WithOnStartupRecovered(func(issueNumber int) {
							for _, p := range ghPollers {
								p.ClearProcessed(issueNumber)
							}
						}))
					}
					// GH-2354: when pilot-in-progress is stripped from a closed
					// issue, remove its task from the dashboard monitor so it
					// stops showing in the queue view.
					if monitor != nil {
						cleanerOpts = append(cleanerOpts, github.WithOnInProgressCleaned(func(issueNumber int) {
							monitor.Remove(fmt.Sprintf("GH-%d", issueNumber))
						}))
					}
					cleaner, cleanerErr := github.NewCleaner(client, store, cfg.Adapters.GitHub.Repo, cfg.Adapters.GitHub.StaleLabelCleanup, cleanerOpts...)
					if cleanerErr != nil {
						if !dashboardMode {
							fmt.Printf("⚠️  Stale label cleanup disabled: %v\n", cleanerErr)
						}
					} else {
						if !dashboardMode {
							fmt.Printf("🧹 Stale label cleanup enabled (every %s, in-progress: %s, failed: %s)\n",
								cfg.Adapters.GitHub.StaleLabelCleanup.Interval,
								cfg.Adapters.GitHub.StaleLabelCleanup.Threshold,
								cfg.Adapters.GitHub.StaleLabelCleanup.FailedThreshold)
						}
						// GH-2589: On daemon startup, strip pilot-in-progress labels
						// that have no live execution row. Daemon restart leaves these
						// stuck on issues whose executor was killed mid-flight.
						if n, err := cleaner.StartupRecover(ctx); err != nil {
							logging.WithComponent("github-cleanup").Warn("startup recovery failed",
								slog.Any("error", err))
						} else if !dashboardMode && n > 0 {
							fmt.Printf("🔄 Startup recovery: cleared %d stuck pilot-in-progress label(s)\n", n)
						}
						go cleaner.Start(ctx)
					}
				}
			}
		}
	}

	// GH-1847: Start adapter pollers via registry pattern (polling mode)
	pollingDeps := &PollerDeps{
		Cfg:                  cfg,
		ProjectPath:          projectPath,
		Dispatcher:           dispatcher,
		Runner:               runner,
		Monitor:              monitor,
		Program:              program,
		AlertsEngine:         alertsEngine,
		Enforcer:             enforcer,
		AutopilotController:  autopilotController,
		AutopilotStateStore:  autopilotStateStore,
		AutopilotControllers: autopilotControllers,
	}
	StartAdapterPollers(ctx, pollingDeps, adapterPollerRegistrations())

	// Start Telegram polling if enabled
	if tgHandler != nil {
		if !dashboardMode {
			fmt.Println("📱 Telegram polling started")
		}
		tgHandler.StartPolling(ctx)
	}

	// Start Slack Socket Mode if enabled (GH-652: wire into polling mode)
	var slackHandler *slack.Handler
	if cfg.Adapters.Slack != nil && cfg.Adapters.Slack.Enabled && cfg.Adapters.Slack.SocketMode &&
		cfg.Adapters.Slack.AppToken != "" && cfg.Adapters.Slack.BotToken != "" {
		slackClient := slack.NewClient(cfg.Adapters.Slack.BotToken)
		slackMessenger := slack.NewMessenger(slackClient)

		var slackMemberResolver comms.MemberResolver
		if teamAdapter != nil {
			slackMemberResolver = &slack.MemberResolverAdapter{Inner: teamAdapter}
		}

		slackCommsHandler := comms.NewHandler(&comms.HandlerConfig{
			Messenger:      slackMessenger,
			Runner:         runner,
			Projects:       config.NewSlackProjectSource(cfg),
			ProjectPath:    projectPath,
			MemberResolver: slackMemberResolver,
			Store:          store,
			TaskIDPrefix:   "SLACK",
		})

		slackHandler = slack.NewHandler(&slack.HandlerConfig{
			AppToken:        cfg.Adapters.Slack.AppToken,
			Client:          slackClient,
			CommsHandler:    slackCommsHandler,
			AllowedChannels: cfg.Adapters.Slack.AllowedChannels,
			AllowedUsers:    cfg.Adapters.Slack.AllowedUsers,
		})

		go func() {
			if err := slackHandler.StartListening(ctx); err != nil {
				logging.WithComponent("slack").Error("Slack Socket Mode error", slog.Any("error", err))
			}
		}()

		if !dashboardMode {
			fmt.Println("💬 Slack Socket Mode started")
		}
		logging.WithComponent("start").Info("Slack Socket Mode started in polling mode")
	}

	// Discord bot started via poller registry (poller_discord.go)

	// Start brief scheduler if enabled
	var briefScheduler *briefs.Scheduler
	if cfg.Orchestrator.DailyBrief != nil && cfg.Orchestrator.DailyBrief.Enabled {
		briefCfg := cfg.Orchestrator.DailyBrief

		// Convert config to briefs.BriefConfig
		briefsConfig := &briefs.BriefConfig{
			Enabled:  briefCfg.Enabled,
			Schedule: briefCfg.Schedule,
			Timezone: briefCfg.Timezone,
			Content: briefs.ContentConfig{
				IncludeMetrics:     briefCfg.Content.IncludeMetrics,
				IncludeErrors:      briefCfg.Content.IncludeErrors,
				MaxItemsPerSection: briefCfg.Content.MaxItemsPerSection,
			},
			Filters: briefs.FilterConfig{
				Projects: briefCfg.Filters.Projects,
			},
		}

		// Convert channels
		for _, ch := range briefCfg.Channels {
			briefsConfig.Channels = append(briefsConfig.Channels, briefs.ChannelConfig{
				Type:       ch.Type,
				Channel:    ch.Channel,
				Recipients: ch.Recipients,
			})
		}

		// Create generator (requires store)
		if store != nil {
			generator := briefs.NewGenerator(store, briefsConfig)

			// Create delivery service with available clients
			var deliveryOpts []briefs.DeliveryOption
			if cfg.Adapters.Slack != nil && cfg.Adapters.Slack.Enabled {
				slackClient := slack.NewClient(cfg.Adapters.Slack.BotToken)
				deliveryOpts = append(deliveryOpts, briefs.WithSlackClient(slackClient))
			}
			if cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.Enabled {
				tgClient := telegram.NewClient(cfg.Adapters.Telegram.BotToken)
				deliveryOpts = append(deliveryOpts, briefs.WithTelegramSender(&telegramBriefAdapter{client: tgClient}))
			}
			deliveryOpts = append(deliveryOpts, briefs.WithLogger(slog.Default()))

			delivery := briefs.NewDeliveryService(briefsConfig, deliveryOpts...)

			// Create and start scheduler
			briefScheduler = briefs.NewScheduler(generator, delivery, briefsConfig, slog.Default(), store)
			if err := briefScheduler.Start(ctx); err != nil {
				logging.WithComponent("start").Warn("Failed to start brief scheduler", slog.Any("error", err))
				briefScheduler = nil
			} else {
				logging.WithComponent("start").Info("brief scheduler started",
					slog.String("schedule", briefCfg.Schedule),
					slog.String("timezone", briefCfg.Timezone),
				)
			}
		} else {
			logging.WithComponent("start").Warn("Brief scheduler requires memory store, skipping")
		}
	}

	// Dashboard mode: run TUI and handle shutdown via TUI quit
	if dashboardMode && program != nil {
		fmt.Println("\n🖥️  Starting TUI dashboard...")

		// Start background version checker for hot reload (GH-369)
		versionChecker := upgrade.NewVersionChecker(version, upgrade.DefaultCheckInterval)
		versionChecker.OnUpdate(func(info *upgrade.VersionInfo) {
			program.Send(dashboard.NotifyUpdateAvailable(info.Current, info.Latest, info.ReleaseNotes)())
			program.Send(dashboard.AddLog(fmt.Sprintf("⬆️ Update available: %s → %s", info.Current, info.Latest))())
		})
		versionChecker.Start(ctx)
		defer versionChecker.Stop()

		// Set up hot upgrade goroutine - listens for upgrade requests from 'u' key press
		// The channel is created above and passed to the dashboard model
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-upgradeRequestCh:
					info := versionChecker.GetLatestInfo()
					if info == nil || !info.UpdateAvail || info.LatestRelease == nil {
						program.Send(dashboard.NotifyUpgradeComplete(false, "No update available")())
						continue
					}

					// Drain pollers — stop accepting new issues before upgrade
					program.Send(dashboard.AddLog("⏳ Draining pollers — no new issues will be accepted...")())
					for _, p := range ghPollers {
						go p.Drain()
					}

					// Perform hot upgrade with monitor as TaskChecker
					// Monitor tracks running/queued tasks; upgrade waits for them to finish
					hotUpgrader, err := upgrade.NewHotUpgrader(version, monitor)
					if err != nil {
						program.Send(dashboard.NotifyUpgradeComplete(false, err.Error())())
						program.Send(dashboard.AddLog(fmt.Sprintf("❌ Upgrade failed: %v", err))())
						continue
					}

					upgradeCfg := &upgrade.HotUpgradeConfig{
						WaitForTasks: true,
						TaskTimeout:  30 * time.Minute,
						OnProgress: func(pct int, msg string) {
							program.Send(dashboard.NotifyUpgradeProgress(pct, msg)())
						},
						FlushSession: func() error {
							// Future: flush session state to SQLite here
							return nil
						},
					}

					if err := hotUpgrader.PerformHotUpgrade(ctx, info.LatestRelease, upgradeCfg); err != nil {
						program.Send(dashboard.NotifyUpgradeComplete(false, err.Error())())
						program.Send(dashboard.AddLog(fmt.Sprintf("❌ Upgrade failed: %v", err))())
					} else {
						// On Unix, process is replaced and this line is never reached.
						// On Windows, hot restart is not supported — binary is installed
						// but process continues. Notify user to restart manually.
						program.Send(dashboard.NotifyUpgradeComplete(true, "")())
						program.Send(dashboard.AddLog("✅ Upgrade installed! Please restart Pilot to use the new version.")())
					}
				}
			}
		}()

		// Periodic refresh to catch any missed updates
		go func() {
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if monitor != nil {
						tasks := convertTaskStatesToDisplay(monitor.GetAll())
						program.Send(dashboard.UpdateTasks(tasks)())
					}
				}
			}
		}()

		// Add startup logs after TUI starts (Send blocks if Run hasn't been called)
		go func() {
			time.Sleep(100 * time.Millisecond) // Wait for Run() to start
			program.Send(dashboard.AddLog(fmt.Sprintf("🚀 Pilot %s started - Polling mode", version))())
			if hasTelegram {
				program.Send(dashboard.AddLog("📱 Telegram polling active")())
			}
			hasGitHubPolling := cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Enabled &&
				cfg.Adapters.GitHub.Polling != nil && cfg.Adapters.GitHub.Polling.Enabled
			if hasGitHubPolling {
				repoCount := countGitHubRepos(cfg)
				if repoCount == 0 {
					program.Send(dashboard.AddLog("🐙 GitHub polling: no repos configured")())
				} else {
					program.Send(dashboard.AddLog(fmt.Sprintf("🐙 GitHub polling: %d repo(s) configured", repoCount))())
				}
			}
			hasLinearPolling := cfg.Adapters.Linear != nil && cfg.Adapters.Linear.Enabled &&
				cfg.Adapters.Linear.Polling != nil && cfg.Adapters.Linear.Polling.Enabled
			if hasLinearPolling {
				workspaces := cfg.Adapters.Linear.GetWorkspaces()
				for _, ws := range workspaces {
					program.Send(dashboard.AddLog(fmt.Sprintf("📊 Linear polling: %s/%s", ws.Name, ws.TeamID))())
				}
			}

			// Show GitLab status (GH-2045)
			if cfg.Adapters.GitLab != nil && cfg.Adapters.GitLab.Enabled {
				if cfg.Adapters.GitLab.Polling != nil && cfg.Adapters.GitLab.Polling.Enabled {
					program.Send(dashboard.AddLog("🦊 GitLab polling active")())
				} else {
					program.Send(dashboard.AddLog("🦊 GitLab webhooks enabled")())
				}
			}
			// Show Jira status (GH-2045)
			if cfg.Adapters.Jira != nil && cfg.Adapters.Jira.Enabled {
				if cfg.Adapters.Jira.Polling != nil && cfg.Adapters.Jira.Polling.Enabled {
					program.Send(dashboard.AddLog("🎫 Jira polling active")())
				} else {
					program.Send(dashboard.AddLog("🎫 Jira webhooks enabled")())
				}
			}
			// Show Asana status (GH-2045)
			if cfg.Adapters.Asana != nil && cfg.Adapters.Asana.Enabled {
				if cfg.Adapters.Asana.Polling != nil && cfg.Adapters.Asana.Polling.Enabled {
					program.Send(dashboard.AddLog("📋 Asana polling active")())
				} else {
					program.Send(dashboard.AddLog("📋 Asana webhooks enabled")())
				}
			}
			// Show Azure DevOps status (GH-2045)
			if cfg.Adapters.AzureDevOps != nil && cfg.Adapters.AzureDevOps.Enabled {
				if cfg.Adapters.AzureDevOps.Polling != nil && cfg.Adapters.AzureDevOps.Polling.Enabled {
					program.Send(dashboard.AddLog("🔷 Azure DevOps polling active")())
				} else {
					program.Send(dashboard.AddLog("🔷 Azure DevOps webhooks enabled")())
				}
			}
			// Show Plane status (GH-2045)
			if cfg.Adapters.Plane != nil && cfg.Adapters.Plane.Enabled {
				if cfg.Adapters.Plane.Polling != nil && cfg.Adapters.Plane.Polling.Enabled {
					program.Send(dashboard.AddLog("✈️  Plane polling active")())
				} else {
					program.Send(dashboard.AddLog("✈️  Plane webhooks enabled")())
				}
			}
			// Show Discord status (GH-2045)
			if cfg.Adapters.Discord != nil && cfg.Adapters.Discord.Enabled {
				program.Send(dashboard.AddLog("🎮 Discord gateway enabled")())
			}

			// Check for restart marker (set by hot upgrade)
			// GH-879: Config is automatically reloaded because syscall.Exec starts a fresh process
			if os.Getenv("PILOT_RESTARTED") == "1" {
				prevVersion := os.Getenv("PILOT_PREVIOUS_VERSION")
				if prevVersion != "" {
					program.Send(dashboard.AddLog(fmt.Sprintf("✅ Upgraded from %s to %s (config reloaded)", prevVersion, version))())
				} else {
					program.Send(dashboard.AddLog("✅ Pilot restarted (config reloaded)")())
				}
			}
		}()

		// Run TUI (blocks until quit via 'q' or Ctrl+C)
		// Note: The upgrade callback is handled via upgradeRequestCh above
		if _, err := program.Run(); err != nil {
			cancel() // Stop goroutines
			return fmt.Errorf("dashboard error: %w", err)
		}

		// Clean shutdown - cancel context to stop all goroutines
		cancel()

		// Terminate all running subprocesses (GH-883)
		runner.CancelAll()

		if tgHandler != nil {
			tgHandler.Stop()
		}
		// ghPoller stops via context cancellation (no explicit stop needed)
		if dispatcher != nil {
			dispatcher.Stop()
		}
		if briefScheduler != nil {
			briefScheduler.Stop()
		}
		return nil
	}

	// Non-dashboard mode: wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	<-sigCh
	fmt.Println("\n🛑 Shutting down...")

	// Terminate all running subprocesses (GH-883)
	runner.CancelAll()

	if tgHandler != nil {
		tgHandler.Stop()
	}
	if len(ghPollers) > 0 {
		fmt.Printf("🐙 Stopping GitHub pollers (%d)...\n", len(ghPollers))
	}
	if dispatcher != nil {
		fmt.Println("📋 Stopping task dispatcher...")
		dispatcher.Stop()
	}
	if briefScheduler != nil {
		briefScheduler.Stop()
	}

	return nil
}

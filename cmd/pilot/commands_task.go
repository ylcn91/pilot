package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/adapters/slack"
	"github.com/ylcn91/pilot/internal/adapters/telegram"
	"github.com/ylcn91/pilot/internal/alerts"
	"github.com/ylcn91/pilot/internal/banner"
	"github.com/ylcn91/pilot/internal/budget"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/logging"
	"github.com/ylcn91/pilot/internal/memory"
	"github.com/ylcn91/pilot/internal/quality"
)

func newTaskCmd() *cobra.Command {
	var projectPath string
	var dryRun bool
	var verbose bool
	var enableAlerts bool
	var enableBudget bool
	var localMode bool    // GH-2103: problem-solving prompt without PR constraints
	var resultJSON string // Write ExecutionResult as JSON to file
	var teamID string     // GH-635: team project access scoping
	var teamMember string // GH-635: member email for access scoping

	cmd := &cobra.Command{
		Use:   "task [description]",
		Short: "Execute a task using Claude Code",
		Long: `Execute a task using Claude Code with Navigator integration.

PRs are always created to enable autopilot workflow.

Examples:
  pilot task "Add user authentication with JWT"
  pilot task "Fix the login bug in auth.go" --project /path/to/project
  pilot task "Refactor the API handlers" --dry-run
  pilot task "Add index.py with hello world" --verbose
  pilot task "Fix bug" --alerts
  pilot task "Fix bug" --local`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskDesc := args[0]

			// Create context with cancellation on SIGINT
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Handle Ctrl+C
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				fmt.Println("\n\n⚠️  Cancelling task...")
				cancel()
			}()

			banner.Print()

			// Resolve project path
			if projectPath == "" {
				cwd, _ := os.Getwd()
				projectPath = cwd
			}

			// Generate task ID based on timestamp
			taskID := fmt.Sprintf("TASK-%d", time.Now().Unix()%100000)
			branchName := fmt.Sprintf("pilot/%s", taskID)

			// Check for Navigator
			hasNavigator := false
			if _, err := os.Stat(projectPath + "/.agent"); err == nil {
				hasNavigator = true
			}

			fmt.Println("🚀 Pilot Task Execution")
			fmt.Println("───────────────────────────────────────")
			fmt.Printf("   Task ID:   %s\n", taskID)
			fmt.Printf("   Project:   %s\n", projectPath)
			if localMode {
				fmt.Printf("   Mode:      local (no git workflow)\n")
			} else {
				fmt.Printf("   Branch:    %s\n", branchName)
				fmt.Printf("   Create PR: ✓ always enabled\n")
			}
			if hasNavigator {
				fmt.Printf("   Navigator: ✓ enabled\n")
			}
			fmt.Println()
			fmt.Println("📋 Task:")
			fmt.Printf("   %s\n", taskDesc)
			fmt.Println("───────────────────────────────────────")
			fmt.Println()

			// Build the task early so we can show prompt in dry-run
			// Always create branches and PRs - required for autopilot workflow
			task := &executor.Task{
				ID:          taskID,
				Title:       taskDesc,
				Description: taskDesc,
				ProjectPath: projectPath,
				Branch:      branchName,
				Verbose:     verbose,
				CreatePR:    true,
				LocalMode:   localMode, // GH-2103
			}

			// Local mode: skip git workflow (no branch/push/PR)
			if localMode {
				task.CreatePR = false
				task.Branch = ""
				task.DirectCommit = false
				task.LocalMode = true
			}

			// Dry run mode - just show what would happen
			if dryRun {
				// GH-2286: load config to respect executor.type setting
				dryRunCfgPath := cfgFile
				if dryRunCfgPath == "" {
					dryRunCfgPath = config.DefaultConfigPath()
				}
				dryRunCfg, cfgErr := config.Load(dryRunCfgPath)
				if cfgErr != nil {
					return fmt.Errorf("failed to load config for dry-run: %w", cfgErr)
				}
				dryRunBackendCfg := dryRunCfg.Executor
				if dryRunBackendCfg == nil {
					dryRunBackendCfg = executor.DefaultBackendConfig()
				}

				fmt.Println("🧪 DRY RUN - showing what would execute:")
				fmt.Println()
				fmt.Printf("Command: %s -p \"<prompt>\" --verbose --output-format stream-json\n", dryRunBackendCfg.Type)
				fmt.Println("Working directory:", projectPath)
				fmt.Println()
				fmt.Println("Prompt:")
				fmt.Println("─────────────────────────────────────")
				// Build actual prompt using a runner with config
				runner, runnerErr := executor.NewRunnerWithConfig(dryRunBackendCfg)
				if runnerErr != nil {
					return fmt.Errorf("failed to create runner for dry-run: %w", runnerErr)
				}
				// TASK-286 / GH-3027: harmless on the dry-run path (no gh calls),
				// but kept for consistency so the wiring is uniform across sites.
				runner.SetRepoAllowlist(newConfigRepoAllowlist(dryRunCfg))
				prompt := runner.BuildPrompt(task, task.ProjectPath)
				fmt.Println(prompt)
				fmt.Println("─────────────────────────────────────")
				return nil
			}

			// Check budget before task execution if --budget flag is set
			if enableBudget {
				// Load config for budget
				configPath := cfgFile
				if configPath == "" {
					configPath = config.DefaultConfigPath()
				}

				budgetCfg, err := config.Load(configPath)
				if err != nil {
					return fmt.Errorf("failed to load config for budget: %w", err)
				}

				// Get budget config or use defaults
				budgetConfig := budgetCfg.Budget
				if budgetConfig == nil {
					budgetConfig = budget.DefaultConfig()
				}

				// Enable budget check even if not enabled in config (flag overrides)
				budgetConfig.Enabled = true

				// Open memory store for usage data
				store, err := memory.NewStore(budgetCfg.Memory.Path)
				if err != nil {
					return fmt.Errorf("failed to open memory store for budget: %w", err)
				}
				defer func() { _ = store.Close() }()

				// Create budget enforcer and check
				enforcer := budget.NewEnforcer(budgetConfig, store)
				result, err := enforcer.CheckBudget(ctx, "", "")
				if err != nil {
					return fmt.Errorf("budget check failed: %w", err)
				}

				if !result.Allowed {
					fmt.Println()
					fmt.Println("🚫 Task Blocked by Budget")
					fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
					fmt.Printf("   Reason: %s\n", result.Reason)
					fmt.Println()
					fmt.Println("   Run 'pilot budget status' for details")
					fmt.Println("   Run 'pilot budget reset' to reset daily counters")
					fmt.Println()
					return fmt.Errorf("task blocked by budget: %s", result.Reason)
				}

				// Show budget status
				fmt.Printf("   Budget:    ✓ $%.2f daily / $%.2f monthly remaining\n", result.DailyLeft, result.MonthlyLeft)
			}

			// Initialize alerts engine if --alerts flag is set
			var alertsEngine *alerts.Engine
			if enableAlerts {
				// Load config for alerts
				configPath := cfgFile
				if configPath == "" {
					configPath = config.DefaultConfigPath()
				}

				cfg, err := config.Load(configPath)
				if err != nil {
					return fmt.Errorf("failed to load config for alerts: %w", err)
				}

				// Get alerts config
				alertsCfg := getAlertsConfig(cfg)
				if alertsCfg == nil {
					// Use default config with alerts enabled
					alertsCfg = alerts.DefaultConfig()
					alertsCfg.Enabled = true
				} else {
					alertsCfg.Enabled = true
				}

				// Create dispatcher and register channels
				dispatcher := alerts.NewDispatcher(alertsCfg)

				// Register Slack channel if configured
				if cfg.Adapters.Slack != nil && cfg.Adapters.Slack.Enabled && cfg.Adapters.Slack.BotToken != "" {
					slackClient := slack.NewClient(cfg.Adapters.Slack.BotToken)
					for _, ch := range alertsCfg.Channels {
						if ch.Type == "slack" && ch.Slack != nil {
							slackChannel := alerts.NewSlackChannel(ch.Name, slackClient, ch.Slack.Channel)
							dispatcher.RegisterChannel(slackChannel)
						}
					}
				}

				// Register Telegram channel if configured
				if cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.Enabled && cfg.Adapters.Telegram.BotToken != "" {
					telegramClient := telegram.NewClient(cfg.Adapters.Telegram.BotToken)
					for _, ch := range alertsCfg.Channels {
						if ch.Type == "telegram" && ch.Telegram != nil {
							telegramChannel := alerts.NewTelegramChannel(ch.Name, telegramClient, ch.Telegram.ChatID)
							dispatcher.RegisterChannel(telegramChannel)
						}
					}
				}

				alertsEngine = alerts.NewEngine(alertsCfg, alerts.WithDispatcher(dispatcher))
				if err := alertsEngine.Start(ctx); err != nil {
					return fmt.Errorf("failed to start alerts engine: %w", err)
				}
				defer alertsEngine.Stop()

				fmt.Printf("   Alerts:    ✓ enabled (%d channels)\n", len(dispatcher.ListChannels()))

				// Send task started event
				alertsEngine.ProcessEvent(alerts.Event{
					Type:      alerts.EventTypeTaskStarted,
					TaskID:    taskID,
					TaskTitle: taskDesc,
					Project:   projectPath,
					Timestamp: time.Now(),
				})
			}

			// Load config for runner setup
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}
			cfg, cfgErr := config.Load(configPath)
			if cfgErr != nil {
				return fmt.Errorf("failed to load config: %w", cfgErr)
			}

			// Apply team flag overrides (GH-635)
			applyTeamOverrides(cfg, cmd, teamID, teamMember)

			// Create the executor runner with config (GH-956: enables worktree isolation, decomposer, model routing)
			runner, runnerErr := executor.NewRunnerWithConfig(cfg.Executor)
			if runnerErr != nil {
				return fmt.Errorf("failed to create executor runner: %w", runnerErr)
			}
			// TASK-286 / GH-3027: refuse sub-issue creation on unmanaged repos.
			runner.SetRepoAllowlist(newConfigRepoAllowlist(cfg))

			// GH-962: Clean up orphaned worktree directories from previous crashed executions
			if cfg.Executor != nil && cfg.Executor.UseWorktree {
				if err := executor.CleanupOrphanedWorktrees(ctx, projectPath); err != nil {
					// Log the cleanup but don't fail startup - this is best-effort cleanup
					fmt.Printf("   Worktree:  ✓ cleanup completed (%s)\n", err.Error())
				}
			}

			// Quality gates (GH-207)
			if cfg.Quality != nil && cfg.Quality.Enabled {
				runner.SetQualityCheckerFactory(func(taskID, projectPath string) executor.QualityChecker {
					return &qualityCheckerWrapper{
						executor: quality.NewExecutor(&quality.ExecutorConfig{
							Config:      cfg.Quality,
							ProjectPath: projectPath,
							TaskID:      taskID,
						}),
					}
				})
				fmt.Println("   Quality:   ✓ gates enabled")
			}

			// Decomposer status (GH-218) - wired via NewRunnerWithConfig
			if cfg.Executor != nil && cfg.Executor.Decompose != nil && cfg.Executor.Decompose.Enabled {
				fmt.Println("   Decompose: ✓ enabled")
			}

			// GH-539: Wire per-task budget limits if configured
			// GH-1019: Debug logging for budget state visibility
			if cfg.Budget != nil && cfg.Budget.Enabled {
				maxTokens := cfg.Budget.PerTask.MaxTokens
				maxDuration := cfg.Budget.PerTask.MaxDuration
				if maxTokens > 0 || maxDuration > 0 {
					limiter := budget.NewTaskLimiter(maxTokens, maxDuration)
					runner.SetTokenLimitCheck(func(_ string, deltaInput, deltaOutput int64) bool {
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
					fmt.Printf("   Per-task:  ✓ max %d tokens, %v duration\n", maxTokens, maxDuration)
				}
				logging.WithComponent("execute").Debug("budget enforcement enabled",
					slog.Int64("max_tokens", cfg.Budget.PerTask.MaxTokens),
					slog.Duration("max_duration", cfg.Budget.PerTask.MaxDuration),
				)
			} else {
				// GH-1019: Log why budget is disabled for debugging
				logging.WithComponent("execute").Debug("budget enforcement disabled",
					slog.Bool("config_nil", cfg.Budget == nil),
					slog.Bool("enabled", cfg.Budget != nil && cfg.Budget.Enabled),
				)
			}

			// Team project access checker (GH-635)
			if runTeamCleanup := wireProjectAccessChecker(runner, cfg); runTeamCleanup != nil {
				defer runTeamCleanup()
				fmt.Println("   Team:      ✓ project access scoping enabled")
			}

			// GH-2146: Initialize learning system for task command
			// Mirrors main.go polling/gateway mode learning init
			if cfg.Memory != nil && cfg.Memory.Path != "" {
				learningStore, lsErr := memory.NewStore(cfg.Memory.Path)
				if lsErr != nil {
					logging.WithComponent("learning").Warn("Failed to open memory store for learning, learning disabled", slog.Any("error", lsErr))
				} else {
					defer func() { _ = learningStore.Close() }()

					// Wire log store for execution milestone entries (GH-1599)
					runner.SetLogStore(learningStore)

					// Wire knowledge store for experiential memories (GH-1027)
					knowledgeStore := memory.NewKnowledgeStore(learningStore.DB())
					if ksErr := knowledgeStore.InitSchema(); ksErr != nil {
						logging.WithComponent("knowledge").Warn("Failed to initialize knowledge store schema", slog.Any("error", ksErr))
					} else {
						runner.SetKnowledgeStore(knowledgeStore)
					}

					// Initialize learning components if enabled
					if cfg.Memory.Learning == nil || cfg.Memory.Learning.Enabled {
						patternStore, patternErr := memory.NewGlobalPatternStore(cfg.Memory.Path)
						if patternErr != nil {
							logging.WithComponent("learning").Warn("Failed to create pattern store, learning disabled", slog.Any("error", patternErr))
						} else {
							extractor := memory.NewPatternExtractor(patternStore, learningStore)
							learningLoop := memory.NewLearningLoop(learningStore, extractor, nil)
							patternContext := executor.NewPatternContext(learningStore)

							runner.SetLearningLoop(learningLoop)
							runner.SetPatternContext(patternContext)
							runner.SetSelfReviewExtractor(extractor)

							logging.WithComponent("learning").Info("Learning system initialized")

							// GH-1991: Wire outcome tracker for model escalation
							outcomeTracker := memory.NewModelOutcomeTracker(learningStore)
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
						}
					}

					fmt.Println("   Learning:  ✓ initialized")
				}
			}

			// Create progress display (disabled in verbose mode - show raw JSON instead)
			progress := executor.NewProgressDisplay(task.ID, taskDesc, !verbose)

			// Suppress slog progress output when visual display is active
			runner.SuppressProgressLogs(!verbose)

			// Track Navigator mode detection
			var detectedNavMode string

			// Set up progress callback
			runner.OnProgress(func(taskID, phase string, pct int, message string) {
				// Detect Navigator mode from phase names
				switch phase {
				case "Navigator", "Loop Mode", "Task Mode":
					progress.SetNavigator(true, phase)
					detectedNavMode = phase
				case "Research", "Implement", "Verify":
					if detectedNavMode == "" {
						detectedNavMode = "nav-task"
					}
					progress.SetNavigator(true, detectedNavMode)
				}

				if verbose {
					// Verbose mode: simple line output
					timestamp := time.Now().Format("15:04:05")
					if message != "" {
						fmt.Printf("   [%s] %s (%d%%): %s\n", timestamp, phase, pct, message)
					}
				} else {
					// Normal mode: visual progress display
					progress.Update(phase, pct, message)
				}

				// Send progress event to alerts engine
				if alertsEngine != nil {
					alertsEngine.ProcessEvent(alerts.Event{
						Type:      alerts.EventTypeTaskProgress,
						TaskID:    taskID,
						TaskTitle: taskDesc,
						Project:   projectPath,
						Phase:     phase,
						Progress:  pct,
						Timestamp: time.Now(),
					})
				}
			})

			fmt.Println("⏳ Executing task with Claude Code...")
			if verbose {
				fmt.Println("   (streaming raw JSON)")
			}
			fmt.Println()

			// Start progress display with Navigator check
			progress.StartWithNavigatorCheck(projectPath)

			// Execute the task
			result, err := runner.Execute(ctx, task)
			if err != nil {
				return fmt.Errorf("execution failed: %w", err)
			}

			// Write result as JSON if --result-json flag is set
			if resultJSON != "" {
				data, jsonErr := json.MarshalIndent(result, "", "  ")
				if jsonErr != nil {
					fmt.Printf("   ⚠️  Failed to marshal result JSON: %v\n", jsonErr)
				} else if writeErr := os.WriteFile(resultJSON, data, 0644); writeErr != nil {
					fmt.Printf("   ⚠️  Failed to write result JSON to %s: %v\n", resultJSON, writeErr)
				}
			}

			// Build execution report
			report := &executor.ExecutionReport{
				TaskID:           result.TaskID,
				TaskTitle:        taskDesc,
				Success:          result.Success,
				Duration:         result.Duration,
				Branch:           task.Branch,
				CommitSHA:        result.CommitSHA,
				PRUrl:            result.PRUrl,
				HasNavigator:     detectedNavMode != "",
				NavMode:          detectedNavMode,
				TokensInput:      result.TokensInput,
				TokensOutput:     result.TokensOutput,
				EstimatedCostUSD: result.EstimatedCostUSD,
				ModelName:        result.ModelName,
				ErrorMessage:     result.Error,
			}

			// Finish progress display with comprehensive report
			progress.FinishWithReport(report)

			// Send alerts based on result
			if result.Success {
				if result.PRUrl == "" {
					fmt.Println("   ⚠️  PR not created (check gh auth status)")
				}

				// Send task completed event to alerts engine
				if alertsEngine != nil {
					alertsEngine.ProcessEvent(alerts.Event{
						Type:      alerts.EventTypeTaskCompleted,
						TaskID:    taskID,
						TaskTitle: taskDesc,
						Project:   projectPath,
						Timestamp: time.Now(),
						Metadata: map[string]string{
							"duration":   result.Duration.String(),
							"pr_url":     result.PRUrl,
							"commit_sha": result.CommitSHA,
						},
					})
				}
			} else {
				// Send task failed event to alerts engine
				if alertsEngine != nil {
					alertsEngine.ProcessEvent(alerts.Event{
						Type:      alerts.EventTypeTaskFailed,
						TaskID:    taskID,
						TaskTitle: taskDesc,
						Project:   projectPath,
						Error:     result.Error,
						Timestamp: time.Now(),
						Metadata: map[string]string{
							"duration": result.Duration.String(),
						},
					})
					// Give time for alert to be sent before exiting
					time.Sleep(500 * time.Millisecond)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&projectPath, "project", "p", "", "Project path (default: current directory)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be executed without running")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Stream Claude Code output")
	cmd.Flags().BoolVar(&enableAlerts, "alerts", false, "Enable alerts for task execution")
	cmd.Flags().BoolVar(&enableBudget, "budget", false, "Enable budget enforcement for this task")
	cmd.Flags().BoolVar(&localMode, "local", false, "Use problem-solving prompt without PR/Navigator constraints")
	cmd.Flags().StringVar(&resultJSON, "result-json", "", "Write execution result as JSON to file path")
	cmd.Flags().StringVar(&teamID, "team", "", "Team ID or name for project access scoping (overrides config)")
	cmd.Flags().StringVar(&teamMember, "team-member", "", "Member email for team access scoping (overrides config)")

	return cmd
}

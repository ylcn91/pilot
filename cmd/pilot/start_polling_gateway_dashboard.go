package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/dashboard"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/gateway"
	"github.com/ylcn91/pilot/internal/logging"
)

func (p *pollingRuntime) setupGateway() {
	cfg := p.cfg
	ctx := p.ctx
	runner := p.runner
	store := p.store
	projectPath := p.projectPath
	autopilotController := p.autopilotController

	// GH-1662: Start gateway in background so desktop app can reach /health
	var gwServer *gateway.Server // hoisted so TASK-332 alert-metrics wiring can run after alerts engine is created
	if !p.noGateway && cfg.Gateway != nil {
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

	p.gwServer = gwServer
}

func (p *pollingRuntime) setupDashboardProgram() {
	cfg := p.cfg
	cmd := p.cmd
	runner := p.runner
	store := p.store
	projectPath := p.projectPath
	autopilotControllers := p.autopilotControllers
	autopilotController := p.autopilotController

	// Create monitor and TUI program for dashboard mode
	var monitor *executor.Monitor
	var program *tea.Program
	var upgradeRequestCh chan struct{} // Channel for hot upgrade requests (GH-369)
	if p.dashboardMode {
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

	p.monitor = monitor
	p.program = program
	p.upgradeRequestCh = upgradeRequestCh
}

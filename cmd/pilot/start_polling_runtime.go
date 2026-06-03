package main

import (
	"context"
	"fmt"
	"log/slog"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/adapters/telegram"
	"github.com/ylcn91/pilot/internal/alerts"
	"github.com/ylcn91/pilot/internal/approval"
	"github.com/ylcn91/pilot/internal/architect"
	"github.com/ylcn91/pilot/internal/autopilot"
	"github.com/ylcn91/pilot/internal/banner"
	"github.com/ylcn91/pilot/internal/briefs"
	"github.com/ylcn91/pilot/internal/budget"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/gateway"
	"github.com/ylcn91/pilot/internal/logging"
	"github.com/ylcn91/pilot/internal/memory"
	"github.com/ylcn91/pilot/internal/quality"
)

// pollingRuntime holds the locals shared across the phases of runPollingMode.
// Fields populated by one phase and consumed by another live here; locals used
// within a single phase remain local to that phase's method.
type pollingRuntime struct {
	ctx           context.Context
	cancel        context.CancelFunc
	cmd           *cobra.Command
	cfg           *config.Config
	projectPath   string
	replace       bool
	dashboardMode bool
	noGateway     bool

	hasTelegram bool

	runner *executor.Runner

	approvalMgr           *approval.Manager
	tgApprovalHandler     telegram.ApprovalCallbackHandler
	tgApprovalHandlerImpl *approval.TelegramHandler

	autopilotControllers map[string]*autopilot.Controller
	autopilotController  *autopilot.Controller

	store               *memory.Store
	autopilotStateStore *autopilot.StateStore
	knowledgeStore      *memory.KnowledgeStore

	gwServer *gateway.Server

	monitor          *executor.Monitor
	program          *tea.Program
	upgradeRequestCh chan struct{}

	tgHandler *telegram.Handler

	alertsEngine *alerts.Engine
	dispatcher   *executor.Dispatcher
	enforcer     *budget.Enforcer

	ghPollers []*github.Poller

	briefScheduler *briefs.Scheduler

	architectStore     *architect.FindingsStore
	architectScheduler *architect.Scheduler
}

// validatePollingConfig validates Telegram and Slack Socket Mode config.
func validatePollingConfig(cfg *config.Config) error {
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

	return nil
}

func (p *pollingRuntime) setupRunner() error {
	cfg := p.cfg

	// Suppress logging BEFORE creating runner in dashboard mode (GH-190)
	// Runner caches its logger at creation time, so suppression must happen first
	if p.dashboardMode {
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

	p.runner = runner
	return nil
}

func (p *pollingRuntime) cleanupOrphanedWorktrees() {
	cfg := p.cfg
	// GH-962: Clean up orphaned worktree directories from previous crashed executions
	if cfg.Executor != nil && cfg.Executor.UseWorktree {
		if err := executor.CleanupOrphanedWorktrees(p.ctx, p.projectPath); err != nil {
			// Log the cleanup but don't fail startup - this is best-effort cleanup
			logging.WithComponent("start").Info("worktree cleanup completed", slog.String("result", err.Error()))
		} else {
			logging.WithComponent("start").Debug("worktree cleanup scan completed, no orphans found")
		}
	}
}

func (p *pollingRuntime) logStartupBanner() {
	cfg := p.cfg
	// Show startup banner (skip in dashboard mode to avoid corrupting TUI)
	if !p.dashboardMode {
		banner.StartupTelegram(version, p.projectPath, cfg.Adapters.Telegram.ChatID, cfg)
	}

	// Log autopilot status
	if cfg.Orchestrator.Autopilot != nil && cfg.Orchestrator.Autopilot.Enabled {
		logging.WithComponent("start").Info("autopilot enabled",
			slog.String("environment", string(cfg.Orchestrator.Autopilot.Environment)),
			slog.Bool("auto_merge", cfg.Orchestrator.Autopilot.AutoMerge),
			slog.Bool("auto_review", cfg.Orchestrator.Autopilot.AutoReview),
		)
	}
}

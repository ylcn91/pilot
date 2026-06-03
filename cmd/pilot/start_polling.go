package main

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/config"
)

// runPollingMode runs lightweight polling-only mode.
// When noGateway is false, the HTTP gateway starts in the background so the
// desktop app (and any other client hitting /health) can reach the daemon.
//
// The body is decomposed into pollingRuntime phase methods (see
// start_polling_*.go). This orchestrator wires the shared state, runs the
// phases in their original order, and owns the deferred cleanups.
func runPollingMode(cmd *cobra.Command, cfg *config.Config, projectPath string, replace, dashboardMode, noGateway bool) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := validatePollingConfig(cfg); err != nil {
		return err
	}

	p := &pollingRuntime{
		ctx:           ctx,
		cancel:        cancel,
		cmd:           cmd,
		cfg:           cfg,
		projectPath:   projectPath,
		replace:       replace,
		dashboardMode: dashboardMode,
		noGateway:     noGateway,
		hasTelegram:   cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.Enabled,
	}

	if err := p.setupRunner(); err != nil {
		return err
	}

	// Set up team project access checker if configured (GH-635)
	if teamCleanup := wireProjectAccessChecker(p.runner, cfg); teamCleanup != nil {
		defer teamCleanup()
	}

	p.cleanupOrphanedWorktrees()
	p.setupApproval()
	p.setupAutopilotControllers()

	if closeStore := p.setupStores(); closeStore != nil {
		defer closeStore()
	}

	p.setupGateway()
	p.setupDashboardProgram()

	if err := p.setupTelegram(); err != nil {
		return err
	}

	p.logStartupBanner()
	p.setupAlerts()
	p.setupDispatcher()
	p.setupBudget()
	p.startGitHubPolling()
	p.startAdapterPollers()
	p.startTelegramPolling()
	p.startSlackSocketMode()
	p.startBriefScheduler()
	p.startArchitectScheduler()

	return p.run()
}

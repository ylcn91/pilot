package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ylcn91/pilot/internal/dashboard"
	"github.com/ylcn91/pilot/internal/upgrade"
)

func (p *pollingRuntime) run() error {
	cfg := p.cfg
	ctx := p.ctx
	cancel := p.cancel
	runner := p.runner
	monitor := p.monitor
	program := p.program
	upgradeRequestCh := p.upgradeRequestCh
	ghPollers := p.ghPollers
	tgHandler := p.tgHandler
	dispatcher := p.dispatcher
	briefScheduler := p.briefScheduler
	architectScheduler := p.architectScheduler
	architectStore := p.architectStore

	// Dashboard mode: run TUI and handle shutdown via TUI quit
	if p.dashboardMode && program != nil {
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
					if architectStore != nil {
						program.Send(dashboard.UpdateFindings(architectStore.Findings())())
					}
				}
			}
		}()

		// Add startup logs after TUI starts (Send blocks if Run hasn't been called)
		go func() {
			time.Sleep(100 * time.Millisecond) // Wait for Run() to start
			program.Send(dashboard.AddLog(fmt.Sprintf("🚀 Pilot %s started - Polling mode", version))())
			if p.hasTelegram {
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
		if architectScheduler != nil {
			architectScheduler.Stop()
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
	if architectScheduler != nil {
		architectScheduler.Stop()
	}

	return nil
}

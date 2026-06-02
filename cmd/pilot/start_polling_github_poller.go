package main

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/autopilot"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/logging"
)

// createRepoPoller builds a GitHub poller for a single repo. The execution
// parameters (resolved once by the caller) are passed explicitly; everything
// else lives on the polling runtime.
func (p *pollingRuntime) createRepoPoller(
	repoFullName, projPath string,
	client *github.Client,
	label string,
	interval time.Duration,
	execMode github.ExecutionMode,
	waitForMerge bool,
	pollInterval, prTimeout time.Duration,
) (*github.Poller, error) {
	cfg := p.cfg
	ctx := p.ctx
	store := p.store
	runner := p.runner
	monitor := p.monitor
	program := p.program
	dispatcher := p.dispatcher
	alertsEngine := p.alertsEngine
	enforcer := p.enforcer
	autopilotControllers := p.autopilotControllers
	autopilotStateStore := p.autopilotStateStore

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

// startAutopilotLoops starts the autopilot processing loops for all controllers
// once pollers exist (corresponds to the len(ghPollers) > 0 block).
func (p *pollingRuntime) startAutopilotLoops(
	client *github.Client,
	execMode github.ExecutionMode,
	waitForMerge bool,
	pollInterval, prTimeout time.Duration,
) {
	cfg := p.cfg
	ctx := p.ctx
	store := p.store
	runner := p.runner
	alertsEngine := p.alertsEngine
	autopilotControllers := p.autopilotControllers
	autopilotController := p.autopilotController
	ghPollers := p.ghPollers

	if !p.dashboardMode && execMode == github.ExecutionModeSequential && waitForMerge {
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

	if len(autopilotControllers) > 0 && !p.dashboardMode {
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

// startStaleLabelCleanup starts the stale label cleanup for the default repo
// when enabled (corresponds to the StaleLabelCleanup block).
func (p *pollingRuntime) startStaleLabelCleanup(client *github.Client) {
	cfg := p.cfg
	ctx := p.ctx
	store := p.store
	monitor := p.monitor
	ghPollers := p.ghPollers

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
				if !p.dashboardMode {
					fmt.Printf("⚠️  Stale label cleanup disabled: %v\n", cleanerErr)
				}
			} else {
				if !p.dashboardMode {
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
				} else if !p.dashboardMode && n > 0 {
					fmt.Printf("🔄 Startup recovery: cleared %d stuck pilot-in-progress label(s)\n", n)
				}
				go cleaner.Start(ctx)
			}
		}
	}
}

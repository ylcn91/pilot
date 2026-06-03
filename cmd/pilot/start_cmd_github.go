package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/logging"
	"github.com/ylcn91/pilot/internal/pilot"
)

// wireGatewayGitHubPolling configures GitHub polling for gateway mode and, on
// success, appends the resulting poller to pilotOpts. It mirrors the inline
// block previously in newStartCmd (GH-350, GH-351, GH-392).
//
// Early return: an invalid repo format returns a non-nil error, which the
// caller propagates exactly as the original inline `return` did. All other
// failure paths log and continue, leaving pilotOpts unchanged.
func wireGatewayGitHubPolling(gw *gatewayInfra, cfg *config.Config, projectPath string, pilotOpts *[]pilot.Option) error {
	token := cfg.Adapters.GitHub.Token
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}

	if token == "" || cfg.Adapters.GitHub.Repo == "" {
		return nil
	}

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
	if gw.AutopilotController != nil {
		pollerOpts = append(pollerOpts, github.WithOnPRCreated(gw.AutopilotController.OnPRCreated))
		// Wire sub-issue PR callback so epic sub-PRs are tracked by autopilot (GH-594)
		gw.Runner.SetOnSubIssuePRCreated(gw.AutopilotController.OnPRCreated)
	}

	// Wire sub-issue merge-wait so epic sub-issues block until their PR merges (GH-2179)
	if waitForMerge {
		gwRepoParts := strings.SplitN(cfg.Adapters.GitHub.Repo, "/", 2)
		if len(gwRepoParts) == 2 {
			mergeWaiter := github.NewMergeWaiter(client, gwRepoParts[0], gwRepoParts[1], &github.MergeWaiterConfig{
				PollInterval: pollInterval,
				Timeout:      prTimeout,
			})
			gw.Runner.SetSubIssueMergeWait(func(ctx context.Context, prNumber int) error {
				_, err := mergeWaiter.WaitForMerge(ctx, prNumber)
				return err
			})
		}
	}

	// GH-2211: Wire native sub-issue linker so epic children get proper parent→child links
	gw.Runner.SetSubIssueLinker(client)

	// GH-726: Wire processed issue persistence for gateway poller
	if gw.AutopilotStateStore != nil {
		pollerOpts = append(pollerOpts, github.WithProcessedStore(gw.AutopilotStateStore))
	}

	// GH-2201: Wire task checker for retry grace period (gateway mode)
	if gw.Store != nil {
		pollerOpts = append(pollerOpts, github.WithTaskChecker(storeTaskChecker{store: gw.Store}))
	}

	// GH-2242: Wire execution checker to prevent re-dispatch of completed tasks (gateway mode)
	if gw.Store != nil {
		pollerOpts = append(pollerOpts, github.WithExecutionChecker(gw.Store, projectPath))
	}

	// Wire issue metrics recorder for rate-limit tracking.
	if gw.AutopilotController != nil {
		pollerOpts = append(pollerOpts, github.WithIssueMetricsRecorder(gw.AutopilotController.Metrics()))
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
			if gw.Store != nil {
				pollerOpts = append(pollerOpts, github.WithExecutionSaver(storeExecutionSaver{store: gw.Store}))
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
			result, err = handleGitHubIssueWithResult(retryCtx, cfg, client, issue, projectPath, gwSourceRepo, gw.Dispatcher, gw.Runner, gw.Monitor, gw.Program, gw.AlertsEngine, gw.Enforcer, gw.TeamAdapter)
		} else {
			result, err = handleGitHubIssueWithResult(retryCtx, cfg, client, issue, projectPath, gwSourceRepo, gw.Dispatcher, gw.Runner, gw.Monitor, gw.Program, gw.AlertsEngine, gw.Enforcer, gw.TeamAdapter)
		}

		// GH-797: Call OnPRCreated for retried issues so autopilot tracks their PRs
		if result != nil && result.PRNumber > 0 && gw.AutopilotController != nil {
			gw.AutopilotController.OnPRCreated(result.PRNumber, result.PRURL, issue.Number, result.HeadSHA, result.BranchName, issue.NodeID)
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
				return handleGitHubIssueWithResult(issueCtx, cfg, client, issue, projectPath, gwSourceRepo, gw.Dispatcher, gw.Runner, gw.Monitor, gw.Program, gw.AlertsEngine, gw.Enforcer, gw.TeamAdapter)
			}),
		)
	} else {
		pollerOpts = append(pollerOpts,
			github.WithScheduler(rateLimitScheduler),
			github.WithMaxConcurrent(cfg.Orchestrator.MaxConcurrent),
			github.WithOnIssueWithResult(func(issueCtx context.Context, issue *github.Issue) (*github.IssueResult, error) {
				return handleGitHubIssueWithResult(issueCtx, cfg, client, issue, projectPath, gwSourceRepo, gw.Dispatcher, gw.Runner, gw.Monitor, gw.Program, gw.AlertsEngine, gw.Enforcer, gw.TeamAdapter)
			}),
		)
	}

	ghPoller, err := github.NewPoller(client, cfg.Adapters.GitHub.Repo, label, interval, pollerOpts...)
	if err != nil {
		logging.WithComponent("start").Warn("GitHub polling disabled in gateway mode", slog.Any("error", err))
	} else {
		*pilotOpts = append(*pilotOpts, pilot.WithGitHubPoller(ghPoller))
		logging.WithComponent("start").Info("GitHub polling enabled in gateway mode",
			slog.String("repo", cfg.Adapters.GitHub.Repo),
			slog.Duration("interval", interval),
			slog.String("mode", string(execMode)),
		)

		// Start autopilot processing loop if controller initialized
		if gw.AutopilotController != nil {
			ctx := context.Background()
			// Scan for existing PRs created by Pilot
			if scanErr := gw.AutopilotController.ScanExistingPRs(ctx); scanErr != nil {
				logging.WithComponent("autopilot").Warn("failed to scan existing PRs",
					slog.Any("error", scanErr),
				)
			}

			// Scan for recently merged PRs that may need release (GH-416)
			if scanErr := gw.AutopilotController.ScanRecentlyMergedPRs(ctx); scanErr != nil {
				logging.WithComponent("autopilot").Warn("failed to scan merged PRs",
					slog.Any("error", scanErr),
				)
			}

			logging.WithComponent("start").Info("autopilot enabled in gateway mode",
				slog.String("environment", string(cfg.Orchestrator.Autopilot.Environment)),
			)
			go func() {
				if runErr := gw.AutopilotController.Run(ctx); runErr != nil && runErr != context.Canceled {
					logging.WithComponent("autopilot").Error("autopilot controller stopped",
						slog.Any("error", runErr),
					)
				}
			}()
		}
	}

	return nil
}

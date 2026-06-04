package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/budget"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/logging"
	"github.com/ylcn91/pilot/internal/quality"
)

// setupTaskRunner loads config and builds the executor runner with all of its
// task-command wiring: repo allowlist, orphaned-worktree cleanup, quality
// gates, decomposer status, per-task budget limits, and team project access.
//
// It returns the loaded cfg (needed by later learning-system wiring) plus a
// team-cleanup func that must run at the *outer* RunE return. The caller
// registers `defer teamCleanup()` whenever a non-nil func is returned, matching
// the original `defer runTeamCleanup()` placement. teamCleanup is nil when no
// access scoping was enabled.
func setupTaskRunner(ctx context.Context, cmd *cobra.Command, projectPath, teamID, teamMember string, skipSelfReview bool) (*executor.Runner, *config.Config, func(), error) {
	// Load config for runner setup
	configPath := cfgFile
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}
	cfg, cfgErr := config.Load(configPath)
	if cfgErr != nil {
		return nil, nil, nil, fmt.Errorf("failed to load config: %w", cfgErr)
	}

	// Apply team flag overrides (GH-635)
	applyTeamOverrides(cfg, cmd, teamID, teamMember)
	if skipSelfReview {
		if cfg.Executor == nil {
			cfg.Executor = executor.DefaultBackendConfig()
		}
		cfg.Executor.SkipSelfReview = true
	}

	// Create the executor runner with config (GH-956: enables worktree isolation, decomposer, model routing)
	runner, runnerErr := executor.NewRunnerWithConfig(cfg.Executor)
	if runnerErr != nil {
		return nil, nil, nil, fmt.Errorf("failed to create executor runner: %w", runnerErr)
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
	var teamCleanup func()
	if runTeamCleanup := wireProjectAccessChecker(runner, cfg); runTeamCleanup != nil {
		teamCleanup = runTeamCleanup
		fmt.Println("   Team:      ✓ project access scoping enabled")
	}

	return runner, cfg, teamCleanup, nil
}

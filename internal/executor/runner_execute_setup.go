package executor

import (
	"fmt"
	"log/slog"
)

// executeSetup runs the pre-worktree validation and isolated worktree creation
// (original lines ~47-151). It mutates s.executionPath and s.cleanupWorktree.
// It returns a non-nil result/error only on an abort path.
func (r *Runner) executeSetup(s *executeState, allowWorktree bool) (*ExecutionResult, error) {
	task := s.task
	ctx := s.ctx

	// Signal monitor that execution is actually starting (queued→running transition)
	if r.monitor != nil {
		r.monitor.Start(task.ID)
	}

	// GH-1599: Log task started milestone
	r.saveLogEntry(task.ID, "info", "Task started: "+task.Title)

	// GH-386: Validate source repo matches project path to prevent cross-project execution
	if task.SourceRepo != "" && task.ProjectPath != "" {
		if err := ValidateRepoProjectMatch(task.SourceRepo, task.ProjectPath); err != nil {
			return &ExecutionResult{
				TaskID:  task.ID,
				Success: false,
				Error:   fmt.Sprintf("cross-project execution blocked: %v", err),
			}, fmt.Errorf("cross-project execution blocked: %w", err)
		}
	}

	// GH-634: Enforce team permissions before execution
	if r.teamChecker != nil && task.MemberID != "" {
		if err := r.teamChecker.CheckProjectAccess(task.MemberID, task.ProjectPath, "execute_tasks"); err != nil {
			return &ExecutionResult{
				TaskID:  task.ID,
				Success: false,
				Error:   fmt.Sprintf("permission denied: %v", err),
			}, fmt.Errorf("permission check failed: %w", err)
		}
	}

	// GH-936: Create isolated worktree if configured
	// This allows execution even when user has uncommitted changes in their working directory
	s.executionPath = task.ProjectPath

	// Debug: log worktree condition state
	r.log.Info("Worktree condition check",
		slog.Bool("allowWorktree", allowWorktree),
		slog.Bool("configNotNil", r.config != nil),
		slog.Bool("useWorktree", r.config != nil && r.config.UseWorktree),
		slog.String("branch", task.Branch),
		slog.Bool("directCommit", task.DirectCommit),
	)

	if allowWorktree && r.config != nil && r.config.UseWorktree && task.Branch != "" && !task.DirectCommit {
		r.log.Info("Creating isolated worktree for execution",
			slog.String("task_id", task.ID),
			slog.String("branch", task.Branch),
		)
		r.reportProgress(task.ID, "Worktree", 1, "Creating isolated worktree...")

		var worktreePath string
		var cleanup func()
		var err error

		// GH-1078: Use pool if available, otherwise fall back to direct creation
		if r.worktreeManager != nil && r.worktreeManager.PoolSize() > 0 {
			r.log.Debug("Using worktree pool",
				slog.Int("pool_available", r.worktreeManager.PoolAvailable()),
			)
			var result *WorktreeResult
			result, err = r.worktreeManager.Acquire(ctx, task.ID, task.Branch, "")
			if err == nil {
				worktreePath = result.Path
				cleanup = result.Cleanup
			}
		} else {
			worktreePath, cleanup, err = CreateWorktreeWithBranch(
				ctx, task.ProjectPath, task.ID, task.Branch, "")
		}

		if err != nil {
			r.log.Error("Failed to create worktree",
				slog.String("task_id", task.ID),
				slog.Any("error", err),
			)
			return &ExecutionResult{
				TaskID:  task.ID,
				Success: false,
				Error:   fmt.Sprintf("failed to create worktree: %v", err),
			}, fmt.Errorf("worktree creation failed: %w", err)
		}
		s.cleanupWorktree = cleanup
		s.executionPath = worktreePath

		// Copy Navigator config to worktree (handles untracked .agent/ content)
		if err := EnsureNavigatorInWorktree(task.ProjectPath, worktreePath); err != nil {
			cleanup()
			r.log.Error("Failed to copy Navigator to worktree",
				slog.String("task_id", task.ID),
				slog.Any("error", err),
			)
			return &ExecutionResult{
				TaskID:  task.ID,
				Success: false,
				Error:   fmt.Sprintf("failed to setup navigator in worktree: %v", err),
			}, fmt.Errorf("navigator worktree setup failed: %w", err)
		}

		r.log.Info("Using isolated worktree",
			slog.String("task_id", task.ID),
			slog.String("worktree", worktreePath),
		)
		r.reportProgress(task.ID, "Worktree", 2, "Worktree ready")
	}

	return nil, nil
}

// executePreflight runs pre-flight checks, Navigator auto-init, and complexity
// detection (original lines ~165-199). It mutates s.complexity and returns a
// non-nil result/error only on an abort path.
func (r *Runner) executePreflight(s *executeState) (*ExecutionResult, error) {
	task := s.task
	ctx := s.ctx
	executionPath := s.executionPath

	// GH-915: Run pre-flight checks to catch environmental issues early
	// Skip when using mock backends in tests (skipPreflightChecks flag)
	// GH-1002: Skip git_clean check when worktree isolation is enabled
	// LocalMode: skip git_clean because sandbox workspaces can have pre-existing files that
	// create dirty git state after our install script commits.
	if !r.skipPreflightChecks {
		preflightOpts := PreflightOptions{
			SkipGitClean:   task.LocalMode || (r.config != nil && r.config.UseWorktree),
			BackendType:    r.backendType(),
			BackendCommand: r.backendCommand(),
		}
		if err := RunPreflightChecksWithOptions(ctx, executionPath, preflightOpts); err != nil {
			r.log.Warn("Pre-flight check failed",
				slog.String("task_id", task.ID),
				slog.Any("error", err),
			)
			return &ExecutionResult{
				TaskID:  task.ID,
				Success: false,
				Error:   fmt.Sprintf("pre-flight check failed: %v", err),
			}, fmt.Errorf("pre-flight check failed: %w", err)
		}
	}

	// Auto-init Navigator if configured and missing
	// Use executionPath to check/init in worktree if worktree isolation is active
	// Skip for LocalMode — sandbox tasks don't use Navigator (GH-2108)
	if !task.LocalMode && r.config != nil && r.config.Navigator != nil && r.config.Navigator.AutoInit {
		if err := r.maybeInitNavigator(executionPath); err != nil {
			r.log.Warn("Navigator auto-init failed", slog.Any("error", err))
			// Continue without Navigator - graceful degradation
		}
	}

	// Detect complexity for routing decisions
	s.complexity = DetectComplexity(task)

	return nil, nil
}

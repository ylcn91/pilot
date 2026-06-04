package executor

import (
	"context"
	"log/slog"
	"strings"
)

// runSelfReview executes a self-review phase where Claude examines its changes.
// This catches issues like unwired config, undefined methods, or incomplete implementations.
// Returns nil if review passes or is skipped, error only for critical failures.
func (r *Runner) runSelfReview(ctx context.Context, task *Task, state *progressState) error {
	// Skip self-review if disabled in config
	if r.config != nil && r.config.SkipSelfReview {
		r.log.Debug("Self-review skipped (disabled in config)", slog.String("task_id", task.ID))
		return nil
	}

	// Skip for trivial tasks - they don't need self-review
	complexity := DetectComplexity(task)
	if complexity.ShouldSkipNavigator() {
		r.log.Debug("Self-review skipped (trivial task)", slog.String("task_id", task.ID))
		return nil
	}

	r.log.Info("Running self-review phase", slog.String("task_id", task.ID))
	r.reportProgress(task.ID, "Self-Review", 95, "Reviewing changes...")

	// Review (and any review fixes) must run in the isolated worktree, not the
	// shared project root. state.executionPath is set after worktree setup; fall
	// back to task.ProjectPath only when unset (unit tests / no worktree). This
	// restores GH-936 worktree isolation that the execute/retry paths already have.
	reviewPath := state.executionPath
	if reviewPath == "" {
		reviewPath = task.ProjectPath
	}

	reviewTask := *task
	if reviewTask.BaseBranch == "" {
		if baseBranch, err := NewGitOperations(reviewPath).GetDefaultBranch(ctx); err == nil && baseBranch != "" {
			reviewTask.BaseBranch = baseBranch
		}
	}
	reviewPrompt := r.buildSelfReviewPrompt(&reviewTask)

	// Choose the self-review backend. In TDD mode the QA role owns review, so
	// route through r.qaBackend (wired from tdd.qa). Outside TDD the dedicated
	// review-stage backend is used. When tdd.qa is unset qaBackend falls back to
	// the primary backend (== reviewBackend default), so behavior is unchanged.
	selfReviewBackend := r.reviewBackend
	// reviewStage is the StageConfig whose model/effort override applies to this
	// self-review call: the TDD qa role when TDD owns review, else the pipeline
	// review stage. Nil when neither is configured (falls back to run-level).
	var reviewStage *StageConfig
	if r.config != nil && r.config.Pipeline != nil {
		reviewStage = r.config.Pipeline.Review
	}
	if r.config != nil && r.config.TDD != nil && r.config.TDD.Enabled {
		selfReviewBackend = r.qaBackend
		reviewStage = r.config.TDD.QA
	}

	// Execute self-review with backend-aware timeout. OpenCode runs are
	// genuinely slower than Claude Code; the 2-minute default cancels review
	// mid-flight and surfaces as a regression. GH-2416.
	reviewCtx, cancel := context.WithTimeout(ctx, r.selfReviewTimeout())
	defer cancel()

	// Select model and effort (use same routing as main execution), then apply
	// the review-stage override (pipeline.review, or tdd.qa in TDD mode) so a
	// stage-level model/effort wins over the run-level selection.
	selectedModel := r.resolveSelectedModel(task)
	selectedEffort := r.modelRouter.SelectEffort(task)
	selectedModel, selectedEffort = effectiveStageModelEffort(reviewStage, selectedModel, selectedEffort)

	// GH-1265: Determine if session resume is enabled and session ID is available.
	// Cross-backend resume is invalid: only claude-code honors --resume, and an
	// execute-stage session id is meaningless to a different review backend. Only
	// pass ResumeSessionID when review runs on the same backend instance that
	// executed; otherwise review works purely from the git diff.
	var resumeSessionID string
	if selfReviewBackend == r.execBackend &&
		r.config != nil && r.config.ClaudeCode != nil && r.config.ClaudeCode.UseSessionResume {
		if state.sessionID != "" {
			resumeSessionID = state.sessionID
			r.log.Debug("Using session resume for self-review",
				slog.String("task_id", task.ID),
				slog.String("session_id", resumeSessionID),
			)
		}
	}

	reviewAllowed, reviewMCP := r.executionToolOptions()
	result, err := selfReviewBackend.Execute(reviewCtx, ExecuteOptions{
		Prompt:          reviewPrompt,
		ProjectPath:     reviewPath,
		Verbose:         task.Verbose,
		Model:           selectedModel,
		Effort:          selectedEffort,
		ResumeSessionID: resumeSessionID,
		AllowedTools:    reviewAllowed,
		MCPConfigPath:   reviewMCP,
		EventHandler: func(event BackendEvent) {
			// Track tokens from self-review
			state.tokensInput += event.TokensInput
			state.tokensOutput += event.TokensOutput
			state.cacheCreationInputTokens += event.CacheCreationInputTokens
			state.cacheReadInputTokens += event.CacheReadInputTokens
			// Extract any new commit SHAs from self-review fixes
			if event.Type == EventTypeToolResult && event.ToolResult != "" {
				extractCommitSHA(event.ToolResult, state)
			}
		},
	})

	if err != nil {
		// Self-review failure is not fatal - log and continue
		r.log.Warn("Self-review execution failed",
			slog.String("task_id", task.ID),
			slog.Any("error", err),
		)
		return nil
	}

	// Check if review found and fixed issues
	if strings.Contains(result.Output, "REVIEW_FIXED:") {
		r.log.Info("Self-review fixed issues",
			slog.String("task_id", task.ID),
		)
		r.reportProgress(task.ID, "Self-Review", 97, "Issues fixed during review")
	} else if strings.Contains(result.Output, "REVIEW_PASSED") {
		r.log.Info("Self-review passed",
			slog.String("task_id", task.ID),
		)
		r.reportProgress(task.ID, "Self-Review", 97, "Review passed")
	} else {
		r.log.Debug("Self-review completed (no explicit signal)",
			slog.String("task_id", task.ID),
		)
	}

	// GH-1955: Extract patterns from self-review output (non-blocking)
	if r.selfReviewExtractor != nil && result.Output != "" {
		extractResult, extractErr := r.selfReviewExtractor.ExtractFromSelfReview(ctx, result.Output, task.ProjectPath)
		if extractErr != nil {
			r.log.Warn("Failed to extract patterns from self-review",
				slog.String("task_id", task.ID),
				slog.Any("error", extractErr),
			)
		} else if len(extractResult.Patterns)+len(extractResult.AntiPatterns) > 0 {
			if saveErr := r.selfReviewExtractor.SaveExtractedPatterns(ctx, extractResult); saveErr != nil {
				r.log.Warn("Failed to save self-review patterns",
					slog.String("task_id", task.ID),
					slog.Any("error", saveErr),
				)
			} else {
				r.log.Info("Saved patterns from self-review",
					slog.String("task_id", task.ID),
					slog.Int("patterns", len(extractResult.Patterns)),
					slog.Int("anti_patterns", len(extractResult.AntiPatterns)),
				)
			}
		}
	}

	return nil
}

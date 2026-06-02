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

	reviewPrompt := r.buildSelfReviewPrompt(task)

	// Execute self-review with backend-aware timeout. OpenCode runs are
	// genuinely slower than Claude Code; the 2-minute default cancels review
	// mid-flight and surfaces as a regression. GH-2416.
	reviewCtx, cancel := context.WithTimeout(ctx, r.selfReviewTimeout())
	defer cancel()

	// Select model and effort (use same routing as main execution).
	selectedModel := r.resolveSelectedModel(task)
	selectedEffort := r.modelRouter.SelectEffort(task)

	// GH-1265: Determine if session resume is enabled and session ID is available.
	// Cross-backend resume is invalid: only claude-code honors --resume, and an
	// execute-stage session id is meaningless to a different review backend. Only
	// pass ResumeSessionID when review runs on the same backend instance that
	// executed; otherwise review works purely from the git diff.
	var resumeSessionID string
	if r.reviewBackend == r.execBackend &&
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
	result, err := r.reviewBackend.Execute(reviewCtx, ExecuteOptions{
		Prompt:          reviewPrompt,
		ProjectPath:     task.ProjectPath,
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

package executor

import (
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// executeNoCommitRetry handles no-commit detection and the explicit-instruction
// retry on the success path (GH-916, original lines ~1407-1604). It returns a
// non-nil result/error when the retry path terminates execution; otherwise it
// returns (nil, nil) to continue.
func (r *Runner) executeNoCommitRetry(s *executeState) (*ExecutionResult, error) {
	task := s.task
	ctx := s.ctx
	log := s.log
	git := s.git
	executionPath := s.executionPath
	result := s.result
	backendResult := s.backendResult
	state := s.state
	recorder := s.recorder
	selectedModel := s.selectedModel
	selectedEffort := s.selectedEffort
	watchdogTimeout := s.watchdogTimeout

	// No-commit detection and retry (GH-916)
	// ~10% of failures are "No commits between main and pilot/GH-XXX"
	// Claude runs successfully but makes no actual changes, then PR creation fails.
	if task.CreatePR && !task.DirectCommit && task.Branch != "" {
		baseBranch := task.BaseBranch
		if baseBranch == "" {
			baseBranch, _ = git.GetDefaultBranch(ctx)
			if baseBranch == "" {
				baseBranch = "main"
			}
		}

		commitCount, countErr := git.CountNewCommits(ctx, baseBranch)
		if countErr != nil {
			log.Warn("Failed to count commits for no-commit check",
				slog.String("task_id", task.ID),
				slog.Any("error", countErr),
			)
		} else if commitCount == 0 {
			log.Warn("Claude made no commits, retrying with explicit instruction",
				slog.String("task_id", task.ID),
				slog.String("branch", task.Branch),
			)
			r.reportProgress(task.ID, "Retry", 91, "No commits detected, retrying...")

			// Build retry prompt with explicit instruction.
			// GH-2777: Offer DECLINED:<reason> as an escape hatch so Claude can
			// signal that a task is genuinely unactionable rather than silently
			// producing no output. The DECLINED path avoids the pilot-failed label
			// and adds pilot-needs-clarification instead.
			retryPrompt := fmt.Sprintf(`## Retry: No Changes Detected

The previous execution completed but made no code changes.

**Original Task:** %s

%s

**You have two options:**

**Option A — Implement:** If this task is actionable, implement the required changes and create at least one git commit. Do NOT just analyze or plan — actually write and commit code.

**Option B — Decline:** If this task is genuinely unactionable (e.g. the requirements are ambiguous, the requested feature already exists, or it would require information you don't have), output exactly one line in this format and nothing else after it:

  DECLINED:<concise reason — one sentence>

Examples of valid DECLINED lines:
  DECLINED: The authentication module requested already exists in internal/auth/jwt.go.
  DECLINED: The issue asks to "improve performance" without specifying which endpoint or metric.
  DECLINED: This requires production database credentials that are not available in this environment.

Only use DECLINED if implementation is truly impossible or undefined. Do not decline due to difficulty alone.`, task.Title, task.Description)

			// Execute retry
			noopRetryAllowed, noopRetryMCP := r.executionToolOptions()
			retryResult, retryErr := r.execBackend.Execute(ctx, ExecuteOptions{
				Prompt:          retryPrompt,
				ProjectPath:     executionPath, // TASK-323: retry in the worktree so CountNewCommits can see its commits
				Verbose:         task.Verbose,
				Model:           selectedModel,
				Effort:          selectedEffort,
				WatchdogTimeout: watchdogTimeout,
				AllowedTools:    noopRetryAllowed,
				MCPConfigPath:   noopRetryMCP,
				EventHandler: func(event BackendEvent) {
					// Track tokens from retry
					state.tokensInput += event.TokensInput
					state.tokensOutput += event.TokensOutput
					state.cacheCreationInputTokens += event.CacheCreationInputTokens
					state.cacheReadInputTokens += event.CacheReadInputTokens
					// Extract any commit SHAs from retry
					if event.Type == EventTypeToolResult && event.ToolResult != "" {
						extractCommitSHA(event.ToolResult, state)
					}
					if recorder != nil {
						if recErr := recorder.RecordEvent(event.Raw); recErr != nil {
							log.Warn("Failed to record retry event", slog.Any("error", recErr))
						}
					}
				},
			})

			// Update result with retry tokens
			if retryResult != nil {
				result.TokensInput += retryResult.TokensInput
				result.TokensOutput += retryResult.TokensOutput
				result.TokensTotal = result.TokensInput + result.TokensOutput
			}

			// Check again after retry
			commitCount, _ = git.CountNewCommits(ctx, baseBranch)
			if commitCount == 0 {
				result.Success = false

				// GH-2777: Collect the last assistant text from the retry response
				// so we can check for an explicit DECLINED marker first.
				refusal := ""
				if backendResult != nil {
					refusal = strings.TrimSpace(backendResult.LastAssistantText)
				}
				if retryResult != nil && strings.TrimSpace(retryResult.LastAssistantText) != "" {
					refusal = strings.TrimSpace(retryResult.LastAssistantText)
				}

				// GH-2777: Check for an explicit DECLINED:<reason> marker before
				// classifying as a generic no_changes failure. DECLINED avoids
				// pilot-failed and instead adds pilot-needs-clarification.
				if declinedReason, ok := parseDeclinedReason(refusal); ok {
					result.Declined = true
					result.DeclinedReason = declinedReason
					result.Outcome = "declined" // TASK-358
					if backendResult != nil {
						backendResult.ErrorType = string(ErrorTypeDeclined)
					}
					log.Warn("Task declined by executor",
						slog.String("task_id", task.ID),
						slog.String("reason", declinedReason),
					)
					r.reportProgress(task.ID, "Declined", 100, "Task declined: "+declinedReason)
					r.persistBackendDiagnostics(task.ID, backendResult)

					if recorder != nil {
						recorder.SetModel(result.ModelName)
						recorder.SetNavigator(state.hasNavigator)
						if finErr := recorder.Finish("declined"); finErr != nil {
							log.Warn("Failed to finish recording", slog.Any("error", finErr))
						}
					}
					return result, nil
				}

				// GH-2328: classify this as ErrorTypeNoChanges and carry the
				// final assistant message so the failure comment surfaces the
				// refusal reason instead of a generic "no changes" string.
				result.Outcome = "no_op" // TASK-358: no edits made, not a code failure
				if refusal != "" {
					result.Error = fmt.Sprintf("no_changes: Claude completed but made no code changes after retry — %s", refusal)
				} else {
					result.Error = "no_changes: Claude completed but made no code changes after retry"
				}
				if backendResult != nil {
					backendResult.ErrorType = string(ErrorTypeNoChanges)
					if refusal != "" {
						backendResult.LastAssistantText = refusal
					}
				}
				log.Error("No commits after retry",
					slog.String("task_id", task.ID),
				)
				r.reportProgress(task.ID, "Failed", 100, result.Error)

				// GH-2328: persist no_changes classification + refusal text.
				r.persistBackendDiagnostics(task.ID, backendResult)

				// Emit task failed event
				r.emitAlertEvent(AlertEvent{
					Type:      AlertEventTypeTaskFailed,
					TaskID:    task.ID,
					TaskTitle: task.Title,
					Project:   task.ProjectPath,
					Error:     result.Error,
					Metadata: map[string]string{
						"reason": "no_commits_after_retry",
					},
					Timestamp: time.Now(),
				})

				// Finish recording with failed status
				if recorder != nil {
					recorder.SetModel(result.ModelName)
					recorder.SetNavigator(state.hasNavigator)
					if finErr := recorder.Finish("no_commits"); finErr != nil {
						log.Warn("Failed to finish recording", slog.Any("error", finErr))
					}
				}
				return result, nil
			} else if retryErr != nil {
				log.Warn("Retry execution error (but commits exist)",
					slog.String("task_id", task.ID),
					slog.Any("error", retryErr),
					slog.Int("commit_count", commitCount),
				)
			}

			log.Info("Retry successful - commits detected",
				slog.String("task_id", task.ID),
				slog.Int("commit_count", commitCount),
			)
			r.reportProgress(task.ID, "Retry Success", 92, fmt.Sprintf("Retry successful: %d commits", commitCount))

			// Update commit SHA from retry if state captured it
			if len(state.commitSHAs) > 0 {
				result.CommitSHA = state.commitSHAs[len(state.commitSHAs)-1]
			} else if sha, shaErr := git.GetCurrentCommitSHA(ctx); shaErr == nil {
				result.CommitSHA = sha
			}
		}
	}

	return nil, nil
}

package executor

import (
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/ylcn91/pilot/internal/quality"
	"github.com/ylcn91/pilot/internal/webhooks"
)

// failQualityGates emits the task-failed alert + webhook and finishes the
// recorder for a quality-gate abort path, then returns (s.result, nil). The
// caller is responsible for having already set s.result.Success/Error and the
// terminal progress report. alertType/metadata/phase are the only values that
// differ across the three quality-failure exits.
func (r *Runner) failQualityGates(s *executeState, alertType AlertEventType, metadata map[string]string, phase string) (*ExecutionResult, error) {
	r.emitAlertEvent(AlertEvent{
		Type:      alertType,
		TaskID:    s.task.ID,
		TaskTitle: s.task.Title,
		Project:   s.task.ProjectPath,
		Error:     s.result.Error,
		Metadata:  metadata,
		Timestamp: time.Now(),
	})

	r.dispatchWebhook(s.ctx, webhooks.EventTaskFailed, webhooks.TaskFailedData{
		TaskID:   s.task.ID,
		Title:    s.task.Title,
		Project:  s.task.ProjectPath,
		Duration: time.Since(s.start),
		Error:    s.result.Error,
		Phase:    phase,
	})

	if s.recorder != nil {
		s.recorder.SetModel(s.result.ModelName)
		s.recorder.SetNavigator(s.state.hasNavigator)
		if finErr := s.recorder.Finish("failed"); finErr != nil {
			s.log.Warn("Failed to finish recording", slog.Any("error", finErr))
		}
	}

	return s.result, nil
}

// executeQualityGates auto-enables a minimal build gate when no quality config
// is present, then runs the quality-gate retry loop (GH-363/GH-209, original
// lines ~1606-1926). It sets s.qualityGatesPassed. It returns a non-nil
// result/error on any quality-failure abort path; otherwise (nil, nil).
func (r *Runner) executeQualityGates(s *executeState) (*ExecutionResult, error) {
	task := s.task
	ctx := s.ctx
	log := s.log
	executionPath := s.executionPath
	result := s.result
	state := s.state
	recorder := s.recorder
	selectedModel := s.selectedModel
	selectedEffort := s.selectedEffort

	// Auto-enable minimal build gate if not configured (GH-363)
	// This ensures broken code never becomes a PR, even without explicit quality config
	if r.qualityCheckerFactory == nil {
		buildCmd := quality.DetectBuildCommand(executionPath)
		testCmd := quality.DetectTestCommand(executionPath)
		if buildCmd != "" {
			log.Info("Auto-enabling build gate (no quality config)",
				slog.String("command", buildCmd),
			)

			// Create minimal quality checker with auto-detected build command
			minimalConfig := quality.MinimalBuildGate()
			minimalConfig.Gates[0].Command = buildCmd

			// GH-2398: also auto-enable a test gate when a test runner is
			// detectable. Empty testCmd → skip the gate entirely instead of
			// failing it on workspaces that lack a Makefile / test harness.
			if testCmd != "" {
				log.Info("Auto-enabling test gate", slog.String("command", testCmd))
				minimalConfig.Gates = append(minimalConfig.Gates, &quality.Gate{
					Name:        "test",
					Type:        quality.GateTest,
					Command:     testCmd,
					Required:    true,
					Timeout:     5 * time.Minute,
					MaxRetries:  1,
					RetryDelay:  3 * time.Second,
					FailureHint: "Fix failing tests in the changed files",
				})
			}

			r.qualityCheckerFactory = func(taskID, projectPath string) QualityChecker {
				return &simpleQualityChecker{
					config:      minimalConfig,
					projectPath: projectPath,
					taskID:      taskID,
				}
			}
		}
	}

	// Track if quality gates passed for self-review decision (GH-1079)
	// Run quality gates if configured.
	// Previously skipped in LocalMode (v25 OOM concern), re-enabled since
	// deps are now pre-installed and gate runs pytest only (bounded cost).
	if r.qualityCheckerFactory != nil {
		const maxAutoRetries = 2 // Circuit breaker to prevent infinite loops

		// Track quality gate results across retries (GH-209)
		var finalOutcome *QualityOutcome
		var totalQualityRetries int

		for retryAttempt := 0; retryAttempt <= maxAutoRetries; retryAttempt++ {
			r.reportProgress(task.ID, "Quality Gates", 91, "Running quality checks...")
			r.saveLogEntry(task.ID, "info", "Running tests...")

			checker := r.qualityCheckerFactory(task.ID, executionPath)
			outcome, qErr := checker.Check(ctx)
			if qErr != nil {
				log.Error("Quality gate check error", slog.Any("error", qErr))
				result.Success = false
				result.Error = fmt.Sprintf("quality gate error: %v", qErr)
				r.reportProgress(task.ID, "Quality Failed", 100, result.Error)

				return r.failQualityGates(s, AlertEventTypeTaskFailed, nil, "Quality Gates")
			}

			// Quality gates passed - exit retry loop
			if outcome.Passed {
				finalOutcome = outcome
				s.qualityGatesPassed = true
				r.reportProgress(task.ID, "Quality Passed", 94, "All quality gates passed")

				// Run simplification phase if enabled (GH-995)
				if r.config != nil && r.config.Simplification != nil && r.config.Simplification.Enabled {
					r.reportProgress(task.ID, "Simplifying", 95, "Simplifying code...")
					simplified, simplifyErr := SimplifyModifiedFiles(executionPath, r.config.Simplification)
					if simplifyErr != nil {
						log.Warn("Simplification error", slog.Any("error", simplifyErr))
						// Continue anyway - simplification is advisory
					} else if len(simplified) > 0 {
						log.Info("Simplified files", slog.Int("count", len(simplified)), slog.Any("files", simplified))
					}
				}

				// Note: Self-review now runs in parallel with intent judge after quality gates (GH-1079)

				break
			}
			// Track this outcome for potential failure reporting
			finalOutcome = outcome

			// Quality gates failed
			log.Warn("Quality gates failed",
				slog.Bool("should_retry", outcome.ShouldRetry),
				slog.Int("attempt", outcome.Attempt),
				slog.Int("retry_attempt", retryAttempt),
			)

			// Check if we should retry with Claude Code
			if outcome.ShouldRetry && retryAttempt < maxAutoRetries {
				totalQualityRetries++ // Track total retries across all gates (GH-209)
				r.reportProgress(task.ID, "Quality Retry", 92,
					fmt.Sprintf("Fixing issues (attempt %d/%d)...", retryAttempt+1, maxAutoRetries))

				// GH-1066: Record correction for drift detection
				if r.driftDetector != nil {
					r.driftDetector.RecordCorrection("quality_gate_retry", fmt.Sprintf("Quality gate failure: %s, Retry attempt: %d", outcome.RetryFeedback, retryAttempt+1))
				}

				// Emit retry event
				r.emitAlertEvent(AlertEvent{
					Type:      AlertEventTypeTaskRetry,
					TaskID:    task.ID,
					TaskTitle: task.Title,
					Project:   task.ProjectPath,
					Metadata: map[string]string{
						"attempt":  strconv.Itoa(retryAttempt + 1),
						"feedback": truncateText(outcome.RetryFeedback, 500),
					},
					Timestamp: time.Now(),
				})

				// Build retry prompt with feedback
				retryPrompt := r.buildRetryPrompt(task, outcome.RetryFeedback, retryAttempt+1)

				log.Info("Re-invoking Claude Code with retry feedback",
					slog.String("task_id", task.ID),
					slog.Int("retry_attempt", retryAttempt+1),
				)

				// Re-invoke backend with retry prompt
				feedbackAllowed, feedbackMCP := r.executionToolOptions()
				retryResult, retryErr := r.execBackend.Execute(ctx, ExecuteOptions{
					Prompt:        retryPrompt,
					ProjectPath:   executionPath, // retry in the worktree, not the original repo path (matches no-commit retry, GH-936)
					Verbose:       task.Verbose,
					Model:         selectedModel,
					Effort:        selectedEffort,
					AllowedTools:  feedbackAllowed,
					MCPConfigPath: feedbackMCP,
					EventHandler: func(event BackendEvent) {
						if recorder != nil {
							if recErr := recorder.RecordEvent(event.Raw); recErr != nil {
								log.Warn("Failed to record retry event", slog.Any("error", recErr))
							}
						}
						r.processBackendEvent(task.ID, event, state)
					},
				})

				if retryErr != nil {
					result.Success = false

					// GH-917: Check for classified backend error types in retry
					alertType := AlertEventTypeTaskFailed
					errorCategory := "unknown"

					if beErr, ok := retryErr.(BackendError); ok {
						result.Error = fmt.Sprintf("retry execution failed: %v", beErr)

						switch beErr.ErrorType() {
						case "rate_limit":
							alertType = AlertEventTypeRateLimit
							errorCategory = "rate_limit"
							log.Warn("Retry hit rate limit",
								slog.String("task_id", task.ID),
								slog.Int("retry_attempt", retryAttempt+1),
							)
							r.reportProgress(task.ID, "Rate Limited", 100, "Retry hit rate limit")
						case "invalid_config":
							alertType = AlertEventTypeConfigError
							errorCategory = "invalid_config"
							log.Error("Retry failed: invalid config", slog.String("message", beErr.ErrorMessage()))
							r.reportProgress(task.ID, "Config Error", 100, beErr.ErrorMessage())
						case "api_error":
							alertType = AlertEventTypeAPIError
							errorCategory = "api_error"
							log.Error("Retry failed: API error", slog.String("message", beErr.ErrorMessage()))
							r.reportProgress(task.ID, "API Error", 100, beErr.ErrorMessage())
						case "oom_killed":
							// GH-2332: surface OOM kills distinctly in the retry path too.
							alertType = AlertEventTypeOOMKilled
							errorCategory = "oom_killed"
							log.Error("Retry failed: OOM-killed", slog.String("message", beErr.ErrorMessage()))
							r.reportProgress(task.ID, "OOM Killed", 100, beErr.ErrorMessage())
						default:
							log.Error("Retry execution failed", slog.Any("error", retryErr))
							r.reportProgress(task.ID, "Retry Failed", 100, result.Error)
						}
					} else {
						result.Error = fmt.Sprintf("retry execution failed: %v", retryErr)
						log.Error("Retry execution failed", slog.Any("error", retryErr))
						r.reportProgress(task.ID, "Retry Failed", 100, result.Error)
					}

					return r.failQualityGates(s, alertType, map[string]string{
						"error_category": errorCategory,
						"phase":          "quality_retry",
					}, "Quality Retry")
				}

				// Update result with retry execution stats
				result.TokensInput += retryResult.TokensInput
				result.TokensOutput += retryResult.TokensOutput
				result.TokensTotal = result.TokensInput + result.TokensOutput
				if retryResult.Model != "" {
					result.ModelName = retryResult.Model
				}

				// Extract new commit SHA if any
				if len(state.commitSHAs) > 0 {
					result.CommitSHA = state.commitSHAs[len(state.commitSHAs)-1]
				}

				// Continue to next iteration to re-check quality gates
				r.reportProgress(task.ID, "Re-testing", 93, "Re-running quality gates...")
				continue
			}

			// No more retries allowed - fail the task
			result.Success = false
			if retryAttempt >= maxAutoRetries {
				result.Error = fmt.Sprintf("quality gates failed after %d auto-retries", maxAutoRetries)
			} else {
				result.Error = "quality gates failed, max retries exhausted"
			}

			r.reportProgress(task.ID, "Quality Failed", 100, "Quality gates did not pass")

			return r.failQualityGates(s, AlertEventTypeTaskFailed, nil, "Quality Gates")
		}

		// Populate quality gate results in ExecutionResult (GH-209)
		if finalOutcome != nil {
			result.QualityGates = r.buildQualityGatesResult(finalOutcome, totalQualityRetries)
		}
	}

	return nil, nil
}

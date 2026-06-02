package executor

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

// executeSelfReviewIntent runs the finalizing progress update plus self-review
// and the intent judge in parallel, with intent-alignment retry (GH-1079,
// original lines ~1928-2120). It never early-returns; it returns (nil, nil) to
// continue.
func (r *Runner) executeSelfReviewIntent(s *executeState) (*ExecutionResult, error) {
	task := s.task
	ctx := s.ctx
	log := s.log
	git := s.git
	result := s.result
	state := s.state
	selectedModel := s.selectedModel
	selectedEffort := s.selectedEffort
	agentPath := s.agentPath

	r.reportProgress(task.ID, "Finalizing", 95, "Preparing for completion")

	// Warn if PR creation requested but quality gates not configured (GH-248)
	if task.CreatePR && r.qualityCheckerFactory == nil {
		log.Warn("quality gates not configured - PR created without local validation",
			slog.String("task_id", task.ID),
			slog.String("project", task.ProjectPath),
		)
	}

	// GH-1079: Run self-review and intent judge in parallel (saves 2-5 min per task)
	// Both are independent read-only operations:
	// - Self-review checks code quality (syntax, wiring, style)
	// - Intent judge verifies diff matches issue intent
	var intentVerdict *JudgeVerdict
	var intentErr error
	var intentDiff string
	var intentBaseBranch string

	// Determine if intent judge should run
	runIntentJudge := r.intentJudge != nil && task.CreatePR && !task.DirectCommit && task.Branch != ""

	// Log skip reasons for intent judge
	if r.intentJudge == nil {
		log.Debug("Intent judge skipped: not initialized")
	} else if !task.CreatePR {
		log.Debug("Intent judge skipped: CreatePR=false")
	} else if task.DirectCommit {
		log.Debug("Intent judge skipped: DirectCommit=true")
	} else if task.Branch == "" {
		log.Debug("Intent judge skipped: no branch")
	}

	// Get diff before parallel execution (needed for intent judge)
	if runIntentJudge {
		intentBaseBranch = task.BaseBranch
		if intentBaseBranch == "" {
			intentBaseBranch, _ = git.GetDefaultBranch(ctx)
			if intentBaseBranch == "" {
				intentBaseBranch = "main"
			}
		}
		intentDiff, intentErr = git.GetDiff(ctx, intentBaseBranch)
		if intentErr != nil {
			log.Warn("Intent judge skipped: failed to get diff",
				slog.String("task_id", task.ID),
				slog.Any("error", intentErr),
			)
			runIntentJudge = false
		} else if intentDiff == "" {
			runIntentJudge = false
		}
	}

	// Determine if self-review should run:
	// 1. Quality gates configured AND passed
	// 2. OR quality gates not configured AND CreatePR=true (GH-364)
	runSelfReview := s.qualityGatesPassed || (r.qualityCheckerFactory == nil && task.CreatePR)

	// Run self-review and intent judge in parallel
	var wg sync.WaitGroup
	var selfReviewErr error

	if runSelfReview {
		r.saveLogEntry(task.ID, "info", "Running self-review...")
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.runSelfReview(ctx, task, state); err != nil {
				selfReviewErr = err
			}
		}()
	}

	if runIntentJudge {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Info("Intent judge running",
				slog.String("task_id", task.ID),
				slog.Int("diff_len", len(intentDiff)),
			)
			r.reportProgress(task.ID, "Intent Check", 96, "Verifying diff matches intent...")
			intentVerdict, intentErr = r.intentJudge.Judge(ctx, task.Title, task.Description, intentDiff)
		}()
	}

	wg.Wait()

	// Handle self-review result
	if runSelfReview && selfReviewErr != nil {
		log.Warn("Self-review error", slog.Any("error", selfReviewErr))
		// Continue anyway - self-review is advisory
	}

	// Handle intent judge result
	if runIntentJudge {
		if intentErr != nil {
			log.Warn("Intent judge error (continuing to PR)",
				slog.String("task_id", task.ID),
				slog.Any("error", intentErr),
			)
		} else if intentVerdict != nil && !intentVerdict.Passed {
			log.Warn("Intent judge vetoed diff",
				slog.String("task_id", task.ID),
				slog.String("reason", intentVerdict.Reason),
				slog.Float64("confidence", intentVerdict.Confidence),
			)

			if !state.intentRetried {
				state.intentRetried = true
				r.reportProgress(task.ID, "Intent Retry", 80, "Retrying with intent feedback...")

				// Re-anchor the retry on the original acceptance criteria so the
				// fix is judged against the same target as the first attempt
				// instead of drifting toward the veto reason alone.
				var acSection string
				if len(task.AcceptanceCriteria) > 0 {
					var acb strings.Builder
					acb.WriteString("\n\n## Acceptance Criteria\n\n")
					for _, ac := range task.AcceptanceCriteria {
						acb.WriteString(fmt.Sprintf("- [ ] %s\n", ac))
					}
					acSection = acb.String()
				}

				// Re-inject persisted constraints/decisions from the run doc so
				// the retry stays anchored to context captured earlier in the run.
				var docSection string
				if runDoc, docErr := loadRunDoc(agentPath, task.ID); docErr == nil && runDoc != nil {
					var db strings.Builder
					if len(runDoc.Constraints) > 0 {
						db.WriteString("\n\n## Constraints\n\n")
						for _, c := range runDoc.Constraints {
							db.WriteString(fmt.Sprintf("- %s\n", c))
						}
					}
					if len(runDoc.DecisionLog) > 0 {
						db.WriteString("\n\n## Prior Decisions\n\n")
						for _, d := range runDoc.DecisionLog {
							db.WriteString(fmt.Sprintf("- %s — %s\n", d.Decision, d.Reasoning))
						}
					}
					docSection = db.String()
				}

				retryPrompt := fmt.Sprintf(
					"## Intent Alignment Retry\n\nThe intent judge flagged the previous implementation:\n\n**Reason:** %s\n\nPlease fix the issues above. Focus on implementing exactly what the issue asks for.\n\n## Original Task: %s\n\n%s%s%s",
					intentVerdict.Reason, task.Title, task.Description, acSection, docSection,
				)

				intentAllowed, intentMCP := r.executionToolOptions()
				_, retryErr := r.backend.Execute(ctx, ExecuteOptions{
					Prompt:        retryPrompt,
					ProjectPath:   task.ProjectPath,
					Verbose:       task.Verbose,
					Model:         selectedModel,
					Effort:        selectedEffort,
					AllowedTools:  intentAllowed,
					MCPConfigPath: intentMCP,
					EventHandler: func(event BackendEvent) {
						state.tokensInput += event.TokensInput
						state.tokensOutput += event.TokensOutput
						state.cacheCreationInputTokens += event.CacheCreationInputTokens
						state.cacheReadInputTokens += event.CacheReadInputTokens
						if event.Type == EventTypeToolResult && event.ToolResult != "" {
							extractCommitSHA(event.ToolResult, state)
						}
					},
				})

				if retryErr == nil {
					// Update result tokens
					result.TokensInput = state.tokensInput
					result.TokensOutput = state.tokensOutput
					result.TokensTotal = state.tokensInput + state.tokensOutput

					// Re-judge the new diff
					newDiff, _ := git.GetDiff(ctx, intentBaseBranch)
					if newDiff != "" {
						v2, _ := r.intentJudge.Judge(ctx, task.Title, task.Description, newDiff)
						if v2 != nil && !v2.Passed {
							result.IntentWarning = v2.Reason
						}
					}
				} else {
					result.IntentWarning = intentVerdict.Reason
				}
			} else {
				result.IntentWarning = intentVerdict.Reason
			}
		}
	}

	return nil, nil
}

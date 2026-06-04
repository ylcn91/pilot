package executor

import (
	"log/slog"
)

// executeCopyResult copies the backend result into the execution result, tracks
// research tokens, recovers/guards the commit SHA, fills metrics from state, and
// emits Prometheus token/cost/outcome counters (original lines ~1207-1340).
//
// This runs after the inline retrySucceeded label. It only mutates state and
// never early-returns.
func (r *Runner) executeCopyResult(s *executeState) {
	task := s.task
	ctx := s.ctx
	log := s.log
	git := s.git
	executionPath := s.executionPath
	result := s.result
	backendResult := s.backendResult
	researchResult := s.researchResult
	state := s.state

	// Copy backend result to execution result
	result.Success = backendResult.Success
	result.Output = backendResult.Output
	result.Error = backendResult.Error
	result.TokensInput = backendResult.TokensInput
	result.TokensOutput = backendResult.TokensOutput
	result.TokensTotal = backendResult.TokensInput + backendResult.TokensOutput
	result.ModelName = backendResult.Model
	// GH-3028: propagate RSS telemetry from backend to execution result.
	result.PeakRSSMB = backendResult.PeakRSSMB
	result.FinalRSSMB = backendResult.FinalRSSMB

	// Track research phase tokens (GH-217)
	if researchResult != nil {
		result.ResearchTokens = researchResult.TotalTokens
		result.TokensTotal += researchResult.TotalTokens
	}

	// Extract commit SHA from state (parsed from Claude Code output)
	if len(state.commitSHAs) > 0 {
		result.CommitSHA = state.commitSHAs[len(state.commitSHAs)-1] // Use last commit
	}

	// Post-execution summary via structured output (GH-1264)
	// This replaces brittle regex parsing with reliable --json-schema output
	if result.CommitSHA == "" && result.Success && r.config != nil && r.config.ClaudeCode != nil && r.config.ClaudeCode.UseStructuredOutput {
		if summary, summaryErr := r.getPostExecutionSummary(ctx, executionPath); summaryErr == nil {
			if summary.CommitSHA != "" {
				result.CommitSHA = summary.CommitSHA
				log.Info("CommitSHA extracted via post-execution summary",
					slog.String("task_id", task.ID),
					slog.String("sha", summary.CommitSHA[:min(7, len(summary.CommitSHA))]),
					slog.String("branch", summary.BranchName),
				)
			}
		} else {
			log.Debug("post-execution summary failed, falling back to git",
				slog.String("task_id", task.ID),
				slog.Any("error", summaryErr),
			)
		}
	}

	// Fallback: if output parsing missed the commit SHA, ask git directly.
	// This handles cases where Claude's git commit output format doesn't match
	// the extractCommitSHA() pattern (e.g. different flags, localized output).
	if result.CommitSHA == "" && task.Branch != "" && result.Success {
		baseBranch := task.BaseBranch
		if baseBranch == "" {
			baseBranch, _ = git.GetDefaultBranch(ctx)
			if baseBranch == "" {
				baseBranch = "main"
			}
		}
		if commitCount, countErr := git.CountNewCommits(ctx, baseBranch); countErr == nil && commitCount > 0 {
			if sha, shaErr := git.GetCurrentCommitSHA(ctx); shaErr == nil && sha != "" {
				log.Info("CommitSHA recovered via git (output parsing missed it)",
					slog.String("task_id", task.ID),
					slog.String("sha", sha[:min(7, len(sha))]),
					slog.Int("new_commits", commitCount),
				)
				result.CommitSHA = sha
			}
		}
	}

	// GH-3126: Ghost-SHA guard — reject SHAs that are already on the base branch.
	// When Claude makes no new commit, git log returns the parent (pre-execution) SHA.
	// Recording that as CommitSHA causes IsTaskShipped to return true on a no-op run,
	// triggering pilot-done + issue close with no actual work delivered.
	// Fail open on check errors (e.g. no origin configured in test repos): only reject
	// when the check conclusively shows the SHA is already on origin/<base>.
	if result.CommitSHA != "" && result.Success {
		ghostBase := task.BaseBranch
		if ghostBase == "" {
			ghostBase, _ = git.GetDefaultBranch(ctx)
			if ghostBase == "" {
				ghostBase = "main"
			}
		}
		if isNew, checkErr := commitSHAIsNew(ctx, executionPath, result.CommitSHA, ghostBase); checkErr != nil {
			log.Warn("executor: ghost-SHA check skipped (will not block)",
				slog.String("task_id", task.ID),
				slog.String("sha", result.CommitSHA[:min(7, len(result.CommitSHA))]),
				slog.Any("error", checkErr),
			)
		} else if !isNew {
			log.Warn("executor: harvested SHA is already on base branch — no new commit",
				slog.String("task_id", task.ID),
				slog.String("sha", result.CommitSHA[:min(7, len(result.CommitSHA))]),
				slog.String("base", ghostBase),
			)
			result.CommitSHA = ""
			result.Success = false
			result.Error = "no new commit produced — worktree HEAD matches base branch parent"
		}
	}

	// Fill in additional metrics from state
	result.FilesChanged = state.filesWrite
	result.CacheCreationInputTokens = state.cacheCreationInputTokens
	result.CacheReadInputTokens = state.cacheReadInputTokens
	if result.ModelName == "" {
		result.ModelName = state.modelName
	}
	if result.ModelName == "" {
		// GH-2428: derive from config (DefaultModel/OpenCode.Model/backend type)
		// instead of hardcoding "claude-opus-4-6". The hardcoded value was stale
		// (Claude Code reports 4-7) and silently labelled OpenCode/GLM runs as
		// Claude Opus, biasing model-outcome metrics.
		result.ModelName = r.fallbackModelName()
	}
	// Estimate cost based on token usage (including research tokens) with cache-aware pricing (GH-2164)
	result.EstimatedCostUSD = estimateCostWithCache(result.TokensInput+result.ResearchTokens, result.TokensOutput, result.CacheCreationInputTokens, result.CacheReadInputTokens, result.ModelName)

	// Emit Prometheus counters for token usage, cost, and execution outcome (GH-2855).
	if r.metricsRecorder != nil {
		model := result.ModelName
		r.metricsRecorder.RecordTokens(model, "input", result.TokensInput+result.ResearchTokens)
		r.metricsRecorder.RecordTokens(model, "output", result.TokensOutput)
		if result.CacheCreationInputTokens > 0 {
			r.metricsRecorder.RecordTokens(model, "cache_creation", result.CacheCreationInputTokens)
		}
		if result.CacheReadInputTokens > 0 {
			r.metricsRecorder.RecordTokens(model, "cache_read", result.CacheReadInputTokens)
		}
		r.metricsRecorder.RecordCost(model, result.EstimatedCostUSD)
		outcomeLabel := "success"
		if !result.Success {
			outcomeLabel = "failed"
		}
		r.metricsRecorder.RecordExecution(model, outcomeLabel)
	}
}

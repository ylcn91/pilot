package executor

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/ylcn91/pilot/internal/memory"
)

// recordLearning records the execution outcome for pattern learning.
// It is non-fatal — errors are logged but do not affect the execution result.
func (r *Runner) recordLearning(ctx context.Context, task *Task, result *ExecutionResult) {
	if r.learningLoop == nil {
		return
	}
	statusStr := "completed"
	if result.Declined {
		statusStr = "declined"
	} else if !result.Success {
		// Distinguish stalled from generic failure for pattern learning.
		if strings.Contains(result.Error, "session stalled") {
			statusStr = "stalled"
		} else {
			statusStr = "failed"
		}
	}
	exec := &memory.Execution{
		ID:           task.ID,
		TaskID:       task.ID,
		ProjectPath:  task.ProjectPath,
		Status:       statusStr,
		Output:       result.Output,
		Error:        result.Error,
		DurationMs:   result.Duration.Milliseconds(),
		PRUrl:        result.PRUrl,
		CommitSHA:    result.CommitSHA,
		TokensInput:  result.TokensInput,
		TokensOutput: result.TokensOutput,
		FilesChanged: result.FilesChanged,
		ModelName:    result.ModelName,
	}
	if learnErr := r.learningLoop.RecordExecution(ctx, exec, nil); learnErr != nil {
		r.log.Warn("Failed to record execution for learning", slog.Any("error", learnErr))
	}

	// GH-2021: Record per-pattern outcome for contextual confidence tracking
	r.recordPatternOutcomes(task, result)
}

// recordPatternOutcomes records success/failure for each pattern that was applied
// to this task's project+type context. Uses logStore if available.
func (r *Runner) recordPatternOutcomes(task *Task, result *ExecutionResult) {
	if r.logStore == nil {
		return
	}
	taskType := inferTaskType(task)
	model := result.ModelName
	if model == "" {
		model = "claude-opus-4-6"
	}

	// GH-2021/B3: Credit only the patterns actually injected into this task's
	// prompt, not every pattern linked to the project. PatternContext recorded
	// the injected IDs at prompt-build time; fall back to all project patterns
	// when no injection record exists (e.g. pattern injection was disabled).
	var ids []string
	if r.patternContext != nil {
		ids = r.patternContext.AppliedPatterns(task.ProjectPath, taskType, task.Description)
	}
	if len(ids) == 0 {
		patterns, err := r.logStore.GetCrossPatternsForProject(task.ProjectPath, false)
		if err != nil {
			r.log.Warn("Failed to get patterns for outcome recording", slog.Any("error", err))
			return
		}
		ids = make([]string, 0, len(patterns))
		for _, p := range patterns {
			ids = append(ids, p.ID)
		}
	}

	for _, id := range ids {
		if recErr := r.logStore.RecordPatternOutcome(id, task.ProjectPath, taskType, model, result.Success); recErr != nil {
			r.log.Warn("Failed to record pattern outcome",
				slog.String("pattern_id", id),
				slog.Any("error", recErr),
			)
		}
	}
}

// recordGraphLearning records the execution into the knowledge graph (GH-2015).
// It is non-fatal — errors are logged but do not affect the execution result.
func (r *Runner) recordGraphLearning(task *Task, result *ExecutionResult) {
	if r.knowledgeGraph == nil {
		return
	}
	outcome := "success"
	if !result.Success {
		outcome = "failure"
	}
	// Extract simple patterns from task context
	patterns := extractLearningPatterns(task)
	content := task.Description
	if len(content) > 500 {
		content = content[:500]
	}
	if err := r.knowledgeGraph.AddExecutionLearning(task.Title, content, nil, patterns, outcome); err != nil {
		r.log.Warn("Failed to record graph learning", slog.Any("error", err))
	}
}

// extractLearningPatterns extracts simple pattern hints from a task's title and description.
func extractLearningPatterns(task *Task) []string {
	combined := strings.ToLower(task.Title + " " + task.Description)
	candidates := []string{
		"refactor", "test", "fix", "feature", "api", "database",
		"auth", "webhook", "migration", "config", "ci", "lint",
	}
	var found []string
	for _, c := range candidates {
		if strings.Contains(combined, c) {
			found = append(found, c)
		}
	}
	return found
}

// recordOutcome records the model execution outcome for escalation tracking (GH-1991).
func (r *Runner) recordOutcome(task *Task, result *ExecutionResult, complexity Complexity, duration time.Duration) {
	if r.outcomeTracker == nil {
		return
	}
	outcome := "success"
	if !result.Success {
		outcome = "failure"
	}
	tokens := int(result.TokensInput + result.TokensOutput)
	if err := r.outcomeTracker.RecordOutcome(string(complexity), result.ModelName, outcome, tokens, duration); err != nil {
		r.log.Warn("Failed to record model outcome", slog.Any("error", err))
	}
}

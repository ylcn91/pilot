package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/ylcn91/pilot/internal/executor/workflow"
	"github.com/ylcn91/pilot/internal/quality"
)

// buildQualityGatesResult converts QualityOutcome to QualityGatesResult for ExecutionResult (GH-209)
func (r *Runner) buildQualityGatesResult(outcome *QualityOutcome, totalRetries int) *QualityGatesResult {
	if outcome == nil {
		return nil
	}

	qgResult := &QualityGatesResult{
		Enabled:       true,
		AllPassed:     outcome.Passed,
		TotalDuration: outcome.TotalDuration,
		TotalRetries:  totalRetries,
		Gates:         make([]QualityGateResult, len(outcome.GateDetails)),
	}

	for i, detail := range outcome.GateDetails {
		qgResult.Gates[i] = QualityGateResult(detail)
	}

	return qgResult
}

// simpleQualityChecker is a minimal quality checker for auto-enabled build gates (GH-363).
// Used when quality gates aren't explicitly configured but we still want basic build verification.
type simpleQualityChecker struct {
	config      *quality.Config
	projectPath string
	taskID      string
}

// Check runs the build gate and returns the outcome.
func (c *simpleQualityChecker) Check(ctx context.Context) (*QualityOutcome, error) {
	runner := quality.NewRunner(c.config, c.projectPath)

	results, err := runner.RunAll(ctx, c.taskID)
	if err != nil {
		return nil, err
	}

	// Convert to QualityOutcome
	outcome := &QualityOutcome{
		Passed:        results.AllPassed,
		ShouldRetry:   !results.AllPassed && c.config.OnFailure.Action == quality.ActionRetry,
		TotalDuration: results.TotalTime,
		GateDetails:   quality.GateDetails(results),
	}

	// Build retry feedback if failed
	if !results.AllPassed {
		outcome.RetryFeedback = quality.FormatErrorFeedback(results)
	}

	return outcome, nil
}

// PostExecutionSummary contains git state information extracted via structured output
type PostExecutionSummary struct {
	BranchName   string   `json:"branch_name"`
	CommitSHA    string   `json:"commit_sha"`
	FilesChanged []string `json:"files_changed"`
	Summary      string   `json:"summary"`
}

// getPostExecutionSummary runs a structured output query to extract git state information.
// This replaces brittle regex parsing of git output with reliable --json-schema extraction.
func (r *Runner) getPostExecutionSummary(ctx context.Context) (*PostExecutionSummary, error) {
	if r.config == nil || r.config.ClaudeCode == nil {
		return nil, fmt.Errorf("claude code backend not configured")
	}

	prompt := "Report git state: run 'git log --oneline -1' and 'git branch --show-current' and 'git diff --name-only HEAD~1'. Return branch name, latest commit SHA, and changed files."

	// Use fast Haiku model for this simple task
	claudeCmd := "claude"
	if r.config.ClaudeCode.Command != "" {
		claudeCmd = r.config.ClaudeCode.Command
	}
	cmd := exec.CommandContext(ctx, claudeCmd,
		"--print",
		"-p", prompt,
		"--model", r.config.ResolveModel("claude-haiku-4-5-20251001"),
		"--output-format", "json",
		"--json-schema", PostExecutionSummarySchema,
	)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("claude command failed: %w", err)
	}

	structuredOutput, err := extractStructuredOutput(output)
	if err != nil {
		return nil, fmt.Errorf("extract structured output: %w", err)
	}

	var summary PostExecutionSummary
	if err := json.Unmarshal(structuredOutput, &summary); err != nil {
		return nil, fmt.Errorf("parse post-execution summary: %w", err)
	}

	return &summary, nil
}

// runWorkflowHook executes a workflow lifecycle hook, logging output and warning on failure.
// It is a no-op when scripts is empty.
func runWorkflowHook(ctx context.Context, name string, scripts workflow.HookValue, dir string, env []string, log *slog.Logger) {
	if len(scripts) == 0 {
		return
	}
	logFn := func(output string) {
		log.Info("hook output", slog.String("hook", name), slog.String("output", strings.TrimSpace(output)))
	}
	if err := workflow.RunHook(ctx, name, scripts, dir, env, 0, logFn); err != nil {
		log.Warn("workflow hook failed", slog.String("hook", name), slog.Any("error", err))
	}
}

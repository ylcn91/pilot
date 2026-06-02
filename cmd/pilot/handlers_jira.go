package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ylcn91/pilot/internal/adapters/jira"
	"github.com/ylcn91/pilot/internal/alerts"
	"github.com/ylcn91/pilot/internal/budget"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/logging"
)

// handleJiraIssueWithResult processes a Jira issue picked up by the poller (GH-905)
func handleJiraIssueWithResult(ctx context.Context, cfg *config.Config, client *jira.Client, issue *jira.Issue, projectPath string, dispatcher *executor.Dispatcher, runner *executor.Runner, monitor *executor.Monitor, program *tea.Program, alertsEngine *alerts.Engine, enforcer *budget.Enforcer) (*jira.IssueResult, error) {
	taskID := issue.Key // e.g., "PROJ-123"

	taskDesc := fmt.Sprintf("Jira Issue %s: %s\n\n%s", issue.Key, issue.Fields.Summary, issue.Fields.Description)
	branchName := fmt.Sprintf("pilot/%s", taskID)

	task := &executor.Task{
		ID:          taskID,
		Title:       issue.Fields.Summary,
		Description: taskDesc,
		ProjectPath: projectPath,
		Branch:      branchName,
		CreatePR:    true,
		BaseBranch:  resolveProjectBaseBranch(cfg, projectPath), // GH-2290
	}

	deps := HandlerDeps{
		Cfg:          cfg,
		Dispatcher:   dispatcher,
		Runner:       runner,
		Monitor:      monitor,
		Program:      program,
		AlertsEngine: alertsEngine,
		Enforcer:     enforcer,
		ProjectPath:  projectPath,
	}
	info := IssueInfo{
		TaskID:   taskID,
		Title:    issue.Fields.Summary,
		URL:      fmt.Sprintf("%s/browse/%s", cfg.Adapters.Jira.BaseURL, issue.Key),
		Adapter:  "jira",
		LogEmoji: "📊",
	}

	hr, execErr := handleIssueGeneric(ctx, deps, info, task)

	// Build issue result
	issueResult := &jira.IssueResult{
		Success:    hr.Success,
		BranchName: hr.BranchName, // GH-1399: always set branch for autopilot wiring
		PRNumber:   hr.PRNumber,
		PRURL:      hr.PRURL,
		HeadSHA:    hr.HeadSHA, // GH-1399: for autopilot CI monitoring
		Error:      hr.Error,
	}

	// Post-execution: add comment (plain text format), transition issue via explicit ID or name lookup
	if execErr != nil {
		comment := fmt.Sprintf("❌ Pilot execution failed:\n\n%s", execErr.Error())
		if _, err := client.AddComment(ctx, issue.Key, comment); err != nil {
			logging.WithComponent("jira").Warn("Failed to add comment",
				slog.String("issue", issue.Key),
				slog.Any("error", err),
			)
		}
	} else if hr.Result != nil && hr.Result.Success {
		// Validate deliverables before marking as done
		if !hr.Result.IsEpic && hr.Result.CommitSHA == "" && hr.Result.PRUrl == "" { // GH-3053
			comment := fmt.Sprintf("⚠️ Pilot execution completed but no changes were made.\n\nDuration: %s\nBranch: %s\n\nNo commits or PR were created. The task may need clarification or manual intervention.",
				hr.Result.Duration, branchName)
			if _, err := client.AddComment(ctx, issue.Key, comment); err != nil {
				logging.WithComponent("jira").Warn("Failed to add comment",
					slog.String("issue", issue.Key),
					slog.Any("error", err),
				)
			}
			issueResult.Success = false
		} else {
			comment := buildJiraExecutionComment(hr.Result, branchName)
			if _, err := client.AddComment(ctx, issue.Key, comment); err != nil {
				logging.WithComponent("jira").Warn("Failed to add comment",
					slog.String("issue", issue.Key),
					slog.Any("error", err),
				)
			}

			// GH-1403: Best-effort state transition to Done
			// Check config for explicit transition ID, fall back to name-based lookup
			if cfg.Adapters.Jira.Transitions.Done != "" {
				if err := client.TransitionIssue(ctx, issue.Key, cfg.Adapters.Jira.Transitions.Done); err != nil {
					logging.WithComponent("jira").Warn("failed to transition issue to done state (explicit ID)",
						slog.String("issue", issue.Key),
						slog.String("transition_id", cfg.Adapters.Jira.Transitions.Done),
						slog.Any("error", err),
					)
				}
			} else {
				if err := client.TransitionIssueTo(ctx, issue.Key, "Done"); err != nil {
					logging.WithComponent("jira").Warn("failed to transition issue to done state (name lookup)",
						slog.String("issue", issue.Key),
						slog.Any("error", err),
					)
				}
			}
		}
	} else if hr.Result != nil {
		comment := buildJiraFailureComment(hr.Result)
		if _, err := client.AddComment(ctx, issue.Key, comment); err != nil {
			logging.WithComponent("jira").Warn("Failed to add comment",
				slog.String("issue", issue.Key),
				slog.Any("error", err),
			)
		}
	}

	return issueResult, execErr
}

// buildJiraExecutionComment creates a comment for successful Jira execution
func buildJiraExecutionComment(result *executor.ExecutionResult, branchName string) string {
	var parts []string
	parts = append(parts, "✅ Pilot execution completed successfully!")
	parts = append(parts, "")

	if result.PRUrl != "" {
		parts = append(parts, fmt.Sprintf("Pull Request: %s", result.PRUrl))
	}
	if result.CommitSHA != "" {
		parts = append(parts, fmt.Sprintf("Commit: %s", result.CommitSHA[:min(8, len(result.CommitSHA))]))
	}
	parts = append(parts, fmt.Sprintf("Branch: %s", branchName))
	parts = append(parts, fmt.Sprintf("Duration: %s", result.Duration))

	return strings.Join(parts, "\n")
}

// buildJiraFailureComment creates a comment for failed Jira execution
func buildJiraFailureComment(result *executor.ExecutionResult) string {
	var parts []string
	parts = append(parts, "❌ Pilot execution failed")
	parts = append(parts, "")
	if result.Error != "" {
		parts = append(parts, fmt.Sprintf("Error: %s", result.Error))
	}
	if result.Duration > 0 {
		parts = append(parts, fmt.Sprintf("Duration: %s", result.Duration))
	}
	return strings.Join(parts, "\n")
}

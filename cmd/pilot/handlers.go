package main

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/logging"
	"github.com/ylcn91/pilot/internal/teams"
)

// syncBoardStatus updates a GitHub Projects V2 board column for an issue.
// It is a no-op when boardSync is nil or status is empty. Errors are logged, never propagated.
func syncBoardStatus(ctx context.Context, boardSync *github.ProjectBoardSync, nodeID string, status string) {
	if boardSync == nil || status == "" {
		return
	}
	if err := boardSync.UpdateProjectItemStatus(ctx, nodeID, status); err != nil {
		slog.Warn("board sync failed", "status", status, "error", err)
	}
}

// logGitHubAPIError logs a warning when a GitHub API call fails.
func logGitHubAPIError(operation string, owner, repo string, issueNum int, err error) {
	if err != nil {
		logging.WithComponent("github").Warn("GitHub API call failed",
			slog.String("operation", operation),
			slog.String("repo", owner+"/"+repo),
			slog.Int("issue", issueNum),
			slog.Any("error", err),
		)
	}
}

// noOpErrorMarker is the shared prefix of the executor's ghost-SHA guard errors
// ("no new commit produced — …", both the worktree-HEAD and post-push variants).
// TASK-321: used to recognize an ambiguous no-op that may actually be already-merged work.
const noOpErrorMarker = "no new commit produced"

// issueAlreadyMerged reports whether a merged PR already exists for the issue,
// using the same Search + branch-lookup strategy as the poller's pre-dispatch
// guard (Search API has ~30s indexing lag, so we supplement with a strongly-
// consistent branch lookup). Read-only — labeling/closing is the caller's job.
// TASK-321: distinguishes a re-dispatch of shipped work from a genuine no-op.
func issueAlreadyMerged(ctx context.Context, client *github.Client, owner, repo string, issueNumber int) bool {
	if found, err := client.SearchMergedPRsForIssue(ctx, owner, repo, issueNumber); err == nil && found {
		return true
	}
	branch := fmt.Sprintf("pilot/GH-%d", issueNumber)
	found, err := client.FindMergedPRByBranch(ctx, owner, repo, branch)
	return err == nil && found
}

// issueHasOpenPR reports whether an OPEN pilot PR already exists for the issue.
// Counterpart to issueAlreadyMerged for the awaiting-merge window: pilot-done +
// issue close are deferred to merge time (GH-3139/TASK-301), so between PR
// creation and merge a re-dispatch finds the work already on the pilot/GH-N
// branch and produces a "no new commit produced" no-op even though a healthy PR
// is open. TASK-341: used to classify that no-op as awaiting-merge rather than
// pilot-blocked. Branch lookup is strongly consistent (no Search API lag); the
// Search fallback catches PRs whose head deviates from the pilot/GH-N convention.
// Read-only.
func issueHasOpenPR(ctx context.Context, client *github.Client, owner, repo string, issueNumber int) bool {
	branch := fmt.Sprintf("pilot/GH-%d", issueNumber)
	if found, err := client.FindOpenPRByBranch(ctx, owner, repo, branch); err == nil && found {
		return true
	}
	prs, err := client.SearchPRsForIssue(ctx, owner, repo, issueNumber)
	if err != nil {
		return false
	}
	for _, pr := range prs {
		if pr.State == "open" && !pr.Merged {
			return true
		}
	}
	return false
}

// requestReviewersFromConfig looks up the project config for the given sourceRepo
// and requests PR reviewers if configured. Errors are logged but not propagated.
func requestReviewersFromConfig(ctx context.Context, cfg *config.Config, client *github.Client, sourceRepo, owner, repo string, prNumber int) {
	proj := cfg.FindProjectByRepo(sourceRepo)
	if proj == nil {
		return
	}
	if len(proj.Reviewers) == 0 && len(proj.TeamReviewers) == 0 {
		return
	}
	if err := client.RequestReviewers(ctx, owner, repo, prNumber, proj.Reviewers, proj.TeamReviewers); err != nil {
		logging.WithComponent("github").Warn("Failed to request PR reviewers",
			slog.String("repo", sourceRepo),
			slog.Int("pr", prNumber),
			slog.Any("reviewers", proj.Reviewers),
			slog.Any("team_reviewers", proj.TeamReviewers),
			slog.Any("error", err),
		)
	} else {
		slog.Info("PR reviewers requested",
			slog.String("repo", sourceRepo),
			slog.Int("pr", prNumber),
			slog.Any("reviewers", proj.Reviewers),
			slog.Any("team_reviewers", proj.TeamReviewers),
		)
	}
}

// resolveProjectBaseBranch returns the configured default/branch_from for the given
// project path, or "" when no project matches. Used by adapter handlers to honor
// `default_branch` / `branch_from` overrides (GH-2290).
func resolveProjectBaseBranch(cfg *config.Config, projectPath string) string {
	if cfg == nil {
		return ""
	}
	return cfg.FindProjectByPath(projectPath).ResolveBaseBranch()
}

// parseAutopilotBranch extracts the target branch from an autopilot-fix issue's metadata comment.
// Returns empty string if no metadata found.
// Supports both old format (branch:X) and new format (branch:X pr:N).
func parseAutopilotBranch(body string) string {
	re := regexp.MustCompile(`<!-- autopilot-meta branch:(\S+).*?-->`)
	if m := re.FindStringSubmatch(body); len(m) > 1 {
		return m[1]
	}
	return ""
}

// parseAutopilotPR extracts the PR number from an autopilot-fix issue's metadata comment.
// Returns 0 if no PR metadata found. Used for --from-pr session resumption (GH-1267).
func parseAutopilotPR(body string) int {
	re := regexp.MustCompile(`<!-- autopilot-meta.*?pr:(\d+).*?-->`)
	if m := re.FindStringSubmatch(body); len(m) > 1 {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

// parseAutopilotIteration extracts the CI fix iteration counter from an issue's metadata comment.
// Returns 0 if no iteration metadata found (GH-1566).
func parseAutopilotIteration(body string) int {
	re := regexp.MustCompile(`<!-- autopilot-meta.*?iteration:(\d+).*?-->`)
	if m := re.FindStringSubmatch(body); len(m) > 1 {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

// resolveGitHubMemberID maps a GitHub issue author to a team member ID (GH-634).
// The team adapter is threaded in from the gateway/polling runtime (nil when RBAC
// is not configured). Returns "" if no adapter is configured or no matching member
// is found — callers treat "" as "skip RBAC".
func resolveGitHubMemberID(teamAdapter *teams.ServiceAdapter, issue *github.Issue) string {
	if teamAdapter == nil {
		return ""
	}
	memberID, err := teamAdapter.ResolveGitHubIdentity(issue.User.Login, issue.User.Email)
	if err != nil {
		logging.WithComponent("teams").Warn("failed to resolve GitHub identity",
			slog.String("github_user", issue.User.Login),
			slog.Any("error", err),
		)
		return ""
	}
	if memberID != "" {
		logging.WithComponent("teams").Info("resolved GitHub user to team member",
			slog.String("github_user", issue.User.Login),
			slog.String("member_id", memberID),
		)
	}
	return memberID
}

// logTeamTaskEvent writes a task-lifecycle audit entry via the team adapter (#35).
// No-op when RBAC is not configured or no member was resolved; a write failure is
// logged but never blocks task execution.
func logTeamTaskEvent(teamAdapter *teams.ServiceAdapter, memberID, taskID string, action teams.AuditAction, details map[string]interface{}) {
	if teamAdapter == nil || memberID == "" {
		return
	}
	if err := teamAdapter.LogTaskEvent(memberID, taskID, action, details); err != nil {
		logging.WithComponent("teams").Warn("failed to write task audit event",
			slog.String("task_id", taskID),
			slog.String("action", string(action)),
			slog.Any("error", err),
		)
	}
}

// extractGitHubLabelNames returns label name strings from a GitHub issue (GH-727).
// Used to flow labels into executor.Task for decomposition/complexity decisions.
func extractGitHubLabelNames(issue *github.Issue) []string {
	if issue == nil || len(issue.Labels) == 0 {
		return nil
	}
	names := make([]string, len(issue.Labels))
	for i, l := range issue.Labels {
		names[i] = l.Name
	}
	return names
}

// buildExecutionComment formats a comment for successful executions.
func buildExecutionComment(result *executor.ExecutionResult, branchName string) string {
	var sb strings.Builder

	sb.WriteString("✅ Pilot completed!\n\n")
	sb.WriteString("| Metric | Value |\n")
	sb.WriteString("|--------|-------|\n")

	// Duration (always present)
	sb.WriteString(fmt.Sprintf("| Duration | %s |\n", result.Duration.Round(time.Second)))

	// Model
	if result.ModelName != "" {
		sb.WriteString(fmt.Sprintf("| Model | `%s` |\n", result.ModelName))
	}

	// Tokens
	if result.TokensTotal > 0 {
		sb.WriteString(fmt.Sprintf("| Tokens | %s (↑%s ↓%s) |\n",
			formatTokenCountComment(result.TokensTotal),
			formatTokenCountComment(result.TokensInput),
			formatTokenCountComment(result.TokensOutput),
		))
	}

	// Cost
	if result.EstimatedCostUSD > 0 {
		sb.WriteString(fmt.Sprintf("| Cost | ~$%.2f |\n", result.EstimatedCostUSD))
	}

	// Files changed
	if result.FilesChanged > 0 || result.LinesAdded > 0 || result.LinesRemoved > 0 {
		sb.WriteString(fmt.Sprintf("| Files | %d changed (+%d -%d) |\n",
			result.FilesChanged, result.LinesAdded, result.LinesRemoved))
	}

	// Branch
	if branchName != "" {
		sb.WriteString(fmt.Sprintf("| Branch | `%s` |\n", branchName))
	}

	// PR
	if result.PRUrl != "" {
		sb.WriteString(fmt.Sprintf("| PR | %s |\n", result.PRUrl))
	}

	// Intent warning (from intent judge, GH-624)
	if result.IntentWarning != "" {
		sb.WriteString(fmt.Sprintf("\n⚠️ **Intent Warning:** %s\n", result.IntentWarning))
	}

	return sb.String()
}

// buildFailureComment formats a comment for failed executions.
func buildFailureComment(result *executor.ExecutionResult) string {
	var sb strings.Builder
	sb.WriteString("❌ Pilot execution failed\n\n")
	if result != nil && result.Error != "" {
		sb.WriteString("<details>\n<summary>Error details</summary>\n\n")
		sb.WriteString(fmt.Sprintf("```\n%s\n```\n", result.Error))
		sb.WriteString("</details>\n")
	}
	if result != nil {
		if result.Duration > 0 {
			sb.WriteString(fmt.Sprintf("\n**Duration:** %s", result.Duration.Round(time.Second)))
		}
		if result.ModelName != "" {
			sb.WriteString(fmt.Sprintf(" | **Model:** `%s`", result.ModelName))
		}
		if result.EstimatedCostUSD > 0 {
			sb.WriteString(fmt.Sprintf(" | **Cost:** ~$%.2f", result.EstimatedCostUSD))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// formatTokenCountComment formats a token count for display in comments.
func formatTokenCountComment(tokens int64) string {
	if tokens >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(tokens)/1000000)
	}
	if tokens >= 1000 {
		return fmt.Sprintf("%.1fK", float64(tokens)/1000)
	}
	return fmt.Sprintf("%d", tokens)
}

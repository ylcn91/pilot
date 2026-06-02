package executor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// createSubIssuesViaGitHub creates sub-issues using the gh CLI.
// This is the original implementation and fallback path.
//
// The TASK-286 / GH-3027 repo guardrail runs one level up in CreateSubIssues
// (it must fire before queryRecentSubIssues, which also shells out to gh).
func (r *Runner) createSubIssuesViaGitHub(ctx context.Context, plan *EpicPlan, executionPath, owner, repo string) ([]CreatedIssue, error) {
	var created []CreatedIssue

	// Map subtask order → created GitHub issue number for dependency annotation (GH-1794)
	orderToIssueNumber := make(map[int]int)

	for _, subtask := range plan.Subtasks {
		// Build the issue body
		body := subtask.Description
		if plan.ParentTask != nil && plan.ParentTask.ID != "" {
			// GH-2695: inject autopilot-meta marker so spec_validator's inherited-spec
			// bailout path recognises this as a decomposer-generated sub-issue and skips
			// the full spec check, delegating to the parent's validation result instead.
			body = fmt.Sprintf("<!--autopilot-meta\nparent: %s\ninherited-spec: true\n-->\n\nParent: %s\n\n%s",
				plan.ParentTask.ID, plan.ParentTask.ID, body)
		}

		// Wire DependsOn annotations into the body (GH-1794)
		for _, depOrder := range subtask.DependsOn {
			if depNum, ok := orderToIssueNumber[depOrder]; ok {
				body += fmt.Sprintf("\n\nDepends on: #%d", depNum)
			}
		}

		// Truncate title to max 80 chars for GitHub issue limits (GH-1133)
		title := truncateTitle(subtask.Title, 80)

		// GH-2324: Reject LLM analysis-style titles before they reach GitHub.
		// Falls back to a parent-scope conventional title (not a placeholder).
		if err := validateSubtaskTitle(title); err != nil {
			parentID := ""
			parentProject := ""
			parentTitle := ""
			parentBody := ""
			if plan.ParentTask != nil {
				parentID = plan.ParentTask.ID
				parentProject = plan.ParentTask.ProjectPath
				parentTitle = plan.ParentTask.Title
				parentBody = plan.ParentTask.Description
			}
			fallback := applyParentTypeScopeFallback(
				[]PlannedSubtask{{Title: title, Order: subtask.Order}},
				[]int{0},
				parentTitle,
				parentBody,
			)[0].Title
			r.log.Warn("Rejected invalid LLM subtask title; using conventional fallback",
				"original_title", subtask.Title,
				"fallback_title", fallback,
				"reason", err.Error(),
				"subtask_order", subtask.Order,
				"parent_id", parentID,
			)
			r.emitAlertEvent(AlertEvent{
				Type:      AlertEventTypeConfigError,
				TaskID:    parentID,
				TaskTitle: parentTitle,
				Project:   parentProject,
				Error:     fmt.Sprintf("invalid subtask title rejected: %v", err),
				Metadata: map[string]string{
					"event":          "invalid_subtask_title",
					"original_title": subtask.Title,
					"fallback_title": fallback,
					"subtask_order":  strconv.Itoa(subtask.Order),
				},
				Timestamp: time.Now(),
			})
			title = fallback
		}

		// Final CC-format guard: ensures the title passed to gh is always conventional-commit.
		// Catches titles that satisfy validateSubtaskTitle (action verb) but lack type prefix.
		if !isConventionalSubtaskTitle(title) {
			parentTitle := ""
			parentBody := ""
			if plan.ParentTask != nil {
				parentTitle = plan.ParentTask.Title
				parentBody = plan.ParentTask.Description
			}
			title = applyParentTypeScopeFallback(
				[]PlannedSubtask{{Title: title, Order: subtask.Order}},
				[]int{0},
				parentTitle,
				parentBody,
			)[0].Title
		}

		// Create issue using gh CLI
		subLabels := append([]string{"pilot"}, filterPropagatableLabels(plan.ParentTask.Labels)...)
		args := []string{"issue", "create"}
		if slug := ghRepoSlug(owner, repo); slug != "" {
			// GH-3411: target the guardrail-validated repo explicitly. Without --repo,
			// `gh` infers the base repo from ambient state and can create the issue on
			// the fork's upstream parent (e.g. ylcn91/pilot) instead of this repo.
			args = append(args, "--repo", slug)
		}
		args = append(args, "--title", title, "--body", body)
		for _, l := range subLabels {
			args = append(args, "--label", l)
		}

		cmd := exec.CommandContext(ctx, "gh", args...)

		// Set working directory - use executionPath which respects worktree isolation
		if executionPath != "" {
			cmd.Dir = executionPath
		}

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		r.log.Debug("Creating GitHub issue",
			"subtask_order", subtask.Order,
			"title", subtask.Title,
		)

		if err := cmd.Run(); err != nil {
			return created, fmt.Errorf("failed to create issue for subtask %d: %w (stderr: %s)",
				subtask.Order, err, stderr.String())
		}

		// gh issue create outputs the issue URL on success
		issueURL := strings.TrimSpace(stdout.String())
		issueNumber := parseIssueNumber(issueURL)

		created = append(created, CreatedIssue{
			Number:     issueNumber,
			Identifier: strconv.Itoa(issueNumber), // For consistency, populate Identifier too
			URL:        issueURL,
			Subtask:    subtask,
		})

		// GH-3240: pre-mark in the poller so the sub-issue is not re-dispatched
		// on the next poll cycle (the epic will execute it directly via ExecuteSubIssues).
		if r.subIssuePollerSkip != nil && issueNumber > 0 {
			r.subIssuePollerSkip(issueNumber)
		}

		// Track order → issue number for dependency resolution (GH-1794)
		orderToIssueNumber[subtask.Order] = issueNumber

		// GH-2211: Wire native GitHub sub-issue link (non-fatal — text marker is fallback)
		if r.subIssueLinker != nil &&
			plan.ParentTask != nil &&
			plan.ParentTask.SourceRepo != "" &&
			plan.ParentTask.SourceIssueID != "" {
			if parts := strings.SplitN(plan.ParentTask.SourceRepo, "/", 2); len(parts) == 2 {
				if parentNum, parseErr := strconv.Atoi(plan.ParentTask.SourceIssueID); parseErr == nil {
					if linkErr := r.subIssueLinker.LinkSubIssue(ctx, parts[0], parts[1], parentNum, issueNumber); linkErr != nil {
						r.log.Warn("Failed to link native sub-issue",
							"parent", parentNum,
							"child", issueNumber,
							"error", linkErr,
						)
					}
				}
			}
		}

		r.log.Info("Created GitHub issue",
			"subtask_order", subtask.Order,
			"issue_number", issueNumber,
			"url", issueURL,
		)
	}

	return created, nil
}

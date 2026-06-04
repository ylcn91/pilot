package executor

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// ErrIssueCreationDisabled is returned when a caller asks the runner to create
// tracker issues while executor.create_sub_issues is not enabled.
var ErrIssueCreationDisabled = errors.New("issue creation is disabled")

// CreateSubIssues creates issues from the planned subtasks.
// For GitHub-sourced tasks (or when no SubIssueCreator is set), uses gh CLI.
// For non-GitHub adapters with a SubIssueCreator, dispatches via that interface (GH-1471).
// Returns a slice of CreatedIssue with issue identifiers and URLs.
// executionPath may differ from task.ProjectPath when using worktree isolation (GH-968).
func (r *Runner) CreateSubIssues(ctx context.Context, plan *EpicPlan, executionPath string) ([]CreatedIssue, error) {
	if plan == nil || len(plan.Subtasks) == 0 {
		return nil, fmt.Errorf("plan has no subtasks to create issues from")
	}

	if plan.ParentTask != nil {
		// GH-2867 / CS-2 (#32): refuse to spawn sub-issues for a parent that is
		// already done. MustParentBeActionable is the single chokepoint for this
		// decision so the closed-parent guard stays consistent across call sites.
		if err := MustParentBeActionable(plan.ParentTask); err != nil {
			r.log.Info("Skipping sub-issue creation: parent is already done",
				"parent_id", plan.ParentTask.ID,
				"state", plan.ParentTask.State,
				"labels", plan.ParentTask.Labels,
			)
			return nil, err
		}
	}

	if !r.issueCreationEnabled {
		parentID := ""
		if plan.ParentTask != nil {
			parentID = plan.ParentTask.ID
		}
		r.log.Warn("Skipping sub-issue creation: issue creation disabled",
			"parent_id", parentID,
			"subtasks", len(plan.Subtasks),
		)
		return nil, ErrIssueCreationDisabled
	}

	// GH-1471: pick the creation backend up front. The adapter path is
	// non-GitHub (Linear, Jira, …) and uses its own per-adapter auth, so
	// the repo allowlist guardrail below is skipped for that branch.
	useAdapterCreator := r.subIssueCreator != nil &&
		plan.ParentTask != nil &&
		plan.ParentTask.SourceAdapter != "" &&
		plan.ParentTask.SourceAdapter != "github"

	// TASK-286 / GH-3027: guardrail must run BEFORE queryRecentSubIssues
	// because that helper also shells out to `gh` against the worktree's
	// inferred origin remote. Without this ordering, a misconfigured Pilot
	// would still leak `gh issue list` calls to an unmanaged repo even if
	// no sub-issue was created. Missing allowlist/remote now fails closed;
	// the GitHub path may not infer a target repo from ambient gh state.
	// ghOwner/ghRepo capture the guardrail-validated origin repo so every `gh`
	// shell-out below can pin --repo to it (GH-3411). Without an explicit target,
	// `gh` resolves the base repo from ambient state (origin's fork parent, an
	// `upstream` remote, GH_REPO, or `gh repo set-default`) and can read from or
	// create issues on the upstream repo instead of the one the guardrail validated.
	var ghOwner, ghRepo string
	if !useAdapterCreator {
		owner, repo, remoteErr := resolveGitRemote(ctx, executionPath)
		if remoteErr != nil {
			r.log.Error("sub-issue guardrail: could not resolve origin remote",
				"execution_path", executionPath, "error", remoteErr)
			if err := ValidateTargetRepo(r.repoAllowlist, "", "", executionPath); err != nil {
				return nil, fmt.Errorf("sub-issue guardrail (no origin remote at %s): %w", executionPath, err)
			}
		} else if err := ValidateTargetRepo(r.repoAllowlist, owner, repo, executionPath); err != nil {
			r.log.Error("sub-issue guardrail rejected target repo",
				"owner", owner, "repo", repo,
				"execution_path", executionPath, "error", err)
			return nil, fmt.Errorf("sub-issue guardrail: %w", err)
		} else {
			r.log.Debug("sub-issue guardrail passed",
				"owner", owner, "repo", repo, "execution_path", executionPath)
			ghOwner, ghRepo = owner, repo
		}
	}

	if plan.ParentTask != nil {
		// Dedup guard: skip creation if recent sub-issues referencing this parent already exist.
		// Uses an injectable checker so tests can control the result without spawning gh CLI.
		checker := r.openSubIssueCheck
		if checker == nil {
			// GH-3411: pin the dedup `gh issue list` to the validated origin repo
			// so it cannot drift to the fork's upstream parent via gh's ambient
			// base-repo resolution. Empty slug falls back to ambient gh behavior.
			repoSlug := ghRepoSlug(ghOwner, ghRepo)
			checker = func(ctx context.Context, dir, parentID string) (bool, error) {
				return queryRecentSubIssues(ctx, dir, repoSlug, parentID)
			}
		}
		if exists, _ := checker(ctx, executionPath, plan.ParentTask.ID); exists {
			r.log.Info("Skipping sub-issue creation: open children already exist",
				"parent_id", plan.ParentTask.ID,
			)
			return nil, ErrSubIssuesAlreadyExist
		}
	}

	if useAdapterCreator {
		return r.createSubIssuesViaAdapter(ctx, plan)
	}

	return r.createSubIssuesViaGitHub(ctx, plan, executionPath, ghOwner, ghRepo)
}

// createSubIssuesViaAdapter creates sub-issues using the SubIssueCreator interface.
// Used for non-GitHub adapters like Linear, Jira, GitLab, Azure DevOps.
func (r *Runner) createSubIssuesViaAdapter(ctx context.Context, plan *EpicPlan) ([]CreatedIssue, error) {
	var created []CreatedIssue
	parentID := plan.ParentTask.SourceIssueID

	// Map subtask order → created issue identifier for dependency annotation (GH-1794)
	orderToIdentifier := make(map[int]string)

	r.log.Info("Creating sub-issues via adapter",
		"adapter", plan.ParentTask.SourceAdapter,
		"parent_id", parentID,
		"subtask_count", len(plan.Subtasks),
	)

	for _, subtask := range plan.Subtasks {
		// Build the issue body
		body := subtask.Description
		if plan.ParentTask.ID != "" {
			// GH-2695: mirror the GitHub path — inject autopilot-meta marker for parity.
			body = fmt.Sprintf("<!--autopilot-meta\nparent: %s\ninherited-spec: true\n-->\n\nParent: %s\n\n%s",
				plan.ParentTask.ID, plan.ParentTask.ID, body)
		}

		// Wire DependsOn annotations into the body (GH-1794)
		for _, depOrder := range subtask.DependsOn {
			if depID, ok := orderToIdentifier[depOrder]; ok {
				body += fmt.Sprintf("\n\nDepends on: %s", depID)
			}
		}

		// Truncate title (adapter may have different limits, but 80 is reasonable)
		title := truncateTitle(subtask.Title, 80)

		// GH-2324: Reject LLM analysis-style titles before they reach the tracker.
		// Falls back to a parent-scope conventional title (not a placeholder).
		if err := validateSubtaskTitle(title); err != nil {
			fallback := applyParentTypeScopeFallback(
				[]PlannedSubtask{{Title: title, Order: subtask.Order}},
				[]int{0},
				plan.ParentTask.Title,
				plan.ParentTask.Description,
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
				TaskTitle: plan.ParentTask.Title,
				Project:   plan.ParentTask.ProjectPath,
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

		// Final CC-format guard for adapter path.
		if !isConventionalSubtaskTitle(title) {
			title = applyParentTypeScopeFallback(
				[]PlannedSubtask{{Title: title, Order: subtask.Order}},
				[]int{0},
				plan.ParentTask.Title,
				plan.ParentTask.Description,
			)[0].Title
		}

		r.log.Debug("Creating sub-issue via adapter",
			"subtask_order", subtask.Order,
			"title", title,
			"parent_id", parentID,
		)

		subLabels := append([]string{"pilot"}, filterPropagatableLabels(plan.ParentTask.Labels)...)
		identifier, url, err := r.subIssueCreator.CreateIssue(ctx, parentID, title, body, subLabels)
		if err != nil {
			return created, fmt.Errorf("failed to create sub-issue for subtask %d via %s adapter: %w",
				subtask.Order, plan.ParentTask.SourceAdapter, err)
		}

		created = append(created, CreatedIssue{
			Number:     0, // Non-GitHub adapters don't use numeric IDs
			Identifier: identifier,
			URL:        url,
			Subtask:    subtask,
		})

		// Track order → identifier for dependency resolution (GH-1794)
		orderToIdentifier[subtask.Order] = identifier

		r.log.Info("Created sub-issue via adapter",
			"subtask_order", subtask.Order,
			"identifier", identifier,
			"url", url,
		)
	}

	return created, nil
}

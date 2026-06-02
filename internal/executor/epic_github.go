package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ghRepoSlug returns "owner/repo" for a guardrail-validated remote, or "" when
// either part is empty. Callers append "--repo <slug>" to gh invocations only
// when the slug is non-empty; an empty slug falls back to gh's ambient repo
// resolution (the pre-GH-3411 behavior), which the guardrail above only permits
// after it has already validated the resolved owner/repo.
func ghRepoSlug(owner, repo string) string {
	if owner == "" || repo == "" {
		return ""
	}
	return owner + "/" + repo
}

// queryRecentSubIssues returns true when there are open or recently-closed GitHub
// issues (created within the last 24 hours) that include "Parent: <parentID>" in
// their body. Used by CreateSubIssues as a dedup guard (GH-2867).
// Non-fatal: if the gh CLI call fails the check returns (false, nil) to allow creation.
func queryRecentSubIssues(ctx context.Context, dir, repoSlug, parentID string) (bool, error) {
	since := time.Now().UTC().Add(-24 * time.Hour).Format("2006-01-02T15:04:05Z")
	args := []string{"issue", "list"}
	if repoSlug != "" {
		// GH-3411: pin to the validated repo so the dedup search reads the fork,
		// not its upstream parent that gh would otherwise resolve.
		args = append(args, "--repo", repoSlug)
	}
	args = append(args,
		"--state", "all",
		"--search", fmt.Sprintf("\"Parent: %s\" in:body created:>=%s", parentID, since),
		"--json", "number",
		"--limit", "3",
	)
	cmd := exec.CommandContext(ctx, "gh", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return false, nil // non-fatal
	}
	var issues []struct{ Number int }
	if err := json.Unmarshal(stdout.Bytes(), &issues); err != nil {
		return false, nil
	}
	return len(issues) > 0, nil
}

// recoverExistingSubIssues lists all issues (any state) whose body contains
// "Parent: <parentID>" and reconstructs them as []CreatedIssue so the epic
// orchestrator can decide whether to no-op or continue executing open children.
// Non-fatal: returns an empty slice on gh CLI failure.
func recoverExistingSubIssues(ctx context.Context, dir, repoSlug, parentID string) ([]CreatedIssue, error) {
	args := []string{"issue", "list"}
	if repoSlug != "" {
		// GH-3411: pin to the validated repo so recovery reads the fork's children,
		// not the upstream parent's.
		args = append(args, "--repo", repoSlug)
	}
	args = append(args,
		"--state", "all",
		"--search", fmt.Sprintf("\"Parent: %s\" in:body", parentID),
		"--json", "number,url,state",
		"--limit", "50",
	)
	cmd := exec.CommandContext(ctx, "gh", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return nil, nil // non-fatal
	}
	var raw []struct {
		Number int    `json:"number"`
		URL    string `json:"url"`
		State  string `json:"state"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		return nil, nil
	}
	out := make([]CreatedIssue, 0, len(raw))
	for _, r := range raw {
		out = append(out, CreatedIssue{
			Number:     r.Number,
			Identifier: strconv.Itoa(r.Number),
			URL:        r.URL,
			State:      r.State,
		})
	}
	return out, nil
}

// allChildrenDone reports whether every issue in the slice is in a non-open state.
func allChildrenDone(issues []CreatedIssue) bool {
	// GH-3053: empty slice must not vacuously satisfy "all done". A
	// recoverExistingSubIssues call that returns zero issues (gh search
	// hiccup, no sub-issues created yet, network blip) would otherwise
	// trip the false epic-complete path in runner.go: empty → true →
	// "All sub-issues already completed (100%)" → exit without work.
	if len(issues) == 0 {
		return false
	}
	for _, iss := range issues {
		if strings.ToLower(iss.State) == "open" {
			return false
		}
	}
	return true
}

// issueNumberRegex extracts the issue number from a GitHub issue URL.
// Matches patterns like: https://github.com/owner/repo/issues/123
var issueNumberRegex = regexp.MustCompile(`/issues/(\d+)`)

// parseIssueNumber extracts the issue number from a GitHub issue URL.
// Returns 0 if no issue number is found.
func parseIssueNumber(url string) int {
	matches := issueNumberRegex.FindStringSubmatch(url)
	if len(matches) < 2 {
		return 0
	}
	var num int
	_, _ = fmt.Sscanf(matches[1], "%d", &num)
	return num
}

// parsePRNumberFromURL extracts a PR number from a GitHub PR URL.
// Returns 0 if the URL doesn't contain a valid PR number.
func parsePRNumberFromURL(url string) int {
	// Match /pull/123 at the end of the URL
	idx := strings.LastIndex(url, "/pull/")
	if idx < 0 {
		return 0
	}
	numStr := strings.TrimSpace(url[idx+len("/pull/"):])
	// Strip any trailing path segments
	if slashIdx := strings.Index(numStr, "/"); slashIdx >= 0 {
		numStr = numStr[:slashIdx]
	}
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return 0
	}
	return n
}

// UpdateIssueProgress adds a progress comment to an issue.
func (r *Runner) UpdateIssueProgress(ctx context.Context, projectPath string, issueID string, message string) error {
	if r.dryRun {
		r.log.Info("dry-run: skipping UpdateIssueProgress", "issue", issueID)
		return nil
	}

	args := []string{"issue", "comment", issueID, "--body", message}
	cmd := exec.CommandContext(ctx, "gh", args...)
	if projectPath != "" {
		cmd.Dir = projectPath
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to comment on issue %s: %w (stderr: %s)", issueID, err, stderr.String())
	}
	return nil
}

// CloseIssueWithComment closes an issue with a completion comment.
// Includes an idempotency check: if the issue is already CLOSED, the close is skipped.
func (r *Runner) CloseIssueWithComment(ctx context.Context, projectPath string, issueID string, comment string) error {
	if r.dryRun {
		r.log.Info("dry-run: skipping CloseIssueWithComment", "issue", issueID)
		return nil
	}

	// Idempotency: check if issue is already closed before attempting close.
	stateCmd := exec.CommandContext(ctx, "gh", "issue", "view", issueID, "--json", "state", "--jq", ".state")
	if projectPath != "" {
		stateCmd.Dir = projectPath
	}
	if stateOut, err := stateCmd.Output(); err == nil {
		if strings.TrimSpace(string(stateOut)) == "CLOSED" {
			r.log.Info("issue already closed, skipping", "issue", issueID)
			return nil
		}
	}

	args := []string{"issue", "close", issueID, "--comment", comment}
	cmd := exec.CommandContext(ctx, "gh", args...)
	if projectPath != "" {
		cmd.Dir = projectPath
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to close issue %s: %w (stderr: %s)", issueID, err, stderr.String())
	}
	return nil
}

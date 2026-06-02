package github

import (
	"context"
	"fmt"
	"net/http"
)

// CreatePullRequest creates a new pull request
func (c *Client) CreatePullRequest(ctx context.Context, owner, repo string, input *PullRequestInput) (*PullRequest, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls", owner, repo)
	var result PullRequest
	if err := c.doRequest(ctx, http.MethodPost, path, input, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RequestReviewers requests reviewers for a pull request.
// reviewers are individual GitHub usernames, teamReviewers are team slugs.
func (c *Client) RequestReviewers(ctx context.Context, owner, repo string, number int, reviewers, teamReviewers []string) error {
	if len(reviewers) == 0 && len(teamReviewers) == 0 {
		return nil
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/requested_reviewers", owner, repo, number)
	body := map[string][]string{}
	if len(reviewers) > 0 {
		body["reviewers"] = reviewers
	}
	if len(teamReviewers) > 0 {
		body["team_reviewers"] = teamReviewers
	}
	return c.doRequest(ctx, http.MethodPost, path, body, nil)
}

// GetPullRequest fetches a pull request by number
func (c *Client) GetPullRequest(ctx context.Context, owner, repo string, number int) (*PullRequest, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, number)
	var result PullRequest
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ClosePullRequest closes a pull request without merging.
// Used by autopilot to close failed PRs so the sequential poller can unblock.
func (c *Client) ClosePullRequest(ctx context.Context, owner, repo string, number int) error {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, number)
	payload := map[string]string{"state": "closed"}
	return c.doRequest(ctx, http.MethodPatch, path, payload, nil)
}

// AddPRComment adds a comment to a pull request (issue comment API)
// For review comments on specific lines, use CreatePRReviewComment instead
func (c *Client) AddPRComment(ctx context.Context, owner, repo string, number int, body string) (*PRComment, error) {
	// PRs use the issues API for general comments
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, repo, number)
	reqBody := map[string]string{"body": body}
	var result PRComment
	if err := c.doRequest(ctx, http.MethodPost, path, reqBody, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// MergePullRequest merges a pull request
// method can be "merge", "squash", or "rebase" (use MergeMethod* constants)
// commitTitle is optional - if empty, GitHub uses the default
func (c *Client) MergePullRequest(ctx context.Context, owner, repo string, number int, method, commitTitle string) error {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/merge", owner, repo, number)

	body := map[string]string{
		"merge_method": method,
	}
	if commitTitle != "" {
		body["commit_title"] = commitTitle
	}

	return c.doRequest(ctx, http.MethodPut, path, body, nil)
}

// ApprovePullRequest creates an approval review on a PR
// body is the optional review comment
func (c *Client) ApprovePullRequest(ctx context.Context, owner, repo string, number int, body string) error {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews", owner, repo, number)

	payload := map[string]string{
		"event": ReviewEventApprove,
	}
	if body != "" {
		payload["body"] = body
	}

	return c.doRequest(ctx, http.MethodPost, path, payload, nil)
}

// ListPullRequests lists pull requests for a repository.
// state can be "open", "closed", or "all".
// Results are paginated at 100 per page (GitHub default is 30). A safety cap
// of 50 pages (5 000 PRs) prevents runaway loops on very large repos.
func (c *Client) ListPullRequests(ctx context.Context, owner, repo, state string) ([]*PullRequest, error) {
	const perPage = 100
	const maxPages = 50

	var all []*PullRequest
	for page := 1; page <= maxPages; page++ {
		path := fmt.Sprintf("/repos/%s/%s/pulls?state=%s&per_page=%d&page=%d", owner, repo, state, perPage, page)
		var batch []*PullRequest
		if err := c.doRequest(ctx, http.MethodGet, path, nil, &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < perPage {
			break
		}
	}
	return all, nil
}

// GetPRCommits returns all commits in a pull request
func (c *Client) GetPRCommits(ctx context.Context, owner, repo string, prNumber int) ([]*Commit, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/commits?per_page=100", owner, repo, prNumber)
	var result []*Commit
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// CompareCommits compares two commits and returns commits between them
func (c *Client) CompareCommits(ctx context.Context, owner, repo, base, head string) ([]*Commit, error) {
	path := fmt.Sprintf("/repos/%s/%s/compare/%s...%s", owner, repo, base, head)
	var result struct {
		Commits []*Commit `json:"commits"`
	}
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return result.Commits, nil
}

// ListPullRequestReviews lists all reviews for a pull request
func (c *Client) ListPullRequestReviews(ctx context.Context, owner, repo string, number int) ([]*PullRequestReview, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews", owner, repo, number)
	var result []*PullRequestReview
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// PRFile represents a file changed in a pull request.
type PRFile struct {
	Filename  string `json:"filename"`
	Status    string `json:"status"` // "added", "removed", "modified", "renamed"
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

// ListPullRequestFiles returns the list of files changed in a pull request.
func (c *Client) ListPullRequestFiles(ctx context.Context, owner, repo string, number int) ([]*PRFile, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/files", owner, repo, number)
	var result []*PRFile
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// HasApprovalReview checks if a PR has at least one approval review.
// Returns (hasApproval, approverLogin, error).
// Only considers the latest review from each user.
func (c *Client) HasApprovalReview(ctx context.Context, owner, repo string, number int) (bool, string, error) {
	reviews, err := c.ListPullRequestReviews(ctx, owner, repo, number)
	if err != nil {
		return false, "", err
	}

	// Track latest review state per user
	latestReviews := make(map[string]string) // user login -> state
	for _, review := range reviews {
		latestReviews[review.User.Login] = review.State
	}

	// Check if any user's latest review is APPROVED
	for login, state := range latestReviews {
		if state == ReviewStateApproved {
			return true, login, nil
		}
	}

	return false, "", nil
}

// GetPullRequestComments returns line-level review comments on a pull request.
// These are inline code annotations, distinct from top-level review bodies returned by ListPullRequestReviews.
// Uses: GET /repos/{owner}/{repo}/pulls/{number}/comments
func (c *Client) GetPullRequestComments(ctx context.Context, owner, repo string, number int) ([]*PRReviewComment, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/comments?per_page=100", owner, repo, number)
	var result []*PRReviewComment
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// UpdatePullRequestBranch updates the PR branch with the latest base branch.
// Uses GitHub API: PUT /repos/{owner}/{repo}/pulls/{number}/update-branch
// Returns nil on success, error if the branch cannot be automatically updated (true conflict).
func (c *Client) UpdatePullRequestBranch(ctx context.Context, owner, repo string, number int) error {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/update-branch", owner, repo, number)
	body := map[string]interface{}{}
	return c.doRequest(ctx, http.MethodPut, path, body, nil)
}

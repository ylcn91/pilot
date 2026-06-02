package azuredevops

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Git/Repository API methods

// GetDefaultBranch returns the default branch of the repository
func (c *Client) GetDefaultBranch(ctx context.Context) (string, error) {
	path := fmt.Sprintf("/%s/%s/_apis/git/repositories/%s?api-version=%s",
		url.PathEscape(c.organization),
		url.PathEscape(c.project),
		url.PathEscape(c.repository),
		apiVersion,
	)

	var repo struct {
		DefaultBranch string `json:"defaultBranch"`
	}
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &repo); err != nil {
		return "", err
	}

	// Default branch is returned as "refs/heads/main"
	branch := strings.TrimPrefix(repo.DefaultBranch, "refs/heads/")
	return branch, nil
}

// GetBranch gets a branch reference
func (c *Client) GetBranch(ctx context.Context, branchName string) (*GitRef, error) {
	refName := branchName
	if !strings.HasPrefix(refName, "refs/heads/") {
		refName = "refs/heads/" + branchName
	}

	path := fmt.Sprintf("/%s/%s/_apis/git/repositories/%s/refs?filter=%s&api-version=%s",
		url.PathEscape(c.organization),
		url.PathEscape(c.project),
		url.PathEscape(c.repository),
		url.QueryEscape(refName),
		apiVersion,
	)

	var result struct {
		Value []GitRef `json:"value"`
	}
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}

	for _, ref := range result.Value {
		if ref.Name == refName {
			return &ref, nil
		}
	}

	return nil, fmt.Errorf("branch not found: %s", branchName)
}

// CreateBranch creates a new branch from a source ref
func (c *Client) CreateBranch(ctx context.Context, branchName, fromRef string) error {
	// Get the source ref to get the commit SHA
	sourceBranch, err := c.GetBranch(ctx, fromRef)
	if err != nil {
		return fmt.Errorf("failed to get source branch: %w", err)
	}

	newRefName := branchName
	if !strings.HasPrefix(newRefName, "refs/heads/") {
		newRefName = "refs/heads/" + branchName
	}

	path := fmt.Sprintf("/%s/%s/_apis/git/repositories/%s/refs?api-version=%s",
		url.PathEscape(c.organization),
		url.PathEscape(c.project),
		url.PathEscape(c.repository),
		apiVersion,
	)

	refUpdates := []GitRefUpdate{
		{
			Name:        newRefName,
			OldObjectID: "0000000000000000000000000000000000000000", // 40 zeros for new branch
			NewObjectID: sourceBranch.ObjectID,
		},
	}

	var result struct {
		Value []GitRef `json:"value"`
	}
	if err := c.doRequest(ctx, http.MethodPost, path, refUpdates, &result); err != nil {
		return err
	}

	return nil
}

// Pull Request API methods

// CreatePullRequest creates a new pull request
func (c *Client) CreatePullRequest(ctx context.Context, input *PullRequestInput) (*PullRequest, error) {
	// Ensure refs are in full format
	sourceRef := input.SourceRefName
	if !strings.HasPrefix(sourceRef, "refs/heads/") {
		sourceRef = "refs/heads/" + sourceRef
	}
	targetRef := input.TargetRefName
	if !strings.HasPrefix(targetRef, "refs/heads/") {
		targetRef = "refs/heads/" + targetRef
	}

	path := fmt.Sprintf("/%s/%s/_apis/git/repositories/%s/pullrequests?api-version=%s",
		url.PathEscape(c.organization),
		url.PathEscape(c.project),
		url.PathEscape(c.repository),
		apiVersion,
	)

	reqBody := map[string]interface{}{
		"title":         input.Title,
		"description":   input.Description,
		"sourceRefName": sourceRef,
		"targetRefName": targetRef,
	}
	if input.IsDraft {
		reqBody["isDraft"] = true
	}

	var pr PullRequest
	if err := c.doRequest(ctx, http.MethodPost, path, reqBody, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// GetPullRequest fetches a pull request by ID
func (c *Client) GetPullRequest(ctx context.Context, id int) (*PullRequest, error) {
	path := fmt.Sprintf("/%s/%s/_apis/git/repositories/%s/pullrequests/%d?api-version=%s",
		url.PathEscape(c.organization),
		url.PathEscape(c.project),
		url.PathEscape(c.repository),
		id,
		apiVersion,
	)

	var pr PullRequest
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// CompletePullRequest completes (merges) a pull request
func (c *Client) CompletePullRequest(ctx context.Context, id int, deleteSourceBranch bool) (*PullRequest, error) {
	// First get the PR to get the last merge source commit
	pr, err := c.GetPullRequest(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get pull request: %w", err)
	}

	if pr.LastMergeSourceCommit == nil {
		return nil, fmt.Errorf("pull request has no source commit")
	}

	path := fmt.Sprintf("/%s/%s/_apis/git/repositories/%s/pullrequests/%d?api-version=%s",
		url.PathEscape(c.organization),
		url.PathEscape(c.project),
		url.PathEscape(c.repository),
		id,
		apiVersion,
	)

	reqBody := map[string]interface{}{
		"status": PRStateCompleted,
		"lastMergeSourceCommit": map[string]string{
			"commitId": pr.LastMergeSourceCommit.CommitID,
		},
		"completionOptions": map[string]interface{}{
			"deleteSourceBranch": deleteSourceBranch,
			"mergeCommitMessage": pr.Title,
		},
	}

	var result PullRequest
	if err := c.doRequest(ctx, http.MethodPatch, path, reqBody, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// AbandonPullRequest abandons (closes without merge) a pull request
func (c *Client) AbandonPullRequest(ctx context.Context, id int) (*PullRequest, error) {
	path := fmt.Sprintf("/%s/%s/_apis/git/repositories/%s/pullrequests/%d?api-version=%s",
		url.PathEscape(c.organization),
		url.PathEscape(c.project),
		url.PathEscape(c.repository),
		id,
		apiVersion,
	)

	reqBody := map[string]interface{}{
		"status": PRStateAbandoned,
	}

	var pr PullRequest
	if err := c.doRequest(ctx, http.MethodPatch, path, reqBody, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// ListPullRequests lists pull requests with optional status filter
func (c *Client) ListPullRequests(ctx context.Context, status string) ([]*PullRequest, error) {
	path := fmt.Sprintf("/%s/%s/_apis/git/repositories/%s/pullrequests?api-version=%s",
		url.PathEscape(c.organization),
		url.PathEscape(c.project),
		url.PathEscape(c.repository),
		apiVersion,
	)

	if status != "" {
		path += "&searchCriteria.status=" + status
	}

	var result struct {
		Value []*PullRequest `json:"value"`
	}
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return result.Value, nil
}

// AddPRComment adds a comment to a pull request thread
func (c *Client) AddPRComment(ctx context.Context, prID int, comment string) error {
	path := fmt.Sprintf("/%s/%s/_apis/git/repositories/%s/pullrequests/%d/threads?api-version=%s",
		url.PathEscape(c.organization),
		url.PathEscape(c.project),
		url.PathEscape(c.repository),
		prID,
		apiVersion,
	)

	reqBody := map[string]interface{}{
		"comments": []map[string]string{
			{"content": comment},
		},
	}

	return c.doRequest(ctx, http.MethodPost, path, reqBody, nil)
}

// GetWorkItemWebURL constructs the web URL for a work item
func (c *Client) GetWorkItemWebURL(id int) string {
	return fmt.Sprintf("%s/%s/%s/_workitems/edit/%d",
		c.baseURL,
		url.PathEscape(c.organization),
		url.PathEscape(c.project),
		id,
	)
}

// GetPullRequestWebURL constructs the web URL for a pull request
func (c *Client) GetPullRequestWebURL(id int) string {
	return fmt.Sprintf("%s/%s/%s/_git/%s/pullrequest/%d",
		c.baseURL,
		url.PathEscape(c.organization),
		url.PathEscape(c.project),
		url.PathEscape(c.repository),
		id,
	)
}

// HasTag checks if a work item has a specific tag
func HasTag(wi *WorkItem, tag string) bool {
	return wi.HasTag(tag)
}

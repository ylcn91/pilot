package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// GetBranch fetches information about a branch
func (c *Client) GetBranch(ctx context.Context, owner, repo, branch string) (*Branch, error) {
	path := fmt.Sprintf("/repos/%s/%s/branches/%s", owner, repo, url.PathEscape(branch))
	var result Branch
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UpdateRef creates or updates a branch ref to point at the given SHA.
// Uses PATCH /repos/{owner}/{repo}/git/refs/heads/{branch} with force=true.
// If the ref does not exist, falls back to creating it via POST.
func (c *Client) UpdateRef(ctx context.Context, owner, repo, branch, sha string) error {
	path := fmt.Sprintf("/repos/%s/%s/git/refs/heads/%s", owner, repo, url.PathEscape(branch))
	body := map[string]interface{}{
		"sha":   sha,
		"force": true,
	}
	err := c.doRequest(ctx, http.MethodPatch, path, body, nil)
	if err == nil {
		return nil
	}
	// If the ref doesn't exist yet, create it
	if isUnprocessableError(err) || isNotFoundError(err) {
		createPath := fmt.Sprintf("/repos/%s/%s/git/refs", owner, repo)
		createBody := map[string]string{
			"ref": "refs/heads/" + branch,
			"sha": sha,
		}
		return c.doRequest(ctx, http.MethodPost, createPath, createBody, nil)
	}
	return err
}

// DeleteBranch deletes a branch from the repository.
// GitHub API: DELETE /repos/{owner}/{repo}/git/refs/heads/{branch}
// Returns nil on success, or if the branch was already deleted (404/422).
func (c *Client) DeleteBranch(ctx context.Context, owner, repo, branch string) error {
	path := fmt.Sprintf("/repos/%s/%s/git/refs/heads/%s", owner, repo, url.PathEscape(branch))
	err := c.doRequest(ctx, http.MethodDelete, path, nil, nil)
	// 404 = branch doesn't exist, 422 = branch already deleted
	// Both are success cases for cleanup
	if isNotFoundError(err) || isUnprocessableError(err) {
		return nil
	}
	return err
}

package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// CreateCommitStatus creates a status for a specific commit SHA
// The context parameter allows multiple statuses per commit (e.g., "ci/build", "pilot/execution")
func (c *Client) CreateCommitStatus(ctx context.Context, owner, repo, sha string, status *CommitStatus) (*CommitStatus, error) {
	path := fmt.Sprintf("/repos/%s/%s/statuses/%s", owner, repo, sha)
	var result CommitStatus
	if err := c.doRequest(ctx, http.MethodPost, path, status, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// CreateCheckRun creates a check run for the GitHub Checks API
// Requires a GitHub App token with checks:write permission
func (c *Client) CreateCheckRun(ctx context.Context, owner, repo string, checkRun *CheckRun) (*CheckRun, error) {
	path := fmt.Sprintf("/repos/%s/%s/check-runs", owner, repo)
	var result CheckRun
	if err := c.doRequest(ctx, http.MethodPost, path, checkRun, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UpdateCheckRun updates an existing check run
func (c *Client) UpdateCheckRun(ctx context.Context, owner, repo string, checkRunID int64, checkRun *CheckRun) (*CheckRun, error) {
	path := fmt.Sprintf("/repos/%s/%s/check-runs/%d", owner, repo, checkRunID)
	var result CheckRun
	if err := c.doRequest(ctx, http.MethodPatch, path, checkRun, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetCombinedStatus gets combined status for a commit SHA
// Returns the combined state of all statuses for the commit
func (c *Client) GetCombinedStatus(ctx context.Context, owner, repo, sha string) (*CombinedStatus, error) {
	path := fmt.Sprintf("/repos/%s/%s/commits/%s/status", owner, repo, sha)

	var status CombinedStatus
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &status); err != nil {
		return nil, err
	}

	return &status, nil
}

// ListCheckRuns lists check runs for a commit SHA
// Returns check runs from GitHub Actions and other check suites
func (c *Client) ListCheckRuns(ctx context.Context, owner, repo, sha string) (*CheckRunsResponse, error) {
	path := fmt.Sprintf("/repos/%s/%s/commits/%s/check-runs", owner, repo, sha)

	var result CheckRunsResponse
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// GetJobLogs fetches the logs for a GitHub Actions job (check run).
// Uses GET /repos/{owner}/{repo}/actions/jobs/{job_id}/logs which returns
// a 302 redirect to a log download URL. Returns the raw log text.
// GH-1567: Used to include CI error logs in autopilot fix issues.
func (c *Client) GetJobLogs(ctx context.Context, owner, repo string, jobID int64) (string, error) {
	path := fmt.Sprintf("/repos/%s/%s/actions/jobs/%d/logs", owner, repo, jobID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("API error (status %d) fetching job logs", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read log response: %w", err)
	}

	return string(body), nil
}

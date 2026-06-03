package bitbucket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/ylcn91/pilot/internal/executor"
)

// Client is a compile-time check that *Client satisfies the executor.PRCreator
// contract, so the runner can create PRs via the Bitbucket API.
var _ executor.PRCreator = (*Client)(nil)

// ErrIssueTrackerDisabled indicates the repository's Cloud issue tracker is not
// enabled (Bitbucket returns 404 for issue endpoints in that case).
var ErrIssueTrackerDisabled = errors.New("bitbucket issue tracker is disabled for this repository")

// Client is a Bitbucket Cloud REST 2.0 API client
type Client struct {
	token      string
	username   string // when set, Basic auth (username + app password) is used instead of Bearer
	workspace  string
	repo       string
	httpClient *http.Client
	baseURL    string // defaults to defaultBaseURL; Server/Data Center override seam
}

// NewClient creates a new Bitbucket Cloud client.
// workspace and repo are the workspace and repository slugs.
func NewClient(token, workspace, repo string) *Client {
	return &Client{
		token:     token,
		workspace: workspace,
		repo:      repo,
		baseURL:   defaultBaseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewClientWithConfig creates a new Bitbucket client from config, applying the
// base_url override seam (reserved for future Server/Data Center support).
func NewClientWithConfig(config *Config) *Client {
	c := NewClient(config.Token, config.Workspace, config.Repo)
	if config.Username != "" {
		c.username = config.Username
	}
	if config.BaseURL != "" {
		c.baseURL = config.BaseURL
	}
	return c
}

// NewClientWithBaseURL creates a new Bitbucket client with a custom base URL (for testing).
func NewClientWithBaseURL(token, workspace, repo, baseURL string) *Client {
	return &Client{
		token:     token,
		workspace: workspace,
		repo:      repo,
		baseURL:   baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// repoPath returns the URL-escaped /repositories/{workspace}/{repo} prefix.
func (c *Client) repoPath() string {
	return fmt.Sprintf("/repositories/%s/%s", url.PathEscape(c.workspace), url.PathEscape(c.repo))
}

// doRequest performs an HTTP request to the Bitbucket Cloud API.
// Auth is Basic (username + app password) when username is set, otherwise Bearer.
func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	if c.username != "" {
		req.SetBasicAuth(c.username, c.token)
	} else {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}
	}

	return nil
}

// isNotFound reports whether the error wraps a 404 from the Bitbucket API.
func isNotFound(err error) bool {
	return err != nil && bytes.Contains([]byte(err.Error()), []byte("status 404"))
}

// GetRepository fetches repository info
func (c *Client) GetRepository(ctx context.Context) (*Repository, error) {
	var repo Repository
	if err := c.doRequest(ctx, http.MethodGet, c.repoPath(), nil, &repo); err != nil {
		return nil, err
	}
	return &repo, nil
}

// GetIssue fetches an issue by its numeric ID.
// Returns ErrIssueTrackerDisabled if the repository has no issue tracker (404).
func (c *Client) GetIssue(ctx context.Context, id int) (*Issue, error) {
	path := fmt.Sprintf("%s/issues/%d", c.repoPath(), id)
	var issue Issue
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &issue); err != nil {
		if isNotFound(err) {
			return nil, ErrIssueTrackerDisabled
		}
		return nil, err
	}
	return &issue, nil
}

// ListIssues lists issues for the repository with optional filters.
// Bitbucket Cloud uses a BBQL `q` query parameter for filtering.
// Returns an empty slice (no error) if the issue tracker is disabled (404).
func (c *Client) ListIssues(ctx context.Context, opts *ListIssuesOptions) ([]*Issue, error) {
	query := url.Values{}
	if opts != nil {
		var clauses []string
		if opts.State != "" {
			clauses = append(clauses, fmt.Sprintf(`state="%s"`, opts.State))
		}
		if !opts.UpdatedAt.IsZero() {
			clauses = append(clauses, fmt.Sprintf(`updated_on>%s`, opts.UpdatedAt.Format(time.RFC3339)))
		}
		if len(clauses) > 0 {
			q := clauses[0]
			for _, cl := range clauses[1:] {
				q += " AND " + cl
			}
			query.Set("q", q)
		}
		if opts.Sort != "" {
			query.Set("sort", opts.Sort)
		}
	}

	path := c.repoPath() + "/issues"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}

	var list IssueList
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &list); err != nil {
		if isNotFound(err) {
			// Issue tracker disabled — treat as no issues rather than a hard error.
			return nil, nil
		}
		return nil, err
	}

	// Synthesize the label set from kind/priority for downstream label filtering.
	for _, issue := range list.Values {
		issue.Labels = synthesizeLabels(issue)
	}
	return list.Values, nil
}

// AddIssueComment adds a comment to an issue
func (c *Client) AddIssueComment(ctx context.Context, id int, body string) (*Comment, error) {
	path := fmt.Sprintf("%s/issues/%d/comments", c.repoPath(), id)
	reqBody := map[string]interface{}{
		"content": map[string]string{"raw": body},
	}
	var comment Comment
	if err := c.doRequest(ctx, http.MethodPost, path, reqBody, &comment); err != nil {
		if isNotFound(err) {
			return nil, ErrIssueTrackerDisabled
		}
		return nil, err
	}
	return &comment, nil
}

// UpdateIssueState updates an issue's state (e.g. "resolved", "open").
func (c *Client) UpdateIssueState(ctx context.Context, id int, state string) error {
	path := fmt.Sprintf("%s/issues/%d", c.repoPath(), id)
	reqBody := map[string]string{"state": state}
	if err := c.doRequest(ctx, http.MethodPut, path, reqBody, nil); err != nil {
		if isNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// CreatePullRequest creates a new pull request
func (c *Client) CreatePullRequest(ctx context.Context, input *PullRequestInput) (*PullRequest, error) {
	path := c.repoPath() + "/pullrequests"
	var pr PullRequest
	if err := c.doRequest(ctx, http.MethodPost, path, input, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// CreatePR implements the executor.PRCreator interface.
// It creates a Bitbucket pull request and returns the web URL.
func (c *Client) CreatePR(ctx context.Context, sourceBranch, targetBranch, title, body string) (string, error) {
	pr, err := c.CreatePullRequest(ctx, &PullRequestInput{
		Title:             title,
		Description:       body,
		Source:            &PREndpoint{Branch: &Branch{Name: sourceBranch}},
		Destination:       &PREndpoint{Branch: &Branch{Name: targetBranch}},
		CloseSourceBranch: true,
	})
	if err != nil {
		return "", fmt.Errorf("Bitbucket PR creation failed: %w", err)
	}
	if pr.Links != nil && pr.Links.HTML != nil {
		return pr.Links.HTML.Href, nil
	}
	return "", nil
}

// GetPullRequest fetches a pull request by ID
func (c *Client) GetPullRequest(ctx context.Context, id int) (*PullRequest, error) {
	path := fmt.Sprintf("%s/pullrequests/%d", c.repoPath(), id)
	var pr PullRequest
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// MergePullRequest merges a pull request
func (c *Client) MergePullRequest(ctx context.Context, id int, closeSourceBranch bool) (*PullRequest, error) {
	path := fmt.Sprintf("%s/pullrequests/%d/merge", c.repoPath(), id)
	reqBody := map[string]interface{}{
		"close_source_branch": closeSourceBranch,
	}
	var pr PullRequest
	if err := c.doRequest(ctx, http.MethodPost, path, reqBody, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// ListPullRequests lists pull requests for the repository, optionally filtered by state.
func (c *Client) ListPullRequests(ctx context.Context, state string) ([]*PullRequest, error) {
	path := c.repoPath() + "/pullrequests"
	if state != "" {
		query := url.Values{}
		query.Set("state", state)
		path += "?" + query.Encode()
	}
	var list PullRequestList
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &list); err != nil {
		return nil, err
	}
	return list.Values, nil
}

// GetCommitStatuses fetches build statuses for a commit SHA
func (c *Client) GetCommitStatuses(ctx context.Context, sha string) ([]*CommitStatus, error) {
	path := fmt.Sprintf("%s/commit/%s/statuses", c.repoPath(), url.PathEscape(sha))
	var list CommitStatusList
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &list); err != nil {
		return nil, err
	}
	return list.Values, nil
}

// HasLabel checks if an issue carries a specific synthesized label
func HasLabel(issue *Issue, labelName string) bool {
	for _, label := range issue.Labels {
		if label == labelName {
			return true
		}
	}
	return false
}

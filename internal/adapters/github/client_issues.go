package github

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Issue represents a GitHub issue
type Issue struct {
	ID          int64     `json:"id"`
	NodeID      string    `json:"node_id"` // GraphQL global node ID
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	State       string    `json:"state"`
	Labels      []Label   `json:"labels"`
	Assignee    *User     `json:"assignee"`
	Assignees   []User    `json:"assignees"`
	User        User      `json:"user"` // Issue author
	HTMLURL     string    `json:"html_url"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	PullRequest *struct{} `json:"pull_request,omitempty"` // Non-nil when item is a PR (GitHub Issues API returns both)
}

// Label represents a GitHub label
type Label struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Color       string `json:"color"`
}

// User represents a GitHub user
type User struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
	Email string `json:"email,omitempty"`
}

// Repository represents a GitHub repository
type Repository struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	Owner    User   `json:"owner"`
	HTMLURL  string `json:"html_url"`
	CloneURL string `json:"clone_url"`
	SSHURL   string `json:"ssh_url"`
}

// Comment represents a GitHub issue comment
type Comment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	User      User      `json:"user"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// GetIssue fetches an issue by owner, repo, and number
func (c *Client) GetIssue(ctx context.Context, owner, repo string, number int) (*Issue, error) {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", owner, repo, number)
	var issue Issue
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &issue); err != nil {
		return nil, err
	}
	return &issue, nil
}

// ListIssueComments returns all comments on an issue or PR.
func (c *Client) ListIssueComments(ctx context.Context, owner, repo string, number int) ([]*Comment, error) {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, repo, number)
	var comments []*Comment
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &comments); err != nil {
		return nil, err
	}
	return comments, nil
}

// AddComment adds a comment to an issue
func (c *Client) AddComment(ctx context.Context, owner, repo string, number int, body string) (*Comment, error) {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, repo, number)
	reqBody := map[string]string{"body": body}
	var comment Comment
	if err := c.doRequest(ctx, http.MethodPost, path, reqBody, &comment); err != nil {
		return nil, err
	}
	return &comment, nil
}

// UpdateIssueComment edits the body of an existing issue/PR comment by its
// comment ID. Used to update-or-create idempotent bot comments (e.g. the
// guardrails summary) instead of stacking a fresh comment on every run.
func (c *Client) UpdateIssueComment(ctx context.Context, owner, repo string, commentID int64, body string) (*Comment, error) {
	path := fmt.Sprintf("/repos/%s/%s/issues/comments/%d", owner, repo, commentID)
	reqBody := map[string]string{"body": body}
	var comment Comment
	if err := c.doRequest(ctx, http.MethodPatch, path, reqBody, &comment); err != nil {
		return nil, err
	}
	return &comment, nil
}

// AddLabels adds labels to an issue
func (c *Client) AddLabels(ctx context.Context, owner, repo string, number int, labels []string) error {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/labels", owner, repo, number)
	reqBody := map[string][]string{"labels": labels}
	return c.doRequest(ctx, http.MethodPost, path, reqBody, nil)
}

// RemoveLabel removes a label from an issue
func (c *Client) RemoveLabel(ctx context.Context, owner, repo string, number int, label string) error {
	// GitHub API is case-sensitive for label names in URL path, normalize to lowercase
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/labels/%s", owner, repo, number, strings.ToLower(label))
	err := c.doRequest(ctx, http.MethodDelete, path, nil, nil)
	// 404 is OK - label might not exist
	if err != nil && err.Error() != "API error (status 404): " {
		return err
	}
	return nil
}

// UpdateIssueState updates an issue's state (open/closed)
func (c *Client) UpdateIssueState(ctx context.Context, owner, repo string, number int, state string) error {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", owner, repo, number)
	reqBody := map[string]string{"state": state}
	return c.doRequest(ctx, http.MethodPatch, path, reqBody, nil)
}

// GetRepository fetches repository info
func (c *Client) GetRepository(ctx context.Context, owner, repo string) (*Repository, error) {
	path := fmt.Sprintf("/repos/%s/%s", owner, repo)
	var repository Repository
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &repository); err != nil {
		return nil, err
	}
	return &repository, nil
}

// ListIssues lists issues for a repository with optional filters
// Note: Labels are filtered case-insensitively in Go code after fetching,
// because GitHub API label queries are case-sensitive.
func (c *Client) ListIssues(ctx context.Context, owner, repo string, opts *ListIssuesOptions) ([]*Issue, error) {
	const perPage = 100
	const maxPages = 50

	// Build query parameters (pagination is added per-page below).
	// Note: We intentionally skip passing labels to the API because GitHub's
	// label query is case-sensitive. Instead, we filter in code after fetching.
	params := []string{}
	var filterLabels []string
	if opts != nil {
		filterLabels = opts.Labels // Save for post-fetch filtering
		if opts.State != "" {
			params = append(params, "state="+opts.State)
		}
		if opts.Sort != "" {
			params = append(params, "sort="+opts.Sort)
		}
		if !opts.Since.IsZero() {
			params = append(params, "since="+opts.Since.Format(time.RFC3339))
		}
	}
	baseQuery := strings.Join(params, "&")

	// Paginate (GitHub defaults to 30/page, caps at 100). Without this the
	// candidate set was silently truncated to the first ~30 issues, breaking the
	// oldest-first dispatch contract on repos with more open issues. TASK-346 (C6).
	var issues []*Issue
	for page := 1; page <= maxPages; page++ {
		path := fmt.Sprintf("/repos/%s/%s/issues?per_page=%d&page=%d", owner, repo, perPage, page)
		if baseQuery != "" {
			path += "&" + baseQuery
		}
		var batch []*Issue
		if err := c.doRequest(ctx, http.MethodGet, path, nil, &batch); err != nil {
			return nil, err
		}
		issues = append(issues, batch...)
		if len(batch) < perPage {
			break
		}
	}

	// Filter by labels case-insensitively
	if len(filterLabels) > 0 {
		var filtered []*Issue
		for _, issue := range issues {
			hasAllLabels := true
			for _, wantLabel := range filterLabels {
				if !HasLabel(issue, wantLabel) {
					hasAllLabels = false
					break
				}
			}
			if hasAllLabels {
				filtered = append(filtered, issue)
			}
		}
		return filtered, nil
	}

	return issues, nil
}

// HasLabel checks if an issue has a specific label (case-insensitive)
func HasLabel(issue *Issue, labelName string) bool {
	for _, label := range issue.Labels {
		if strings.EqualFold(label.Name, labelName) {
			return true
		}
	}
	return false
}

// IssueInput is the input for creating a new issue
type IssueInput struct {
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Labels []string `json:"labels,omitempty"`
}

// CreateIssue creates a new issue in a repository
func (c *Client) CreateIssue(ctx context.Context, owner, repo string, input *IssueInput) (*Issue, error) {
	if !c.IssueCreationEnabled() {
		return nil, ErrIssueCreationDisabled
	}
	path := fmt.Sprintf("/repos/%s/%s/issues", owner, repo)
	var issue Issue
	if err := c.doRequest(ctx, http.MethodPost, path, input, &issue); err != nil {
		return nil, err
	}
	return &issue, nil
}

// GetIssueNodeID fetches the GraphQL node ID for a given issue number via the REST API.
func (c *Client) GetIssueNodeID(ctx context.Context, owner, repo string, number int) (string, error) {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", owner, repo, number)
	var issue struct {
		NodeID string `json:"node_id"`
	}
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &issue); err != nil {
		return "", fmt.Errorf("get issue node ID for %s/%s#%d: %w", owner, repo, number, err)
	}
	if issue.NodeID == "" {
		return "", fmt.Errorf("issue %s/%s#%d returned empty node_id", owner, repo, number)
	}
	return issue.NodeID, nil
}

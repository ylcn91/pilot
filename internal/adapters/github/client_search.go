package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// SearchPRsForIssue returns all PRs that reference the given issue number using the
// GitHub Search API. It searches for PRs in the specified repo that mention #issueNumber.
func (c *Client) SearchPRsForIssue(ctx context.Context, owner, repo string, issueNumber int) ([]*PullRequest, error) {
	q := fmt.Sprintf("repo:%s/%s is:pr #%d", owner, repo, issueNumber)
	path := fmt.Sprintf("/search/issues?q=%s&per_page=100", url.QueryEscape(q))

	var result struct {
		Items []struct {
			ID          int64  `json:"id"`
			Number      int    `json:"number"`
			Title       string `json:"title"`
			State       string `json:"state"`
			HTMLURL     string `json:"html_url"`
			PullRequest *struct {
				MergedAt string `json:"merged_at"`
			} `json:"pull_request"`
		} `json:"items"`
	}
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, fmt.Errorf("search PRs for issue #%d: %w", issueNumber, err)
	}

	prs := make([]*PullRequest, 0, len(result.Items))
	for _, item := range result.Items {
		pr := &PullRequest{
			ID:      item.ID,
			Number:  item.Number,
			Title:   item.Title,
			State:   item.State,
			HTMLURL: item.HTMLURL,
		}
		if item.PullRequest != nil && item.PullRequest.MergedAt != "" {
			pr.MergedAt = item.PullRequest.MergedAt
			pr.Merged = true
		}
		prs = append(prs, pr)
	}
	return prs, nil
}

// SearchMergedPRsForIssue checks if any merged PRs exist that reference the given
// issue number in their title (e.g. "GH-123" pattern). Uses the GitHub Search API.
// Returns true if at least one merged PR is found.
func (c *Client) SearchMergedPRsForIssue(ctx context.Context, owner, repo string, issueNumber int) (bool, error) {
	q := fmt.Sprintf("repo:%s/%s GH-%d in:title is:pr is:merged", owner, repo, issueNumber)
	path := fmt.Sprintf("/search/issues?q=%s&per_page=1", url.QueryEscape(q))

	var result struct {
		TotalCount int `json:"total_count"`
	}
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return false, fmt.Errorf("search merged PRs for issue %d: %w", issueNumber, err)
	}
	return result.TotalCount > 0, nil
}

// FindMergedPRByBranch looks up PRs by head branch via the strongly-consistent REST API
// (no Search API indexing lag). Returns true if any PR on that branch is merged.
// GH-2341: bypasses Search API lag that allowed duplicate dispatch of recently-merged issues.
func (c *Client) FindMergedPRByBranch(ctx context.Context, owner, repo, branch string) (bool, error) {
	head := fmt.Sprintf("%s:%s", owner, branch)
	path := fmt.Sprintf("/repos/%s/%s/pulls?head=%s&state=closed&per_page=10",
		owner, repo, url.QueryEscape(head))

	var prs []*PullRequest
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &prs); err != nil {
		return false, fmt.Errorf("list PRs by branch %s: %w", branch, err)
	}
	for _, pr := range prs {
		if pr.MergedAt != "" || pr.Merged {
			return true, nil
		}
	}
	return false, nil
}

// FindOpenPRByBranch looks up OPEN PRs by head branch via the strongly-consistent
// REST API (no Search API indexing lag). Returns true if any open PR exists on
// that branch. TASK-341: counterpart to FindMergedPRByBranch for the
// PR-created-but-not-yet-merged window, so a re-dispatch no-op can be classified
// as awaiting-merge rather than pilot-blocked.
func (c *Client) FindOpenPRByBranch(ctx context.Context, owner, repo, branch string) (bool, error) {
	head := fmt.Sprintf("%s:%s", owner, branch)
	path := fmt.Sprintf("/repos/%s/%s/pulls?head=%s&state=open&per_page=10",
		owner, repo, url.QueryEscape(head))

	var prs []*PullRequest
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &prs); err != nil {
		return false, fmt.Errorf("list open PRs by branch %s: %w", branch, err)
	}
	// The server filters by head=owner:branch&state=open, so a matching PR has
	// state=="open" and head.ref==branch. Re-check both rather than trusting the
	// array length, mirroring FindMergedPRByBranch's field inspection.
	for _, pr := range prs {
		if pr.State == "open" && pr.Head.Ref == branch {
			return true, nil
		}
	}
	return false, nil
}

// SearchIssuesContaining counts issues in a repo whose body or title contains the
// given literal phrase. It is a thin, marker-oriented wrapper over the Search API
// used by the Architect family to detect an already-filed proposal: the phrase is
// the hidden dedup marker embedded in a previously-created issue body. Both open
// and closed issues are matched so a resolved-then-reopened signal is not re-filed.
//
// The phrase is wrapped in quotes for an exact-substring search and is:issue
// constrains results to issues (not PRs). Returns the total match count.
func (c *Client) SearchIssuesContaining(ctx context.Context, owner, repo, phrase string) (int, error) {
	q := fmt.Sprintf(`repo:%s/%s is:issue %q`, owner, repo, phrase)
	path := fmt.Sprintf("/search/issues?q=%s&per_page=1", url.QueryEscape(q))

	var result struct {
		TotalCount int `json:"total_count"`
	}
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return 0, fmt.Errorf("search issues containing %q in %s/%s: %w", phrase, owner, repo, err)
	}
	return result.TotalCount, nil
}

// SearchOpenSubIssues counts open issues in a repo whose body contains "Parent: GH-{parentNum}".
// Uses the GitHub Search API to find sub-issues referencing the given parent.
func (c *Client) SearchOpenSubIssues(ctx context.Context, owner, repo string, parentNum int) (int, error) {
	q := fmt.Sprintf(`repo:%s/%s "Parent: GH-%d" is:issue is:open`, owner, repo, parentNum)
	path := fmt.Sprintf("/search/issues?q=%s&per_page=1", url.QueryEscape(q))

	var result struct {
		TotalCount int `json:"total_count"`
	}
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return 0, fmt.Errorf("search open sub-issues for parent %d: %w", parentNum, err)
	}
	return result.TotalCount, nil
}

// LinkSubIssue links a child issue to a parent issue using the addSubIssue GraphQL mutation.
// Both issue numbers are resolved to node IDs first.
func (c *Client) LinkSubIssue(ctx context.Context, owner, repo string, parentNum, childNum int) error {
	parentID, err := c.GetIssueNodeID(ctx, owner, repo, parentNum)
	if err != nil {
		return fmt.Errorf("resolve parent node ID: %w", err)
	}
	childID, err := c.GetIssueNodeID(ctx, owner, repo, childNum)
	if err != nil {
		return fmt.Errorf("resolve child node ID: %w", err)
	}

	const mutation = `mutation($parentID: ID!, $childID: ID!) {
		addSubIssue(input: {issueId: $parentID, subIssueId: $childID}) {
			issue { id }
			subIssue { id }
		}
	}`

	variables := map[string]interface{}{
		"parentID": parentID,
		"childID":  childID,
	}
	return c.ExecuteGraphQL(ctx, mutation, variables, nil)
}

// GetOpenSubIssueCount queries native GitHub sub-issues for a parent issue and returns:
//   - count: number of sub-issues in OPEN state
//   - hasNativeLinks: true when the parent has at least one native sub-issue link (totalCount > 0)
//   - error: any API or parsing error
func (c *Client) GetOpenSubIssueCount(ctx context.Context, owner, repo string, parentNum int) (count int, hasNativeLinks bool, err error) {
	parentID, err := c.GetIssueNodeID(ctx, owner, repo, parentNum)
	if err != nil {
		return 0, false, fmt.Errorf("resolve parent node ID: %w", err)
	}

	const query = `query($issueID: ID!) {
		node(id: $issueID) {
			... on Issue {
				subIssues(first: 100) {
					totalCount
					nodes {
						state
					}
				}
			}
		}
	}`

	var result struct {
		Node struct {
			SubIssues struct {
				TotalCount int `json:"totalCount"`
				Nodes      []struct {
					State string `json:"state"`
				} `json:"nodes"`
			} `json:"subIssues"`
		} `json:"node"`
	}

	if err := c.ExecuteGraphQL(ctx, query, map[string]interface{}{"issueID": parentID}, &result); err != nil {
		return 0, false, fmt.Errorf("query sub-issues for %s/%s#%d: %w", owner, repo, parentNum, err)
	}

	if result.Node.SubIssues.TotalCount == 0 {
		return 0, false, nil
	}

	openCount := 0
	for _, n := range result.Node.SubIssues.Nodes {
		if n.State == "OPEN" {
			openCount++
		}
	}
	return openCount, true, nil
}

// SearchOpenPilotIssuesWithSubIssues returns issue numbers for open issues labeled "pilot"
// in the given repo that have at least one sub-issue (subIssuesSummary.total > 0).
// limit controls the maximum number of issues fetched from the API via the GraphQL first argument.
func (c *Client) SearchOpenPilotIssuesWithSubIssues(ctx context.Context, owner, repo string, limit int) ([]int, error) {
	const query = `query($owner: String!, $repo: String!, $first: Int!) {
		repository(owner: $owner, name: $repo) {
			issues(first: $first, states: [OPEN], labels: ["pilot"]) {
				nodes {
					number
					subIssuesSummary {
						total
						completed
					}
				}
			}
		}
	}`

	var result struct {
		Repository struct {
			Issues struct {
				Nodes []struct {
					Number           int `json:"number"`
					SubIssuesSummary struct {
						Total     int `json:"total"`
						Completed int `json:"completed"`
					} `json:"subIssuesSummary"`
				} `json:"nodes"`
			} `json:"issues"`
		} `json:"repository"`
	}

	variables := map[string]interface{}{
		"owner": owner,
		"repo":  repo,
		"first": limit,
	}

	if err := c.ExecuteGraphQL(ctx, query, variables, &result); err != nil {
		return nil, fmt.Errorf("search pilot issues with sub-issues for %s/%s: %w", owner, repo, err)
	}

	var numbers []int
	for _, node := range result.Repository.Issues.Nodes {
		if node.SubIssuesSummary.Total > 0 {
			numbers = append(numbers, node.Number)
		}
	}
	return numbers, nil
}

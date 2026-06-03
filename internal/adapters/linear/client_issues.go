package linear

import (
	"context"
	"fmt"
)

// GetIssue fetches an issue by ID
func (c *Client) GetIssue(ctx context.Context, id string) (*Issue, error) {
	query := `
		query GetIssue($id: String!) {
			issue(id: $id) {
				id
				identifier
				title
				description
				priority
				state {
					id
					name
					type
				}
				labels {
					nodes {
						id
						name
					}
				}
				assignee {
					id
					name
					email
				}
				project {
					id
					name
				}
				team {
					id
					name
					key
				}
				createdAt
				updatedAt
			}
		}
	`

	var result struct {
		Issue issueListItem `json:"issue"`
	}

	if err := c.Execute(ctx, query, map[string]interface{}{"id": id}, &result); err != nil {
		return nil, err
	}

	return result.Issue.toIssue(), nil
}

// UpdateIssueState updates an issue's state
func (c *Client) UpdateIssueState(ctx context.Context, issueID, stateID string) error {
	mutation := `
		mutation UpdateIssue($id: String!, $stateId: String!) {
			issueUpdate(id: $id, input: { stateId: $stateId }) {
				success
			}
		}
	`

	return c.Execute(ctx, mutation, map[string]interface{}{
		"id":      issueID,
		"stateId": stateID,
	}, nil)
}

// AddComment adds a comment to an issue
func (c *Client) AddComment(ctx context.Context, issueID, body string) error {
	mutation := `
		mutation CreateComment($issueId: String!, $body: String!) {
			commentCreate(input: { issueId: $issueId, body: $body }) {
				success
			}
		}
	`

	return c.Execute(ctx, mutation, map[string]interface{}{
		"issueId": issueID,
		"body":    body,
	}, nil)
}

// ListIssuesOptions configures issue listing
type ListIssuesOptions struct {
	TeamID     string
	Label      string
	ProjectIDs []string
	States     []string // e.g., ["backlog", "unstarted", "started"]
}

// ListIssues fetches issues matching the filter criteria
func (c *Client) ListIssues(ctx context.Context, opts *ListIssuesOptions) ([]*Issue, error) {
	query := `
		query ListIssues($teamId: String!, $label: String!, $states: [String!]) {
			issues(
				filter: {
					team: { key: { eq: $teamId } }
					labels: { name: { eq: $label } }
					state: { type: { in: $states } }
				}
				first: 50
				orderBy: createdAt
			) {
				nodes {
					id
					identifier
					title
					description
					priority
					state { id name type }
					labels { nodes { id name } }
					assignee { id name email }
					project { id name }
					team { id name key }
					createdAt
					updatedAt
				}
			}
		}
	`

	states := opts.States
	if len(states) == 0 {
		states = []string{"backlog", "unstarted", "started"}
	}

	variables := map[string]interface{}{
		"teamId": opts.TeamID,
		"label":  opts.Label,
		"states": states,
	}

	var result struct {
		Issues struct {
			Nodes []*issueListItem `json:"nodes"`
		} `json:"issues"`
	}

	if err := c.Execute(ctx, query, variables, &result); err != nil {
		return nil, err
	}

	// Convert responses to Issue objects
	issues := make([]*Issue, 0, len(result.Issues.Nodes))
	for _, resp := range result.Issues.Nodes {
		issue := resp.toIssue()
		// Filter by project if specified
		if len(opts.ProjectIDs) > 0 {
			if issue.Project == nil || !containsString(opts.ProjectIDs, issue.Project.ID) {
				continue
			}
		}
		issues = append(issues, issue)
	}

	return issues, nil
}

// containsString checks if a slice contains a string
func containsString(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// CreateIssue creates a new issue in Linear with team/project context from parent issue.
// This satisfies the SubIssueCreator interface for epic decomposition.
// parentID: Linear issue ID to get team/project context from
// title: Issue title
// body: Issue description (parent reference will be prepended)
// labels: Label names to apply (will call GetOrCreateLabel for each)
// Returns: issueID (Linear identifier like APP-123), issueURL, error
func (c *Client) CreateIssue(ctx context.Context, parentID, title, body string, labels []string) (string, string, error) {
	// Fetch parent issue to get team/project context
	parent, err := c.GetIssue(ctx, parentID)
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch parent issue %s: %w", parentID, err)
	}

	// Build body with parent reference
	bodyWithParent := fmt.Sprintf("Parent: %s\n\n%s", parentID, body)

	// Get or create "Pilot" label
	pilotLabelID, err := c.GetOrCreateLabel(ctx, parent.Team.Key, "Pilot", "#7ec699")
	if err != nil {
		return "", "", fmt.Errorf("failed to get/create Pilot label: %w", err)
	}

	// Collect all label IDs
	labelIDs := []string{pilotLabelID}
	for _, labelName := range labels {
		if labelName != "Pilot" { // Avoid duplicates
			labelID, err := c.GetOrCreateLabel(ctx, parent.Team.Key, labelName, "#8b949e")
			if err != nil {
				return "", "", fmt.Errorf("failed to get/create label %s: %w", labelName, err)
			}
			labelIDs = append(labelIDs, labelID)
		}
	}

	// Create issue using issueCreate mutation. parentId is included in the input
	// so Linear builds a real epic -> sub-issue tree (not just a "Parent:" line in
	// the body); it is only sent when a non-empty parentID was supplied.
	mutation := `
		mutation CreateIssue($teamId: String!, $title: String!, $description: String, $labelIds: [String!], $projectId: String, $parentId: String) {
			issueCreate(input: {
				teamId: $teamId,
				title: $title,
				description: $description,
				labelIds: $labelIds,
				projectId: $projectId,
				parentId: $parentId
			}) {
				success
				issue {
					id
					identifier
					url
				}
			}
		}
	`

	variables := map[string]interface{}{
		"teamId":      parent.Team.ID,
		"title":       title,
		"description": bodyWithParent,
		"labelIds":    labelIDs,
	}

	// Link the new issue under the parent so the tree is real. Set only when a
	// parent was supplied so existing parentless callers pass through unchanged.
	if parentID != "" {
		variables["parentId"] = parentID
	}

	// Include project if parent has one
	if parent.Project != nil {
		variables["projectId"] = parent.Project.ID
	}

	var result struct {
		IssueCreate struct {
			Success bool `json:"success"`
			Issue   struct {
				ID         string `json:"id"`
				Identifier string `json:"identifier"`
				URL        string `json:"url"`
			} `json:"issue"`
		} `json:"issueCreate"`
	}

	if err := c.Execute(ctx, mutation, variables, &result); err != nil {
		return "", "", fmt.Errorf("failed to create issue: %w", err)
	}

	if !result.IssueCreate.Success {
		return "", "", fmt.Errorf("issueCreate returned success=false")
	}

	return result.IssueCreate.Issue.Identifier, result.IssueCreate.Issue.URL, nil
}

// SearchIssuesContaining counts issues whose description contains the given
// literal phrase (the Architect dedup marker). It mirrors the GitHub client's
// method of the same name so the Architect EMIT stage can dedup across runs
// against Linear too. The owner/repo arguments are part of the cross-tracker
// IssueSearcher seam and are ignored here — Linear scopes by API key, not by a
// GitHub-style owner/repo. The Linear `contains` filter is a substring match, so
// the hidden marker embedded in a previously-created issue body round-trips.
func (c *Client) SearchIssuesContaining(ctx context.Context, _, _, phrase string) (int, error) {
	query := `
		query SearchIssues($phrase: String!) {
			issues(filter: { description: { contains: $phrase } }, first: 1) {
				nodes { id }
			}
		}
	`

	var result struct {
		Issues struct {
			Nodes []struct {
				ID string `json:"id"`
			} `json:"nodes"`
		} `json:"issues"`
	}

	if err := c.Execute(ctx, query, map[string]interface{}{"phrase": phrase}, &result); err != nil {
		return 0, fmt.Errorf("search Linear issues containing %q: %w", phrase, err)
	}
	return len(result.Issues.Nodes), nil
}

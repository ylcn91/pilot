package linear

import (
	"context"
	"fmt"
)

// testableCreateIssueClient extends testableClient with CreateIssue support
type testableCreateIssueClient struct {
	*testableClient
}

func newTestableCreateIssueClient(baseURL, apiKey string) *testableCreateIssueClient {
	return &testableCreateIssueClient{
		testableClient: newTestableClient(baseURL, apiKey),
	}
}

func (c *testableCreateIssueClient) CreateIssue(ctx context.Context, parentID, title, body string, labels []string) (string, string, error) {
	// Fetch parent issue to get team/project context
	parent, err := c.getIssue(ctx, parentID)
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch parent issue %s: %w", parentID, err)
	}

	// Build body with parent reference
	bodyWithParent := fmt.Sprintf("Parent: %s\n\n%s", parentID, body)

	// Get or create "Pilot" label
	pilotLabelID, err := c.getOrCreateLabel(ctx, parent.Team.Key, "Pilot", "#7ec699")
	if err != nil {
		return "", "", fmt.Errorf("failed to get/create Pilot label: %w", err)
	}

	// Collect all label IDs
	labelIDs := []string{pilotLabelID}
	for _, labelName := range labels {
		if labelName != "Pilot" { // Avoid duplicates
			labelID, err := c.getOrCreateLabel(ctx, parent.Team.Key, labelName, "#8b949e")
			if err != nil {
				return "", "", fmt.Errorf("failed to get/create label %s: %w", labelName, err)
			}
			labelIDs = append(labelIDs, labelID)
		}
	}

	// Create issue using issueCreate mutation
	mutation := `
		mutation CreateIssue($teamId: String!, $title: String!, $description: String, $labelIds: [String!], $projectId: String) {
			issueCreate(input: {
				teamId: $teamId,
				title: $title,
				description: $description,
				labelIds: $labelIds,
				projectId: $projectId
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

	if err := c.execute(ctx, mutation, variables, &result); err != nil {
		return "", "", fmt.Errorf("failed to create issue: %w", err)
	}

	if !result.IssueCreate.Success {
		return "", "", fmt.Errorf("issueCreate returned success=false")
	}

	return result.IssueCreate.Issue.Identifier, result.IssueCreate.Issue.URL, nil
}

func (c *testableCreateIssueClient) getOrCreateLabel(ctx context.Context, teamKey, labelName, color string) (string, error) {
	id, err := c.getLabelByName(ctx, teamKey, labelName)
	if err == nil {
		return id, nil
	}

	// Label doesn't exist, create it
	return c.createLabel(ctx, teamKey, labelName, color)
}

func (c *testableCreateIssueClient) getLabelByName(ctx context.Context, teamKey, labelName string) (string, error) {
	query := `
		query GetLabel($teamId: String!, $name: String!) {
			issueLabels(filter: { team: { key: { eq: $teamId } }, name: { eq: $name } }) {
				nodes { id name }
			}
		}
	`
	var result struct {
		IssueLabels struct {
			Nodes []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"nodes"`
		} `json:"issueLabels"`
	}

	if err := c.execute(ctx, query, map[string]interface{}{
		"teamId": teamKey,
		"name":   labelName,
	}, &result); err != nil {
		return "", err
	}

	if len(result.IssueLabels.Nodes) == 0 {
		return "", fmt.Errorf("label %q not found in team %s", labelName, teamKey)
	}

	return result.IssueLabels.Nodes[0].ID, nil
}

func (c *testableCreateIssueClient) createLabel(ctx context.Context, teamKey, labelName, color string) (string, error) {
	// For testing, just return a fake label ID
	return "label-" + labelName, nil
}

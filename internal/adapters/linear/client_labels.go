package linear

import (
	"context"
	"fmt"
)

// AddLabel adds a label to an issue
func (c *Client) AddLabel(ctx context.Context, issueID, labelID string) error {
	mutation := `
		mutation AddLabel($issueId: String!, $labelId: String!) {
			issueAddLabel(id: $issueId, labelId: $labelId) {
				success
			}
		}
	`
	return c.Execute(ctx, mutation, map[string]interface{}{
		"issueId": issueID,
		"labelId": labelID,
	}, nil)
}

// RemoveLabel removes a label from an issue
func (c *Client) RemoveLabel(ctx context.Context, issueID, labelID string) error {
	mutation := `
		mutation RemoveLabel($issueId: String!, $labelId: String!) {
			issueRemoveLabel(id: $issueId, labelId: $labelId) {
				success
			}
		}
	`
	return c.Execute(ctx, mutation, map[string]interface{}{
		"issueId": issueID,
		"labelId": labelID,
	}, nil)
}

// GetLabelByName fetches a label ID by name for a team
func (c *Client) GetLabelByName(ctx context.Context, teamID, labelName string) (string, error) {
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

	if err := c.Execute(ctx, query, map[string]interface{}{
		"teamId": teamID,
		"name":   labelName,
	}, &result); err != nil {
		return "", err
	}

	if len(result.IssueLabels.Nodes) == 0 {
		return "", fmt.Errorf("label %q not found in team %s", labelName, teamID)
	}

	return result.IssueLabels.Nodes[0].ID, nil
}

// CreateLabel creates a new label in a team and returns its ID.
// GH-1351: Used to auto-create pilot status labels (pilot-in-progress, pilot-done, pilot-failed).
func (c *Client) CreateLabel(ctx context.Context, teamID, labelName, color string) (string, error) {
	mutation := `
		mutation CreateLabel($teamId: String!, $name: String!, $color: String!) {
			issueLabelCreate(input: { teamId: $teamId, name: $name, color: $color }) {
				success
				issueLabel {
					id
					name
				}
			}
		}
	`

	var result struct {
		IssueLabelCreate struct {
			Success    bool `json:"success"`
			IssueLabel struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"issueLabel"`
		} `json:"issueLabelCreate"`
	}

	if err := c.Execute(ctx, mutation, map[string]interface{}{
		"teamId": teamID,
		"name":   labelName,
		"color":  color,
	}, &result); err != nil {
		return "", err
	}

	if !result.IssueLabelCreate.Success {
		return "", fmt.Errorf("failed to create label %q in team %s", labelName, teamID)
	}

	return result.IssueLabelCreate.IssueLabel.ID, nil
}

// GetOrCreateLabel fetches a label ID by name, creating it if it doesn't exist.
// GH-1351: Ensures pilot status labels exist for deduplication.
func (c *Client) GetOrCreateLabel(ctx context.Context, teamID, labelName, color string) (string, error) {
	id, err := c.GetLabelByName(ctx, teamID, labelName)
	if err == nil {
		return id, nil
	}

	// Label doesn't exist, create it
	return c.CreateLabel(ctx, teamID, labelName, color)
}

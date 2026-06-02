package linear

import (
	"context"
	"fmt"
)

// GetTeamDoneStateID returns the cached "completed" workflow state ID for a team.
// It queries the Linear API on first call and caches the result.
func (c *Client) GetTeamDoneStateID(ctx context.Context, teamKey string) (string, error) {
	// Check cache with read lock
	c.doneStateMu.RLock()
	if id, ok := c.doneStateCache[teamKey]; ok {
		c.doneStateMu.RUnlock()
		return id, nil
	}
	c.doneStateMu.RUnlock()

	// Query Linear API for completed state
	query := `
		query GetTeamDoneState($teamKey: String!) {
			workflowStates(filter: { team: { key: { eq: $teamKey } }, type: { eq: "completed" } }) {
				nodes {
					id
					name
					type
				}
			}
		}
	`

	var result struct {
		WorkflowStates struct {
			Nodes []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"nodes"`
		} `json:"workflowStates"`
	}

	if err := c.Execute(ctx, query, map[string]interface{}{"teamKey": teamKey}, &result); err != nil {
		return "", err
	}

	if len(result.WorkflowStates.Nodes) == 0 {
		return "", fmt.Errorf("no completed state found for team %s", teamKey)
	}

	stateID := result.WorkflowStates.Nodes[0].ID

	// Store in cache with write lock
	c.doneStateMu.Lock()
	c.doneStateCache[teamKey] = stateID
	c.doneStateMu.Unlock()

	return stateID, nil
}

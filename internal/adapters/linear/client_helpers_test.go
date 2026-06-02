package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// testableClient wraps Client methods with custom URL support for testing
type testableClient struct {
	apiKey     string
	httpClient *http.Client
	baseURL    string

	doneStateMu    sync.RWMutex
	doneStateCache map[string]string
}

func newTestableClient(baseURL, apiKey string) *testableClient {
	return &testableClient{
		apiKey:         apiKey,
		httpClient:     &http.Client{Timeout: 30 * time.Second},
		baseURL:        baseURL,
		doneStateCache: make(map[string]string),
	}
}

func (c *testableClient) execute(ctx context.Context, query string, variables map[string]interface{}, result interface{}) error {
	reqBody := GraphQLRequest{
		Query:     query,
		Variables: variables,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API error: %s", string(respBody))
	}

	var gqlResp GraphQLResponse
	if err := json.Unmarshal(respBody, &gqlResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if len(gqlResp.Errors) > 0 {
		return fmt.Errorf("GraphQL error: %s", gqlResp.Errors[0].Message)
	}

	if result != nil {
		if err := json.Unmarshal(gqlResp.Data, result); err != nil {
			return fmt.Errorf("failed to parse data: %w", err)
		}
	}

	return nil
}

// issueResponse matches the Linear GraphQL response structure
type issueResponse struct {
	Issue struct {
		ID          string `json:"id"`
		Identifier  string `json:"identifier"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Priority    int    `json:"priority"`
		State       State  `json:"state"`
		Labels      struct {
			Nodes []Label `json:"nodes"`
		} `json:"labels"`
		Assignee  *User    `json:"assignee"`
		Project   *Project `json:"project"`
		Team      Team     `json:"team"`
		CreatedAt string   `json:"createdAt"`
		UpdatedAt string   `json:"updatedAt"`
	} `json:"issue"`
}

func (c *testableClient) getIssue(ctx context.Context, id string) (*Issue, error) {
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

	var result issueResponse

	if err := c.execute(ctx, query, map[string]interface{}{"id": id}, &result); err != nil {
		return nil, err
	}

	// Convert response to Issue struct
	issue := &Issue{
		ID:          result.Issue.ID,
		Identifier:  result.Issue.Identifier,
		Title:       result.Issue.Title,
		Description: result.Issue.Description,
		Priority:    result.Issue.Priority,
		State:       result.Issue.State,
		Labels:      result.Issue.Labels.Nodes,
		Assignee:    result.Issue.Assignee,
		Project:     result.Issue.Project,
		Team:        result.Issue.Team,
	}

	return issue, nil
}

func (c *testableClient) updateIssueState(ctx context.Context, issueID, stateID string) error {
	mutation := `
		mutation UpdateIssue($id: String!, $stateId: String!) {
			issueUpdate(id: $id, input: { stateId: $stateId }) {
				success
			}
		}
	`

	return c.execute(ctx, mutation, map[string]interface{}{
		"id":      issueID,
		"stateId": stateID,
	}, nil)
}

func (c *testableClient) addComment(ctx context.Context, issueID, body string) error {
	mutation := `
		mutation CreateComment($issueId: String!, $body: String!) {
			commentCreate(input: { issueId: $issueId, body: $body }) {
				success
			}
		}
	`

	return c.execute(ctx, mutation, map[string]interface{}{
		"issueId": issueID,
		"body":    body,
	}, nil)
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func (c *testableClient) getTeamDoneStateID(ctx context.Context, teamKey string) (string, error) {
	// Check cache with read lock
	c.doneStateMu.RLock()
	if id, ok := c.doneStateCache[teamKey]; ok {
		c.doneStateMu.RUnlock()
		return id, nil
	}
	c.doneStateMu.RUnlock()

	// Query API for completed state
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

	if err := c.execute(ctx, query, map[string]interface{}{"teamKey": teamKey}, &result); err != nil {
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

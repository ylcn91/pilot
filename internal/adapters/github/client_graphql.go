package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// GraphQLRequest is a GitHub GraphQL API request body.
type GraphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

// GraphQLResponse is a GitHub GraphQL API response envelope.
type GraphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []GraphQLError  `json:"errors,omitempty"`
}

// GraphQLError is a single error from a GraphQL response.
type GraphQLError struct {
	Message string `json:"message"`
}

// ExecuteGraphQL executes a GitHub GraphQL query or mutation.
// Posts to baseURL+"/graphql" (testable via NewClientWithBaseURL).
// result is unmarshalled from response.data if non-nil.
// Transient transport errors (5xx, network) and GraphQL-level rate limits
// (HTTP 200 + RATE_LIMITED / "was submitted too quickly") are retried via
// c.retryOpts, matching the behaviour of doRequest.
func (c *Client) ExecuteGraphQL(ctx context.Context, query string, variables map[string]interface{}, result interface{}) error {
	reqBody := GraphQLRequest{Query: query, Variables: variables}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal graphql request: %w", err)
	}

	endpoint := c.baseURL + "/graphql"

	return WithRetryVoid(ctx, func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
		if err != nil {
			return fmt.Errorf("create graphql request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("graphql request failed: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("read graphql response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("graphql API error (status %d): %s", resp.StatusCode, string(respBody))
		}

		var gqlResp GraphQLResponse
		if err := json.Unmarshal(respBody, &gqlResp); err != nil {
			return fmt.Errorf("parse graphql response: %w", err)
		}

		if len(gqlResp.Errors) > 0 {
			return fmt.Errorf("graphql error: %s", gqlResp.Errors[0].Message)
		}

		if result != nil && len(gqlResp.Data) > 0 {
			if err := json.Unmarshal(gqlResp.Data, result); err != nil {
				return fmt.Errorf("unmarshal graphql data: %w", err)
			}
		}

		return nil
	}, c.retryOpts)
}

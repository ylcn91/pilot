package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeProjectSourceServer returns an httptest.Server that serves a fixed sequence of
// GraphQL responses. Each call to the /graphql endpoint pops the next response.
func fakeProjectSourceServer(t *testing.T, responses []string) *httptest.Server {
	t.Helper()
	idx := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if idx >= len(responses) {
			t.Errorf("unexpected GraphQL request #%d (only %d responses configured)", idx+1, len(responses))
			http.Error(w, "no more responses", http.StatusInternalServerError)
			return
		}
		resp := responses[idx]
		idx++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(resp))
	}))
}

// orgProjectResp returns a project-by-org GraphQL response with the given project ID.
func orgProjectResp(projectID string) string {
	return `{"data":{"organization":{"projectV2":{"id":"` + projectID + `"}}}}`
}

// itemsResp builds a single-page project items response for the given nodes.
// hasNextPage controls pagination; endCursor is only meaningful when hasNextPage=true.
func itemsResp(nodes []map[string]interface{}, hasNextPage bool, endCursor string) string {
	data := map[string]interface{}{
		"node": map[string]interface{}{
			"items": map[string]interface{}{
				"pageInfo": map[string]interface{}{
					"hasNextPage": hasNextPage,
					"endCursor":   endCursor,
				},
				"nodes": nodes,
			},
		},
	}
	b, _ := json.Marshal(map[string]interface{}{"data": data})
	return string(b)
}

// issueNode builds a projectBoardItemNode JSON representation.
func issueNode(number int, nodeID, title, body, state, repo, status string, labelNames ...string) map[string]interface{} {
	labelNodes := make([]map[string]interface{}, len(labelNames))
	for i, l := range labelNames {
		labelNodes[i] = map[string]interface{}{"name": l}
	}
	return map[string]interface{}{
		"content": map[string]interface{}{
			"number": number,
			"id":     nodeID,
			"title":  title,
			"body":   body,
			"state":  state,
			"labels": map[string]interface{}{"nodes": labelNodes},
			"repository": map[string]interface{}{
				"nameWithOwner": repo,
			},
		},
		"fieldValueByName": map[string]interface{}{
			"name": status,
		},
	}
}

// issueNodeWithCreatedAt is like issueNode but includes a createdAt RFC3339 timestamp.
func issueNodeWithCreatedAt(number int, nodeID, title, body, state, repo, status, createdAt string, labelNames ...string) map[string]interface{} {
	node := issueNode(number, nodeID, title, body, state, repo, status, labelNames...)
	node["content"].(map[string]interface{})["createdAt"] = createdAt
	return node
}

// draftNode builds a non-issue item (number=0, no content fields set).
func draftNode() map[string]interface{} {
	return map[string]interface{}{
		"content":          map[string]interface{}{},
		"fieldValueByName": map[string]interface{}{"name": "Todo"},
	}
}

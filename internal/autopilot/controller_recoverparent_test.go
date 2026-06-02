package autopilot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

func TestRecoverStaleParentIssues(t *testing.T) {
	type parentCandidate struct {
		num      int
		openSubs int // open sub-issue count from GetOpenSubIssueCount
		subErr   bool
	}

	tests := []struct {
		name        string
		candidates  []parentCandidate // issues returned by SearchOpenPilotIssuesWithSubIssues
		searchErr   bool
		wantClosed  []int
		wantSkipped []int
	}{
		{
			name: "closes orphaned parent with all sub-issues done",
			candidates: []parentCandidate{
				{num: 100, openSubs: 0},
			},
			wantClosed: []int{100},
		},
		{
			name: "skips parent with open siblings",
			candidates: []parentCandidate{
				{num: 100, openSubs: 2},
			},
			wantSkipped: []int{100},
		},
		{
			name:        "no-op when search returns no candidates",
			candidates:  []parentCandidate{},
			wantClosed:  nil,
			wantSkipped: nil,
		},
		{
			name: "continues on per-item error, closes others",
			candidates: []parentCandidate{
				{num: 100, subErr: true},
				{num: 200, openSubs: 0},
			},
			wantClosed:  []int{200},
			wantSkipped: []int{100},
		},
		{
			name:      "search error - no-op",
			searchErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			closedParents := map[int]bool{}
			labeledParents := map[int]bool{}

			// Build a map from parent number to candidate for quick lookup.
			candidateMap := map[int]parentCandidate{}
			for _, c := range tt.candidates {
				candidateMap[c.num] = c
			}

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/graphql" && r.Method == http.MethodPost:
					var body map[string]interface{}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					query, _ := body["query"].(string)

					if strings.Contains(query, "subIssuesSummary") {
						// SearchOpenPilotIssuesWithSubIssues
						if tt.searchErr {
							w.WriteHeader(http.StatusOK)
							_, _ = w.Write([]byte(`{"errors":[{"message":"search failed"}]}`))
							return
						}
						nodes := make([]map[string]interface{}, 0, len(tt.candidates))
						for _, c := range tt.candidates {
							nodes = append(nodes, map[string]interface{}{
								"number": c.num,
								"subIssuesSummary": map[string]int{
									"total":     1,
									"completed": 1,
								},
							})
						}
						resp := map[string]interface{}{
							"data": map[string]interface{}{
								"repository": map[string]interface{}{
									"issues": map[string]interface{}{
										"nodes": nodes,
									},
								},
							},
						}
						w.WriteHeader(http.StatusOK)
						_ = json.NewEncoder(w).Encode(resp)

					} else if strings.Contains(query, "subIssues") {
						// GetOpenSubIssueCount — determine parent num from variables
						vars, _ := body["variables"].(map[string]interface{})
						issueID, _ := vars["issueID"].(string)
						// issueID format: "node_<num>"
						var parentNum int
						_, _ = fmt.Sscanf(issueID, "node_%d", &parentNum)

						cand, ok := candidateMap[parentNum]
						if !ok || cand.subErr {
							w.WriteHeader(http.StatusOK)
							_, _ = w.Write([]byte(`{"errors":[{"message":"sub-issue count failed"}]}`))
							return
						}

						states := make([]map[string]string, cand.openSubs)
						for i := range states {
							states[i] = map[string]string{"state": "OPEN"}
						}
						resp := map[string]interface{}{
							"data": map[string]interface{}{
								"node": map[string]interface{}{
									"subIssues": map[string]interface{}{
										"totalCount": cand.openSubs + 1,
										"nodes":      states,
									},
								},
							},
						}
						w.WriteHeader(http.StatusOK)
						_ = json.NewEncoder(w).Encode(resp)
					}

				case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/owner/repo/issues/"):
					// GetIssueNodeID REST call — extract issue number
					path := strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/issues/")
					var num int
					_, _ = fmt.Sscanf(path, "%d", &num)
					w.WriteHeader(http.StatusOK)
					_, _ = fmt.Fprintf(w, `{"node_id":"node_%d","number":%d}`, num, num)

				case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/labels"):
					// AddLabels
					var num int
					path := strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/issues/")
					_, _ = fmt.Sscanf(path, "%d", &num)
					labeledParents[num] = true
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("[]"))

				case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/labels/"):
					w.WriteHeader(http.StatusOK)

				case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/comments"):
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{"id":1}`))

				case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/repos/owner/repo/issues/"):
					path := strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/issues/")
					var num int
					_, _ = fmt.Sscanf(path, "%d", &num)
					closedParents[num] = true
					w.WriteHeader(http.StatusOK)

				default:
					w.WriteHeader(http.StatusOK)
				}
			}))
			defer server.Close()

			ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			cfg := DefaultConfig()
			c := NewController(cfg, ghClient, nil, "owner", "repo")

			c.recoverStaleParentIssues(context.Background())

			for _, num := range tt.wantClosed {
				if !closedParents[num] {
					t.Errorf("parent %d: expected to be closed, was not", num)
				}
			}
			for _, num := range tt.wantSkipped {
				if closedParents[num] {
					t.Errorf("parent %d: expected to be skipped, was closed", num)
				}
			}
		})
	}
}

func TestRecoverStaleParentIssues_TruncatesAt50(t *testing.T) {
	// Build 50 candidates so SearchOpenPilotIssuesWithSubIssues returns exactly maxRecover=50,
	// triggering the "hit limit" Info log. We don't test the log directly; just
	// verify the sweep still processes all 50 and closes them without error.
	const maxRecover = 50
	candidates := make([]int, maxRecover)
	for i := range candidates {
		candidates[i] = 1000 + i
	}

	closedCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/graphql" && r.Method == http.MethodPost:
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			query, _ := body["query"].(string)

			if strings.Contains(query, "subIssuesSummary") {
				nodes := make([]map[string]interface{}, maxRecover)
				for i, num := range candidates {
					nodes[i] = map[string]interface{}{
						"number":           num,
						"subIssuesSummary": map[string]int{"total": 1, "completed": 1},
					}
				}
				resp := map[string]interface{}{
					"data": map[string]interface{}{
						"repository": map[string]interface{}{
							"issues": map[string]interface{}{"nodes": nodes},
						},
					},
				}
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(resp)
			} else {
				// GetOpenSubIssueCount — all sub-issues closed
				resp := map[string]interface{}{
					"data": map[string]interface{}{
						"node": map[string]interface{}{
							"subIssues": map[string]interface{}{
								"totalCount": 1,
								"nodes":      []map[string]string{{"state": "CLOSED"}},
							},
						},
					},
				}
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(resp)
			}

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/owner/repo/issues/"):
			path := strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/issues/")
			var num int
			_, _ = fmt.Sscanf(path, "%d", &num)
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, `{"node_id":"node_%d","number":%d}`, num, num)

		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/repos/owner/repo/issues/"):
			closedCount++
			w.WriteHeader(http.StatusOK)

		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	c := NewController(cfg, ghClient, nil, "owner", "repo")

	c.recoverStaleParentIssues(context.Background())

	if closedCount != maxRecover {
		t.Errorf("closed %d parents, want %d", closedCount, maxRecover)
	}
}

package autopilot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

func TestMaybeCloseParentIssue(t *testing.T) {
	tests := []struct {
		name             string
		issueNumber      int
		issueBody        string
		openSubIssues    int // used by text-search fallback
		getIssueErr      bool
		searchErr        bool
		nativeTotal      int      // totalCount returned by GraphQL native sub-issues
		nativeOpenStates []string // states of natively linked sub-issues
		wantClosed       bool
		wantLabeled      bool
		wantCommented    bool
	}{
		{
			name:          "last sub-issue triggers parent close (text-search path)",
			issueNumber:   10,
			issueBody:     "Fix the bug\n\nParent: GH-5\n",
			openSubIssues: 0,
			wantClosed:    true,
			wantLabeled:   true,
			wantCommented: true,
		},
		{
			name:          "sibling still open - no-op (text-search path)",
			issueNumber:   10,
			issueBody:     "Fix the bug\n\nParent: GH-5\n",
			openSubIssues: 2,
			wantClosed:    false,
			wantLabeled:   false,
			wantCommented: false,
		},
		{
			name:          "no parent reference - no-op",
			issueNumber:   10,
			issueBody:     "Standalone issue with no parent",
			openSubIssues: 0,
			wantClosed:    false,
			wantLabeled:   false,
			wantCommented: false,
		},
		{
			name:        "no issue number - no-op",
			issueNumber: 0,
			wantClosed:  false,
		},
		{
			name:        "GetIssue API error - graceful no-op",
			issueNumber: 10,
			getIssueErr: true,
			wantClosed:  false,
		},
		{
			name:          "SearchOpenSubIssues API error - graceful no-op",
			issueNumber:   10,
			issueBody:     "Fix the bug\n\nParent: GH-5\n",
			searchErr:     true,
			wantClosed:    false,
			wantLabeled:   false,
			wantCommented: false,
		},
		{
			name:          "label cleanup removes pilot-failed",
			issueNumber:   10,
			issueBody:     "Fix the bug\n\nParent: GH-5\n",
			openSubIssues: 0,
			wantClosed:    true,
			wantLabeled:   true,
			wantCommented: true,
		},
		{
			name:             "native links all closed - closes parent",
			issueNumber:      10,
			issueBody:        "Fix the bug\n\nParent: GH-5\n",
			nativeTotal:      2,
			nativeOpenStates: []string{"CLOSED", "CLOSED"},
			wantClosed:       true,
			wantLabeled:      true,
			wantCommented:    true,
		},
		{
			name:             "native links with open sibling - no-op",
			issueNumber:      10,
			issueBody:        "Fix the bug\n\nParent: GH-5\n",
			nativeTotal:      2,
			nativeOpenStates: []string{"OPEN", "CLOSED"},
			wantClosed:       false,
			wantLabeled:      false,
			wantCommented:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				closeCalled      bool
				addLabelsCalled  bool
				removeLabelCalls []string
				commentCalled    bool
			)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/repos/owner/repo/issues/10" && r.Method == http.MethodGet:
					if tt.getIssueErr {
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					issue := github.Issue{
						Number: 10,
						Body:   tt.issueBody,
						State:  "closed",
					}
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(issue)

				case r.URL.Path == "/repos/owner/repo/issues/5" && r.Method == http.MethodGet:
					// Return node_id for GetIssueNodeID call in native sub-issue path.
					if tt.nativeTotal > 0 {
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`{"node_id":"I_parent_node","number":5}`))
					} else {
						// No native links — return empty body so GetIssueNodeID fails and falls back to text search.
						w.WriteHeader(http.StatusOK)
					}

				case r.URL.Path == "/graphql" && r.Method == http.MethodPost:
					// Serve native sub-issues GraphQL response in node(id:) format used by GetOpenSubIssueCount.
					nodes := make([]map[string]string, len(tt.nativeOpenStates))
					for i, s := range tt.nativeOpenStates {
						nodes[i] = map[string]string{"state": s}
					}
					resp := map[string]interface{}{
						"data": map[string]interface{}{
							"node": map[string]interface{}{
								"subIssues": map[string]interface{}{
									"totalCount": tt.nativeTotal,
									"nodes":      nodes,
								},
							},
						},
					}
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(resp)

				case strings.HasPrefix(r.URL.Path, "/search/issues"):
					if tt.searchErr {
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					resp := struct {
						TotalCount int `json:"total_count"`
					}{TotalCount: tt.openSubIssues}
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(resp)

				case r.URL.Path == "/repos/owner/repo/issues/5/labels" && r.Method == http.MethodPost:
					addLabelsCalled = true
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("[]"))

				case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/issues/5/labels/") && r.Method == http.MethodDelete:
					label := strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/issues/5/labels/")
					removeLabelCalls = append(removeLabelCalls, label)
					w.WriteHeader(http.StatusOK)

				case r.URL.Path == "/repos/owner/repo/issues/5/comments" && r.Method == http.MethodPost:
					commentCalled = true
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{"id":1}`))

				case r.URL.Path == "/repos/owner/repo/issues/5" && r.Method == http.MethodPatch:
					closeCalled = true
					w.WriteHeader(http.StatusOK)

				default:
					w.WriteHeader(http.StatusOK)
				}
			}))
			defer server.Close()

			ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			cfg := DefaultConfig()
			c := NewController(cfg, ghClient, nil, "owner", "repo")

			prState := &PRState{
				PRNumber:    42,
				IssueNumber: tt.issueNumber,
			}

			c.maybeCloseParentIssue(context.Background(), prState)

			if closeCalled != tt.wantClosed {
				t.Errorf("parent closed = %v, want %v", closeCalled, tt.wantClosed)
			}
			if addLabelsCalled != tt.wantLabeled {
				t.Errorf("pilot-done label added = %v, want %v", addLabelsCalled, tt.wantLabeled)
			}
			if commentCalled != tt.wantCommented {
				t.Errorf("comment posted = %v, want %v", commentCalled, tt.wantCommented)
			}
			if tt.wantLabeled {
				// Verify stale labels were removed
				expectedRemoved := map[string]bool{"pilot-failed": false, "pilot-in-progress": false, "pilot-blocked": false}
				for _, label := range removeLabelCalls {
					expectedRemoved[label] = true
				}
				for label, removed := range expectedRemoved {
					if !removed {
						t.Errorf("expected label %q to be removed, but it wasn't", label)
					}
				}
			}
		})
	}
}

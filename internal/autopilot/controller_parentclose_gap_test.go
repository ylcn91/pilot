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

// TestMaybeCloseParentIssue_NativeLinksAbsentFallsBackToSearch covers the gap
// in the tier-1 → tier-2 transition that the existing TestMaybeCloseParentIssue
// does NOT exercise: the case where the parent issue DOES resolve a native node
// ID, but the native sub-issues GraphQL reports totalCount==0.
//
// GetOpenSubIssueCount returns (0, hasNativeLinks=false, err=nil) in that case
// (see client_search.go). The existing "text-search path" subtests instead make
// GetIssueNodeID fail (empty issue body) so the fallback is reached via the
// err!=nil branch (the WARN log). This test reaches the OTHER, error-free
// fallback branch (the DEBUG log), then confirms the text search still drives
// the close decision.
//
// Two sub-cases: text search finds no open siblings (parent closes) and text
// search finds an open sibling (parent stays open). (GH-2086 / tier-2 fallback)
func TestMaybeCloseParentIssue_NativeLinksAbsentFallsBackToSearch(t *testing.T) {
	tests := []struct {
		name          string
		searchOpen    int
		wantClosed    bool
		wantCommented bool
	}{
		{name: "no open siblings via search - parent closes", searchOpen: 0, wantClosed: true, wantCommented: true},
		{name: "open sibling via search - parent stays open", searchOpen: 1, wantClosed: false, wantCommented: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				closeCalled  bool
				commentCalled bool
				searchCalled bool
			)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/repos/owner/repo/issues/10" && r.Method == http.MethodGet:
					// Sub-issue body references parent GH-5.
					issue := github.Issue{Number: 10, Body: "Fix the bug\n\nParent: GH-5\n", State: "closed"}
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(issue)

				case r.URL.Path == "/repos/owner/repo/issues/5" && r.Method == http.MethodGet:
					// Parent RESOLVES a node_id, so GetIssueNodeID succeeds and the
					// native GraphQL query runs (unlike the existing tests, which
					// return an empty body here to force the err!=nil fallback).
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{"node_id":"I_parent_node","number":5}`))

				case r.URL.Path == "/graphql" && r.Method == http.MethodPost:
					// Native sub-issues report totalCount==0 => GetOpenSubIssueCount
					// returns hasNativeLinks=false, err=nil (the clean fallback branch).
					resp := map[string]interface{}{
						"data": map[string]interface{}{
							"node": map[string]interface{}{
								"subIssues": map[string]interface{}{
									"totalCount": 0,
									"nodes":      []map[string]string{},
								},
							},
						},
					}
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(resp)

				case strings.HasPrefix(r.URL.Path, "/search/issues"):
					searchCalled = true
					resp := struct {
						TotalCount int `json:"total_count"`
					}{TotalCount: tt.searchOpen}
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(resp)

				case r.URL.Path == "/repos/owner/repo/issues/5/labels" && r.Method == http.MethodPost:
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("[]"))

				case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/issues/5/labels/") && r.Method == http.MethodDelete:
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

			prState := &PRState{PRNumber: 42, IssueNumber: 10}
			c.maybeCloseParentIssue(context.Background(), prState)

			if !searchCalled {
				t.Fatal("text-search fallback (SearchOpenSubIssues) was not reached; the native totalCount==0 branch must fall through to search")
			}
			if closeCalled != tt.wantClosed {
				t.Errorf("parent closed = %v, want %v", closeCalled, tt.wantClosed)
			}
			if commentCalled != tt.wantCommented {
				t.Errorf("comment posted = %v, want %v", commentCalled, tt.wantCommented)
			}
		})
	}
}

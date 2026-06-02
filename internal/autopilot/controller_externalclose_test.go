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

// TestNotifyExternalClose_MaybeCloseParent verifies that notifyExternalClose
// calls maybeCloseParentIssue so parent epics are auto-closed when the last
// sub-issue PR is closed without merge (GH-2198).
func TestNotifyExternalClose_MaybeCloseParent(t *testing.T) {
	tests := []struct {
		name             string
		issueNumber      int
		issueBody        string
		openSubIssues    int
		wantParentClosed bool
	}{
		{
			name:             "last sub-issue PR closed externally - parent closes",
			issueNumber:      10,
			issueBody:        "Fix the bug\n\nParent: GH-5\n",
			openSubIssues:    0,
			wantParentClosed: true,
		},
		{
			name:             "non-last sub-issue - parent stays open",
			issueNumber:      10,
			issueBody:        "Fix the bug\n\nParent: GH-5\n",
			openSubIssues:    1,
			wantParentClosed: false,
		},
		{
			name:             "non-sub-issue - no parent lookup",
			issueNumber:      10,
			issueBody:        "Standalone issue",
			openSubIssues:    0,
			wantParentClosed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var parentCloseCalled bool

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				// Label/remove calls for the sub-issue itself (notifyExternalClose)
				case r.URL.Path == "/repos/owner/repo/issues/10/labels" && r.Method == http.MethodPost:
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("[]"))
				case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/issues/10/labels/") && r.Method == http.MethodDelete:
					w.WriteHeader(http.StatusOK)

				// maybeCloseParentIssue: fetch sub-issue body
				case r.URL.Path == "/repos/owner/repo/issues/10" && r.Method == http.MethodGet:
					issue := github.Issue{Number: 10, Body: tt.issueBody}
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(issue)

				// maybeCloseParentIssue: count open siblings
				case strings.HasPrefix(r.URL.Path, "/search/issues"):
					resp := struct {
						TotalCount int `json:"total_count"`
					}{TotalCount: tt.openSubIssues}
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(resp)

				// maybeCloseParentIssue: close parent
				case r.URL.Path == "/repos/owner/repo/issues/5" && r.Method == http.MethodPatch:
					parentCloseCalled = true
					w.WriteHeader(http.StatusOK)

				// parent label / comment calls
				case r.URL.Path == "/repos/owner/repo/issues/5/labels" && r.Method == http.MethodPost:
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("[]"))
				case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/issues/5/labels/") && r.Method == http.MethodDelete:
					w.WriteHeader(http.StatusOK)
				case r.URL.Path == "/repos/owner/repo/issues/5/comments" && r.Method == http.MethodPost:
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{"id":1}`))

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

			c.notifyExternalClose(context.Background(), prState)

			if parentCloseCalled != tt.wantParentClosed {
				t.Errorf("parent closed = %v, want %v", parentCloseCalled, tt.wantParentClosed)
			}
		})
	}
}

// GH-2340: notifyExternalClose must not stamp pilot-retry-ready on issues
// that already carry pilot-done. This happens when Pilot itself closed a
// duplicate PR after the original PR was merged — the issue is closed and
// done, and adding pilot-retry-ready strands the label forever (poller
// skips non-open issues).
func TestNotifyExternalClose_SkipsRetryReadyWhenDone(t *testing.T) {
	tests := []struct {
		name           string
		issueLabels    []github.Label
		wantRetryAdded bool
	}{
		{
			name:           "issue already pilot-done - skip retry-ready",
			issueLabels:    []github.Label{{Name: github.LabelDone}},
			wantRetryAdded: false,
		},
		{
			name:           "issue not done - add retry-ready",
			issueLabels:    []github.Label{},
			wantRetryAdded: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var retryReadyAdded bool

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/repos/owner/repo/issues/10" && r.Method == http.MethodGet:
					issue := github.Issue{Number: 10, State: "closed", Labels: tt.issueLabels}
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(issue)

				case r.URL.Path == "/repos/owner/repo/issues/10/labels" && r.Method == http.MethodPost:
					var body struct {
						Labels []string `json:"labels"`
					}
					_ = json.NewDecoder(r.Body).Decode(&body)
					for _, l := range body.Labels {
						if l == github.LabelRetryReady {
							retryReadyAdded = true
						}
					}
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("[]"))

				case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/issues/10/labels/") && r.Method == http.MethodDelete:
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
			c.notifyExternalClose(context.Background(), prState)

			if retryReadyAdded != tt.wantRetryAdded {
				t.Errorf("pilot-retry-ready added = %v, want %v", retryReadyAdded, tt.wantRetryAdded)
			}
		})
	}
}

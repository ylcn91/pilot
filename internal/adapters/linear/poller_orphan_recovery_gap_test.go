package linear

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

// orphanRecoveryServer is a hermetic Linear GraphQL stub that answers the two
// requests recoverOrphanedIssues makes against the real *Client: the ListIssues
// query (filtered on the pilot-in-progress label) and the issueRemoveLabel
// mutation. It dispatches on the GraphQL query text and records every removal it
// receives so the test can assert exactly which issues had their in-progress
// label stripped.
type orphanRecoveryServer struct {
	mu sync.Mutex

	// listNodes is the raw JSON for the issues.nodes array returned by ListIssues.
	listNodes string
	// listErr, when set, makes the ListIssues query return a GraphQL error.
	listErr string
	// removeFailIDs is the set of issueIds whose removal mutation should fail.
	removeFailIDs map[string]bool

	// removed records the issueIds for which a successful removal was requested.
	removed []string
	// listCalls / removeCalls count the dispatched operations.
	listCalls   int
	removeCalls int
}

func (s *orphanRecoveryServer) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		var req GraphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request body: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		switch {
		case contains(req.Query, "issueRemoveLabel"):
			s.mu.Lock()
			s.removeCalls++
			issueID, _ := req.Variables["issueId"].(string)
			fail := s.removeFailIDs[issueID]
			if !fail {
				s.removed = append(s.removed, issueID)
			}
			s.mu.Unlock()

			if fail {
				_ = json.NewEncoder(w).Encode(GraphQLResponse{
					Errors: []GraphQLError{{Message: "cannot remove label"}},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(GraphQLResponse{
				Data: json.RawMessage(`{"issueRemoveLabel": {"success": true}}`),
			})

		case contains(req.Query, "issues("):
			s.mu.Lock()
			s.listCalls++
			listErr := s.listErr
			nodes := s.listNodes
			s.mu.Unlock()

			if listErr != "" {
				_ = json.NewEncoder(w).Encode(GraphQLResponse{
					Errors: []GraphQLError{{Message: listErr}},
				})
				return
			}
			if nodes == "" {
				nodes = "[]"
			}
			_ = json.NewEncoder(w).Encode(GraphQLResponse{
				Data: json.RawMessage(`{"issues": {"nodes": ` + nodes + `}}`),
			})

		default:
			t.Errorf("unexpected GraphQL query: %s", req.Query)
			http.Error(w, "unexpected query", http.StatusInternalServerError)
		}
	}
}

func (s *orphanRecoveryServer) removedIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.removed))
	copy(out, s.removed)
	return out
}

func TestRecoverOrphanedIssues(t *testing.T) {
	const inProgressID = "label-in-progress"

	// A two-issue in-progress listing reused by several cases.
	twoOrphans := `[
		{"id": "issue-1", "identifier": "TST-1", "title": "Orphan one", "labels": {"nodes": [{"id": "label-in-progress", "name": "pilot-in-progress"}]}, "team": {"id": "team-1", "key": "TST"}},
		{"id": "issue-2", "identifier": "TST-2", "title": "Orphan two", "labels": {"nodes": [{"id": "label-in-progress", "name": "pilot-in-progress"}]}, "team": {"id": "team-1", "key": "TST"}}
	]`

	tests := []struct {
		name string
		// inProgressLabelID seeds the poller field that gates recovery.
		inProgressLabelID string
		listNodes         string
		listErr           string
		removeFailIDs     map[string]bool
		// seedProcessed are issue IDs pre-marked as processed (and in the store).
		seedProcessed []string

		wantListCalls   int
		wantRemovedIDs  []string
		wantStillProc   []string // issue IDs that must remain processed afterwards
		wantClearedProc []string // issue IDs that must be cleared from processed
	}{
		{
			name:              "no in-progress label id skips recovery entirely",
			inProgressLabelID: "",
			listNodes:         twoOrphans,
			seedProcessed:     []string{"issue-1"},
			wantListCalls:     0,
			wantRemovedIDs:    nil,
			wantStillProc:     []string{"issue-1"},
		},
		{
			name:              "list error returns without removing labels",
			inProgressLabelID: inProgressID,
			listErr:           "boom",
			seedProcessed:     []string{"issue-1"},
			wantListCalls:     1,
			wantRemovedIDs:    nil,
			wantStillProc:     []string{"issue-1"},
		},
		{
			name:              "no orphaned issues is a no-op",
			inProgressLabelID: inProgressID,
			listNodes:         "[]",
			seedProcessed:     []string{"issue-1"},
			wantListCalls:     1,
			wantRemovedIDs:    nil,
			wantStillProc:     []string{"issue-1"},
		},
		{
			name:              "recovers each orphan: removes label and clears processed",
			inProgressLabelID: inProgressID,
			listNodes:         twoOrphans,
			seedProcessed:     []string{"issue-1", "issue-2"},
			wantListCalls:     1,
			wantRemovedIDs:    []string{"issue-1", "issue-2"},
			wantClearedProc:   []string{"issue-1", "issue-2"},
		},
		{
			name:              "removal failure for one issue keeps it processed, recovers the rest",
			inProgressLabelID: inProgressID,
			listNodes:         twoOrphans,
			removeFailIDs:     map[string]bool{"issue-1": true},
			seedProcessed:     []string{"issue-1", "issue-2"},
			wantListCalls:     1,
			wantRemovedIDs:    []string{"issue-2"},
			wantStillProc:     []string{"issue-1"},
			wantClearedProc:   []string{"issue-2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &orphanRecoveryServer{
				listNodes:     tt.listNodes,
				listErr:       tt.listErr,
				removeFailIDs: tt.removeFailIDs,
			}
			server := httptest.NewServer(stub.handler(t))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeLinearAPIKey, server.URL)
			store := newMockProcessedStore()
			config := &WorkspaceConfig{TeamID: "TST", PilotLabel: "pilot"}

			poller := NewPoller(client, config, 30, WithProcessedStore(store))
			poller.inProgressLabelID = tt.inProgressLabelID

			for _, id := range tt.seedProcessed {
				poller.markProcessed(id)
			}

			poller.recoverOrphanedIssues(context.Background())

			if stub.listCalls != tt.wantListCalls {
				t.Errorf("ListIssues calls = %d, want %d", stub.listCalls, tt.wantListCalls)
			}

			gotRemoved := stub.removedIDs()
			if !equalUnordered(gotRemoved, tt.wantRemovedIDs) {
				t.Errorf("removed label issue IDs = %v, want %v", gotRemoved, tt.wantRemovedIDs)
			}

			for _, id := range tt.wantStillProc {
				if !poller.IsProcessed(id) {
					t.Errorf("issue %s should still be processed", id)
				}
				ok, _ := store.IsProcessed("linear", "", id)
				if !ok {
					t.Errorf("issue %s should still be in store", id)
				}
			}

			for _, id := range tt.wantClearedProc {
				if poller.IsProcessed(id) {
					t.Errorf("issue %s should have been cleared from processed", id)
				}
				ok, _ := store.IsProcessed("linear", "", id)
				if ok {
					t.Errorf("issue %s should have been unmarked in store", id)
				}
			}
		})
	}
}

func equalUnordered(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, s := range a {
		counts[s]++
	}
	for _, s := range b {
		counts[s]--
		if counts[s] < 0 {
			return false
		}
	}
	return true
}

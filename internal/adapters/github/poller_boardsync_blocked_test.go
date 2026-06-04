package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

// statusMutationRecorder records the Status option IDs targeted by each board
// mutation. Appends happen from httptest handler goroutines, so reads/writes
// are mutex-guarded.
type statusMutationRecorder struct {
	mu  sync.Mutex
	ids []string
}

func (r *statusMutationRecorder) add(id string) {
	r.mu.Lock()
	r.ids = append(r.ids, id)
	r.mu.Unlock()
}

func (r *statusMutationRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.ids))
	copy(out, r.ids)
	return out
}

// blockedBoardServer serves the board-sync GraphQL plus the issue list, and
// records which Status option ID each updateProjectV2ItemFieldValue mutation
// targeted. The field exposes In Progress / Done / Blocked options.
func blockedBoardServer(t *testing.T, issues []*Issue, freshIssue *Issue, rec *statusMutationRecorder) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/issues":
			_ = json.NewEncoder(w).Encode(issues)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/issues/1":
			_ = json.NewEncoder(w).Encode(freshIssue)
		case r.Method == http.MethodGet && r.URL.Path == "/search/issues":
			_, _ = w.Write([]byte(`{"total_count":0}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/pulls":
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/graphql":
			var req GraphQLRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			var resp string
			switch {
			case strings.Contains(req.Query, "organization"):
				resp = `{"data":{"organization":{"projectV2":{"id":"PVT_org1"}}}}`
			case strings.Contains(req.Query, "field(name:"):
				resp = `{"data":{"node":{"field":{"id":"PVTSSF_f1","options":[{"id":"OPT_inprog","name":"In Progress"},{"id":"OPT_done","name":"Done"},{"id":"OPT_blocked","name":"Blocked"}]}}}}`
			case strings.Contains(req.Query, "projectItems"):
				resp = `{"data":{"node":{"projectItems":{"nodes":[{"id":"PVTI_item1","project":{"id":"PVT_org1"}}]}}}}`
			case strings.Contains(req.Query, "updateProjectV2ItemFieldValue"):
				if optID, ok := req.Variables["optionID"].(string); ok {
					rec.add(optID)
				}
				resp = `{"data":{"updateProjectV2ItemFieldValue":{"projectV2Item":{"id":"PVTI_item1"}}}}`
			default:
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(resp))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
}

// TestPollerBoardSync_FailureMovesCardToBlocked verifies #17: when a dispatched
// issue fails, the card is moved to the configured blocked status instead of
// orphaning in In Progress.
func TestPollerBoardSync_FailureMovesCardToBlocked(t *testing.T) {
	issue := &Issue{
		Number: 1, Title: "feat: will fail", NodeID: "I_node1",
		Labels: []Label{{Name: "pilot"}},
	}
	rec := &statusMutationRecorder{}
	server := blockedBoardServer(t, []*Issue{issue}, issue, rec)
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	bs := newBoardSyncForTest(client)

	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithBoardSync(bs, "In Progress"),
		WithBoardBlockedStatus("Blocked"),
		WithOnIssueWithResult(func(ctx context.Context, i *Issue) (*IssueResult, error) {
			return nil, errors.New("execution failed")
		}),
	)

	poller.checkForNewIssues(context.Background())
	poller.WaitForActive()

	// First mutation moves to In Progress (OPT_inprog), then on failure to Blocked (OPT_blocked).
	ids := rec.snapshot()
	if len(ids) < 2 {
		t.Fatalf("expected at least 2 status mutations (in-progress + blocked), got %v", ids)
	}
	if ids[len(ids)-1] != "OPT_blocked" {
		t.Errorf("last status mutation = %q, want OPT_blocked (card should leave In Progress on failure)", ids[len(ids)-1])
	}
}

// TestPollerBoardSync_BlockedStatusUnsetIsNoOp verifies that without a
// configured blocked status, a failure performs no extra board mutation.
func TestPollerBoardSync_BlockedStatusUnsetIsNoOp(t *testing.T) {
	issue := &Issue{
		Number: 1, Title: "feat: will fail", NodeID: "I_node1",
		Labels: []Label{{Name: "pilot"}},
	}
	rec := &statusMutationRecorder{}
	server := blockedBoardServer(t, []*Issue{issue}, issue, rec)
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	bs := newBoardSyncForTest(client)

	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithBoardSync(bs, "In Progress"),
		// No WithBoardBlockedStatus.
		WithOnIssueWithResult(func(ctx context.Context, i *Issue) (*IssueResult, error) {
			return nil, errors.New("execution failed")
		}),
	)

	poller.checkForNewIssues(context.Background())
	poller.WaitForActive()

	for _, id := range rec.snapshot() {
		if id == "OPT_blocked" {
			t.Errorf("unexpected Blocked mutation when blockedStatus unset: %v", rec.snapshot())
		}
	}
}

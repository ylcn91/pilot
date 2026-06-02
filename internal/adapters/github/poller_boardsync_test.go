package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

// --- GH-3252: WithBoardSync write-back tests ---

// boardSyncTestServer creates an httptest.Server that handles the full set of calls
// made during a board-sync dispatch cycle:
//   - GET /repos/owner/repo/issues   → returns issues
//   - GET /repos/owner/repo/issues/N → returns freshIssue (used for label refresh and GetIssueNodeID fallback)
//   - POST /graphql                  → serves ProjectBoardSync GraphQL; counts mutations via mutationCount
func boardSyncTestServer(t *testing.T, issues []*Issue, freshIssue *Issue, mutationCount *atomic.Int32, capturedNodeID *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/issues":
			_ = json.NewEncoder(w).Encode(issues)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/issues/1":
			_ = json.NewEncoder(w).Encode(freshIssue)
		case r.Method == http.MethodPost && r.URL.Path == "/graphql":
			var req GraphQLRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode graphql request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			var resp string
			switch {
			case strings.Contains(req.Query, "organization"):
				resp = `{"data":{"organization":{"projectV2":{"id":"PVT_org1"}}}}`
			case strings.Contains(req.Query, "field(name:"):
				resp = `{"data":{"node":{"field":{"id":"PVTSSF_f1","options":[{"id":"OPT_inprog","name":"In Progress"},{"id":"OPT_done","name":"Done"}]}}}}`
			case strings.Contains(req.Query, "projectItems"):
				if capturedNodeID != nil {
					if id, ok := req.Variables["issueID"].(string); ok {
						*capturedNodeID = id
					}
				}
				resp = `{"data":{"node":{"projectItems":{"nodes":[{"id":"PVTI_item1","project":{"id":"PVT_org1"}}]}}}}`
			case strings.Contains(req.Query, "updateProjectV2ItemFieldValue"):
				mutationCount.Add(1)
				resp = `{"data":{"updateProjectV2ItemFieldValue":{"projectV2Item":{"id":"PVTI_item1"}}}}`
			default:
				t.Errorf("unexpected graphql query: %s", req.Query)
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

// newBoardSyncForTest creates a ProjectBoardSync backed by the given client.
func newBoardSyncForTest(client *Client) *ProjectBoardSync {
	return NewProjectBoardSync(client, &ProjectBoardConfig{
		Enabled:       true,
		ProjectNumber: 1,
		StatusField:   "Status",
	}, "owner")
}

func TestPollerBoardSync_PickupTriggersExactlyOneCall(t *testing.T) {
	issue := &Issue{
		Number: 1, Title: "feat: board test", NodeID: "I_node1",
		Labels: []Label{{Name: "pilot"}},
	}
	var mutCount atomic.Int32
	server := boardSyncTestServer(t, []*Issue{issue}, issue, &mutCount, nil)
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	bs := newBoardSyncForTest(client)

	var dispatched int32
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithBoardSync(bs, "In Progress"),
		WithOnIssueWithResult(func(ctx context.Context, i *Issue) (*IssueResult, error) {
			atomic.AddInt32(&dispatched, 1)
			return &IssueResult{Success: true, PRNumber: 1, PRURL: "https://github.com/owner/repo/pull/1"}, nil
		}),
	)

	poller.checkForNewIssues(context.Background())
	poller.WaitForActive()

	if got := atomic.LoadInt32(&dispatched); got != 1 {
		t.Fatalf("handler called %d times, want 1", got)
	}
	if got := mutCount.Load(); got != 1 {
		t.Errorf("UpdateProjectItemStatus called %d times, want exactly 1", got)
	}
}

func TestPollerBoardSync_NilBoardSync(t *testing.T) {
	issue := &Issue{
		Number: 1, Title: "no board", NodeID: "I_node1",
		Labels: []Label{{Name: "pilot"}},
	}
	var mutCount atomic.Int32
	server := boardSyncTestServer(t, []*Issue{issue}, issue, &mutCount, nil)
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

	// WithBoardSync not called — boardSync remains nil.
	var dispatched int32
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithOnIssueWithResult(func(ctx context.Context, i *Issue) (*IssueResult, error) {
			atomic.AddInt32(&dispatched, 1)
			return &IssueResult{Success: true, PRNumber: 1}, nil
		}),
	)

	poller.checkForNewIssues(context.Background())
	poller.WaitForActive()

	if got := atomic.LoadInt32(&dispatched); got != 1 {
		t.Fatalf("handler called %d times, want 1", got)
	}
	if got := mutCount.Load(); got != 0 {
		t.Errorf("mutation called %d times, want 0 (boardSync is nil)", got)
	}
}

func TestPollerBoardSync_EmptyInProgressStatus(t *testing.T) {
	issue := &Issue{
		Number: 1, Title: "no status", NodeID: "I_node1",
		Labels: []Label{{Name: "pilot"}},
	}
	var mutCount atomic.Int32
	server := boardSyncTestServer(t, []*Issue{issue}, issue, &mutCount, nil)
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	bs := newBoardSyncForTest(client)

	var dispatched int32
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithBoardSync(bs, ""), // empty status — must be no-op
		WithOnIssueWithResult(func(ctx context.Context, i *Issue) (*IssueResult, error) {
			atomic.AddInt32(&dispatched, 1)
			return &IssueResult{Success: true, PRNumber: 1}, nil
		}),
	)

	poller.checkForNewIssues(context.Background())
	poller.WaitForActive()

	if got := atomic.LoadInt32(&dispatched); got != 1 {
		t.Fatalf("handler called %d times, want 1", got)
	}
	if got := mutCount.Load(); got != 0 {
		t.Errorf("mutation called %d times, want 0 (inProgressStatus is empty)", got)
	}
}

func TestPollerBoardSync_NodeIDFromIssue(t *testing.T) {
	// Issue already has NodeID — GetIssueNodeID fallback must NOT be called.
	issue := &Issue{
		Number: 1, Title: "has node id", NodeID: "I_direct_node",
		Labels: []Label{{Name: "pilot"}},
	}
	var capturedNodeID string
	var mutCount atomic.Int32
	server := boardSyncTestServer(t, []*Issue{issue}, issue, &mutCount, &capturedNodeID)
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	bs := newBoardSyncForTest(client)

	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithBoardSync(bs, "In Progress"),
		WithOnIssueWithResult(func(ctx context.Context, i *Issue) (*IssueResult, error) {
			return &IssueResult{Success: true, PRNumber: 1}, nil
		}),
	)

	poller.checkForNewIssues(context.Background())
	poller.WaitForActive()

	if mutCount.Load() != 1 {
		t.Fatalf("mutation called %d times, want 1", mutCount.Load())
	}
	if capturedNodeID != "I_direct_node" {
		t.Errorf("GraphQL used nodeID %q, want %q (should use issue.NodeID directly)", capturedNodeID, "I_direct_node")
	}
}

func TestPollerBoardSync_NodeIDFallback(t *testing.T) {
	// Issue returned by ListIssues has empty NodeID — must fall back to GetIssueNodeID.
	issueNoNodeID := &Issue{
		Number: 1, Title: "no node id", NodeID: "",
		Labels: []Label{{Name: "pilot"}},
	}
	// GetIssueNodeID and fresh-label-check both use GET /repos/.../issues/1.
	// Return the same issue but WITH a node_id so GetIssueNodeID can resolve it.
	freshIssueWithNodeID := &Issue{
		Number: 1, Title: "no node id", NodeID: "I_fallback_node",
		Labels: []Label{{Name: "pilot"}},
	}
	var capturedNodeID string
	var mutCount atomic.Int32
	server := boardSyncTestServer(t, []*Issue{issueNoNodeID}, freshIssueWithNodeID, &mutCount, &capturedNodeID)
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	bs := newBoardSyncForTest(client)

	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithBoardSync(bs, "In Progress"),
		WithOnIssueWithResult(func(ctx context.Context, i *Issue) (*IssueResult, error) {
			return &IssueResult{Success: true, PRNumber: 1}, nil
		}),
	)

	poller.checkForNewIssues(context.Background())
	poller.WaitForActive()

	if mutCount.Load() != 1 {
		t.Fatalf("mutation called %d times, want 1", mutCount.Load())
	}
	if capturedNodeID != "I_fallback_node" {
		t.Errorf("GraphQL used nodeID %q, want %q (should use GetIssueNodeID fallback)", capturedNodeID, "I_fallback_node")
	}
}

// TestPoller_MergeDoneWindow_MarkProcessedAfterGrace verifies that calling
// MarkProcessed (as the onIssueDone callback does) re-enters the grace window
// and prevents phantom re-dispatch during the merge→pilot-done label lag.
// GH-3271 / TASK-321 PR-4.
func TestPoller_MergeDoneWindow_MarkProcessedAfterGrace(t *testing.T) {
	// Issue is open with pilot label, no pilot-done / pilot-in-progress.
	// This is the state during the merge→pilot-done window.
	issues := []*Issue{
		{Number: 77, Title: "Issue 77", Labels: []Label{{Name: "pilot"}}},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/repos/owner/repo/issues") && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(issues)
		case r.URL.Path == "/search/issues":
			// Search API: no merged PR found (simulates index lag)
			_, _ = w.Write([]byte(`{"total_count": 0}`))
		case r.URL.Path == "/repos/owner/repo/pulls":
			// Branch REST lookup: no merged PR (simulates the window before GitHub propagates)
			_, _ = w.Write([]byte(`[]`))
		default:
			_, _ = w.Write([]byte("{}"))
		}
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

	var callCount atomic.Int32
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithRetryGracePeriod(5*time.Millisecond),
		WithOnIssue(func(ctx context.Context, issue *Issue) error {
			callCount.Add(1)
			return nil
		}),
	)

	// Step 1: initial dispatch — markProcessed at T=0
	poller.markProcessed(77)

	// Step 2: grace period expires
	time.Sleep(15 * time.Millisecond)

	// Step 3: MarkProcessed called by onIssueDone (autopilot merge callback)
	// — refreshes the timestamp, re-entering the grace window
	poller.MarkProcessed(77)

	// Step 4: poll tick fires during merge→pilot-done window
	poller.checkForNewIssues(context.Background())
	poller.WaitForActive()

	if got := callCount.Load(); got != 0 {
		t.Errorf("dispatch callback called %d times, want 0 — MarkProcessed should prevent re-dispatch in merge→done window", got)
	}
}

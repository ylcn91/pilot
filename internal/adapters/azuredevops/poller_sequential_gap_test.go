package azuredevops

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

// sequentialServer routes the requests issued during a sequential poll cycle:
// the WIQL list (POST) + batch GET that back findOldestUnprocessedWorkItem, and
// the pull-request GET that backs the MergeWaiter. The first WIQL call returns
// the configured work items; subsequent calls return an empty list so the
// poller's loop quiesces into its interval wait (where the test cancels ctx).
type sequentialServer struct {
	mu        sync.Mutex
	workItems []*WorkItem
	prStatus  string // value returned for PullRequest.Status
	wiqlCalls int
	prCalls   int
}

func (s *sequentialServer) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.Contains(r.URL.Path, "/_apis/wit/wiql"):
			s.mu.Lock()
			s.wiqlCalls++
			first := s.wiqlCalls == 1
			s.mu.Unlock()
			if !first {
				_ = json.NewEncoder(w).Encode(WIQLQueryResult{WorkItems: []WIQLWorkItemRef{}})
				return
			}
			refs := make([]WIQLWorkItemRef, len(s.workItems))
			for i, wi := range s.workItems {
				refs[i] = WIQLWorkItemRef{ID: wi.ID}
			}
			_ = json.NewEncoder(w).Encode(WIQLQueryResult{WorkItems: refs})

		case strings.Contains(r.URL.Path, "/_apis/wit/workitems") && r.URL.Query().Get("ids") != "":
			_ = json.NewEncoder(w).Encode(struct {
				Count int         `json:"count"`
				Value []*WorkItem `json:"value"`
			}{Count: len(s.workItems), Value: s.workItems})

		case strings.Contains(r.URL.Path, "/pullrequests/"):
			s.mu.Lock()
			s.prCalls++
			s.mu.Unlock()
			_ = json.NewEncoder(w).Encode(PullRequest{
				PullRequestID: 42,
				Status:        s.prStatus,
			})

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	}
}

func newSequentialWorkItem(id int, created string) *WorkItem {
	return &WorkItem{
		ID: id,
		Fields: map[string]interface{}{
			"System.Title":       "seq item",
			"System.CreatedDate": created,
			"System.Tags":        "pilot",
		},
	}
}

// waitUntil polls cond up to timeout; deterministic (no fixed sleeps) and avoids
// hanging the test suite if the poller misbehaves.
func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return cond()
}

func TestStartSequential_MergedPR_MarksProcessed(t *testing.T) {
	srv := &sequentialServer{
		workItems: []*WorkItem{newSequentialWorkItem(7, "2024-01-01T10:00:00Z")},
		prStatus:  PRStateCompleted,
	}
	server := httptest.NewServer(srv.handler(t))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeAzureDevOpsPAT, "org", "project", server.URL)

	var (
		handlerMu    sync.Mutex
		handlerCalls int
	)
	var onPRCreatedCalls int32
	var onPRCreatedMu sync.Mutex

	poller := NewPoller(client, "pilot", 5*time.Millisecond,
		WithExecutionMode(ExecutionModeSequential),
		WithSequentialConfig(true, 5*time.Millisecond, 2*time.Second),
		WithOnWorkItemWithResult(func(ctx context.Context, wi *WorkItem) (*WorkItemResult, error) {
			handlerMu.Lock()
			handlerCalls++
			handlerMu.Unlock()
			return &WorkItemResult{
				Success:    true,
				PRNumber:   42,
				PRURL:      "https://dev.azure.com/org/project/_git/project/pullrequest/42",
				HeadSHA:    "abc123",
				BranchName: "pilot/GH-7",
			}, nil
		}),
		WithOnPRCreated(func(prID int, prURL string, workItemID int, headSHA, branchName string) {
			onPRCreatedMu.Lock()
			onPRCreatedCalls++
			onPRCreatedMu.Unlock()
		}),
	)

	if poller.mergeWaiter == nil {
		t.Fatal("expected mergeWaiter to be created for sequential + waitForMerge")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		poller.startSequential(ctx)
		close(done)
	}()

	if !waitUntil(t, 2*time.Second, func() bool { return poller.IsProcessed(7) }) {
		t.Fatal("expected work item 7 to be marked processed after merged PR")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("startSequential did not return after ctx cancel")
	}

	handlerMu.Lock()
	if handlerCalls < 1 {
		t.Errorf("expected handler to be invoked at least once, got %d", handlerCalls)
	}
	handlerMu.Unlock()

	onPRCreatedMu.Lock()
	if onPRCreatedCalls < 1 {
		t.Errorf("expected OnPRCreated callback to fire, got %d", onPRCreatedCalls)
	}
	onPRCreatedMu.Unlock()
}

func TestStartSequential_DirectCommit_MarksProcessed(t *testing.T) {
	srv := &sequentialServer{
		workItems: []*WorkItem{newSequentialWorkItem(8, "2024-01-01T10:00:00Z")},
		// no PR status needed: direct commit means PRNumber==0, MergeWaiter is never hit
	}
	server := httptest.NewServer(srv.handler(t))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeAzureDevOpsPAT, "org", "project", server.URL)

	poller := NewPoller(client, "pilot", 5*time.Millisecond,
		WithExecutionMode(ExecutionModeSequential),
		WithSequentialConfig(true, 5*time.Millisecond, 2*time.Second),
		WithOnWorkItemWithResult(func(ctx context.Context, wi *WorkItem) (*WorkItemResult, error) {
			return &WorkItemResult{Success: true, PRNumber: 0, HeadSHA: "deadbeef"}, nil
		}),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		poller.startSequential(ctx)
		close(done)
	}()

	if !waitUntil(t, 2*time.Second, func() bool { return poller.IsProcessed(8) }) {
		t.Fatal("expected work item 8 to be marked processed after direct commit")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("startSequential did not return after ctx cancel")
	}

	srv.mu.Lock()
	prCalls := srv.prCalls
	srv.mu.Unlock()
	if prCalls != 0 {
		t.Errorf("expected no PR status polls for direct commit, got %d", prCalls)
	}
}

func TestStartSequential_HandlerError_MarksProcessedToAvoidRetryLoop(t *testing.T) {
	srv := &sequentialServer{
		workItems: []*WorkItem{newSequentialWorkItem(9, "2024-01-01T10:00:00Z")},
	}
	server := httptest.NewServer(srv.handler(t))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeAzureDevOpsPAT, "org", "project", server.URL)

	poller := NewPoller(client, "pilot", 5*time.Millisecond,
		WithExecutionMode(ExecutionModeSequential),
		WithSequentialConfig(true, 5*time.Millisecond, 2*time.Second),
		WithOnWorkItemWithResult(func(ctx context.Context, wi *WorkItem) (*WorkItemResult, error) {
			return nil, errors.New("execution failed")
		}),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		poller.startSequential(ctx)
		close(done)
	}()

	if !waitUntil(t, 2*time.Second, func() bool { return poller.IsProcessed(9) }) {
		t.Fatal("expected work item 9 to be marked processed after handler error (avoid retry loop)")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("startSequential did not return after ctx cancel")
	}
}

func TestStartSequential_ContextCancelBeforeWorkExits(t *testing.T) {
	// No work items: loop should enter the interval wait and exit promptly on cancel.
	srv := &sequentialServer{workItems: nil}
	server := httptest.NewServer(srv.handler(t))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeAzureDevOpsPAT, "org", "project", server.URL)

	poller := NewPoller(client, "pilot", 50*time.Millisecond,
		WithExecutionMode(ExecutionModeSequential),
		WithSequentialConfig(false, 5*time.Millisecond, 2*time.Second),
		WithOnWorkItemWithResult(func(ctx context.Context, wi *WorkItem) (*WorkItemResult, error) {
			return &WorkItemResult{Success: true}, nil
		}),
	)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		poller.startSequential(ctx)
		close(done)
	}()

	// Let at least one poll cycle observe "no work" before cancelling.
	if !waitUntil(t, 2*time.Second, func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return srv.wiqlCalls >= 1
	}) {
		t.Fatal("expected at least one WIQL poll before cancel")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("startSequential did not return after ctx cancel")
	}
}

// findOldestUnprocessedWorkItem already has happy-path + empty coverage in
// poller_workitem_test.go; this adds the locally-processed filter branch, which
// is otherwise untested.
func TestFindOldestUnprocessedWorkItem_SkipsLocallyProcessed(t *testing.T) {
	srv := &sequentialServer{
		workItems: []*WorkItem{
			newSequentialWorkItem(1, "2024-01-01T10:00:00Z"),
			newSequentialWorkItem(2, "2024-01-02T10:00:00Z"),
		},
	}
	server := httptest.NewServer(srv.handler(t))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeAzureDevOpsPAT, "org", "project", server.URL)
	poller := NewPoller(client, "pilot", 30*time.Second)

	// Oldest (id 1) is already processed locally; expect id 2 returned.
	poller.markProcessed(1)

	wi, err := poller.findOldestUnprocessedWorkItem(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wi == nil {
		t.Fatal("expected a work item, got nil")
	}
	if wi.ID != 2 {
		t.Errorf("expected oldest *unprocessed* work item id 2, got %d", wi.ID)
	}
}

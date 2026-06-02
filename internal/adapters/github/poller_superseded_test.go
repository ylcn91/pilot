package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

// GH-2341: Fresh label fetch before dispatch blocks re-dispatch when the
// ListIssues snapshot is stale and pilot-done was added in between.
func TestPoller_CheckForNewIssues_StaleSnapshot_RefreshesLabels(t *testing.T) {
	// Snapshot returned by ListIssues has NO pilot-done label.
	staleIssues := []*Issue{
		{Number: 42, Title: "GH-42 feature", State: "open", Labels: []Label{{Name: "pilot"}}},
	}
	// Fresh GetIssue response includes pilot-done.
	freshIssue := &Issue{
		Number: 42, Title: "GH-42 feature", State: "open",
		Labels: []Label{{Name: "pilot"}, {Name: LabelDone}},
	}

	var dispatches int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusOK)
			return
		}
		switch r.URL.Path {
		case "/repos/owner/repo/issues":
			_ = json.NewEncoder(w).Encode(staleIssues)
		case "/repos/owner/repo/issues/42":
			_ = json.NewEncoder(w).Encode(freshIssue)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithOnIssue(func(ctx context.Context, issue *Issue) error {
			atomic.AddInt32(&dispatches, 1)
			return nil
		}),
	)

	poller.checkForNewIssues(context.Background())
	poller.WaitForActive()

	if got := atomic.LoadInt32(&dispatches); got != 0 {
		t.Errorf("dispatched %d times, want 0 (fresh GetIssue shows pilot-done)", got)
	}
}

// GH-2402: Sub-issues whose parent epic already shipped must be auto-closed
// with pilot-superseded instead of dispatched.
func TestPoller_CheckForNewIssues_SkipsSupersededByParent(t *testing.T) {
	subIssue := &Issue{
		Number: 200,
		Title:  "Sub-issue under shipped epic",
		Body:   "Parent: GH-100\n\nSome work.",
		State:  "open",
		Labels: []Label{{Name: "pilot"}},
	}
	parent := &Issue{
		Number: 100,
		Title:  "Epic",
		State:  "closed",
		Labels: []Label{{Name: "pilot"}, {Name: LabelDone}},
	}

	var (
		mu               sync.Mutex
		gotComment       string
		gotLabels        []string
		closedIssueState string
		dispatches       int32
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/issues":
			_ = json.NewEncoder(w).Encode([]*Issue{subIssue})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/issues/100":
			_ = json.NewEncoder(w).Encode(parent)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/issues/200":
			_ = json.NewEncoder(w).Encode(subIssue)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/issues/200/comments":
			var payload struct {
				Body string `json:"body"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			mu.Lock()
			gotComment = payload.Body
			mu.Unlock()
			_, _ = w.Write([]byte(`{"id":1}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/issues/200/labels":
			var payload struct {
				Labels []string `json:"labels"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			mu.Lock()
			gotLabels = append(gotLabels, payload.Labels...)
			mu.Unlock()
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/owner/repo/issues/200":
			var payload struct {
				State string `json:"state"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			mu.Lock()
			closedIssueState = payload.State
			mu.Unlock()
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithOnIssue(func(ctx context.Context, issue *Issue) error {
			atomic.AddInt32(&dispatches, 1)
			return nil
		}),
	)
	poller.checkForNewIssues(context.Background())
	poller.WaitForActive()

	if got := atomic.LoadInt32(&dispatches); got != 0 {
		t.Errorf("dispatched %d times, want 0 (sub-issue should be auto-closed)", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(gotComment, "parent epic #100") {
		t.Errorf("comment missing parent reference: %q", gotComment)
	}
	hasSuperseded := false
	for _, l := range gotLabels {
		if l == LabelSuperseded {
			hasSuperseded = true
			break
		}
	}
	if !hasSuperseded {
		t.Errorf("expected pilot-superseded label, got %v", gotLabels)
	}
	if closedIssueState != "closed" {
		t.Errorf("expected sub-issue closed, got state=%q", closedIssueState)
	}
}

// GH-2402: Sub-issues whose parent is closed but NOT marked done should NOT
// be auto-closed (e.g. user manually closed the epic before Pilot finished).
func TestPoller_CheckForNewIssues_DoesNotSupersedeWhenParentNotDone(t *testing.T) {
	subIssue := &Issue{
		Number: 201,
		Title:  "Sub-issue",
		Body:   "Parent: GH-101\n",
		State:  "open",
		Labels: []Label{{Name: "pilot"}},
	}
	parent := &Issue{
		Number: 101,
		Title:  "Epic",
		State:  "closed",
		Labels: []Label{{Name: "pilot"}}, // No LabelDone
	}

	var dispatches int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/issues":
			_ = json.NewEncoder(w).Encode([]*Issue{subIssue})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/issues/101":
			_ = json.NewEncoder(w).Encode(parent)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/issues/201":
			_ = json.NewEncoder(w).Encode(subIssue)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithOnIssue(func(ctx context.Context, issue *Issue) error {
			atomic.AddInt32(&dispatches, 1)
			return nil
		}),
	)
	poller.checkForNewIssues(context.Background())
	poller.WaitForActive()

	if got := atomic.LoadInt32(&dispatches); got != 1 {
		t.Errorf("dispatched %d times, want 1 (parent closed but not done — should dispatch)", got)
	}
}

// GH-2402: Issues with pilot-blocked must be skipped — no dispatch and no retry
// until the user removes the label.
func TestPoller_CheckForNewIssues_SkipsBlockedIssues(t *testing.T) {
	blocked := &Issue{
		Number: 300,
		Title:  "deterministic failure",
		State:  "open",
		Labels: []Label{{Name: "pilot"}, {Name: LabelBlocked}},
	}

	var dispatches int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]*Issue{blocked})
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithOnIssue(func(ctx context.Context, issue *Issue) error {
			atomic.AddInt32(&dispatches, 1)
			return nil
		}),
	)
	poller.checkForNewIssues(context.Background())
	poller.WaitForActive()

	if got := atomic.LoadInt32(&dispatches); got != 0 {
		t.Errorf("dispatched %d times, want 0 (pilot-blocked must skip)", got)
	}
}

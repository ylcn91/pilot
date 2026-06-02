package autopilot

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

// TestController_OnPRCreated_BoardSyncReview verifies that OnPRCreated triggers a
// board sync to reviewStatus when a non-empty IssueNodeID is present.
func TestController_OnPRCreated_BoardSyncReview(t *testing.T) {
	tests := []struct {
		name         string
		issueNodeID  string
		reviewStatus string
		wantCalls    int
		wantStatus   string
	}{
		{
			name:         "syncs to reviewStatus when nodeID and status set",
			issueNodeID:  "IssueNodeID_abc",
			reviewStatus: "In Review",
			wantCalls:    1,
			wantStatus:   "In Review",
		},
		{
			name:         "no sync when issueNodeID is empty",
			issueNodeID:  "",
			reviewStatus: "In Review",
			wantCalls:    0,
		},
		{
			name:         "no sync when reviewStatus is empty",
			issueNodeID:  "IssueNodeID_abc",
			reviewStatus: "",
			wantCalls:    0,
		},
		{
			name:         "no sync when both empty",
			issueNodeID:  "",
			reviewStatus: "",
			wantCalls:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBoardSyncer{}
			opt := withBoardSyncerForTest(mock, "Done", "Failed", tt.reviewStatus, "")
			c := NewController(DefaultConfig(), github.NewClient(testutil.FakeGitHubToken), nil, "owner", "repo", opt)

			c.OnPRCreated(42, "https://github.com/owner/repo/pull/42", 10, "abc123", "pilot/GH-10", tt.issueNodeID)

			if len(mock.calls) != tt.wantCalls {
				t.Fatalf("board sync calls = %d, want %d", len(mock.calls), tt.wantCalls)
			}
			if tt.wantCalls > 0 {
				got := mock.calls[0]
				if got.issueNodeID != tt.issueNodeID {
					t.Errorf("issueNodeID = %q, want %q", got.issueNodeID, tt.issueNodeID)
				}
				if got.statusName != tt.wantStatus {
					t.Errorf("statusName = %q, want %q", got.statusName, tt.wantStatus)
				}
			}
		})
	}
}

// TestController_OnPRCreated_BoardSync_NoBoardSync is a regression guard that verifies
// OnPRCreated does not panic or error when no board sync is configured.
func TestController_OnPRCreated_BoardSync_NoBoardSync(t *testing.T) {
	c := NewController(DefaultConfig(), github.NewClient(testutil.FakeGitHubToken), nil, "owner", "repo")
	// Should not panic.
	c.OnPRCreated(42, "https://github.com/owner/repo/pull/42", 10, "abc123", "pilot/GH-10", "IssueNodeID_abc")
	if _, ok := c.GetPRState(42); !ok {
		t.Fatal("PR should be registered even without board sync")
	}
}

// TestController_handleCIFailed_BoardSync_IterationLimit verifies that the
// iteration-limit execution-failure path syncs the board to failStatus.
func TestController_handleCIFailed_BoardSync_IterationLimit(t *testing.T) {
	const issueNodeID = "IssueNodeID_iter"

	tests := []struct {
		name       string
		failStatus string
		wantCalls  int
	}{
		{
			name:       "syncs to failStatus at iteration limit",
			failStatus: "Blocked",
			wantCalls:  1,
		},
		{
			name:       "no sync when failStatus is empty",
			failStatus: "",
			wantCalls:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBoardSyncer{}

			// Build a test HTTP server that serves the GitHub API calls made by handleCIFailed.
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/issues/"):
					// Return an issue body with iteration counter at the limit.
					_, _ = fmt.Fprintf(w, `{"number":10,"body":"<!-- autopilot-meta iteration:%d -->"}`, 3)
				case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/pulls/"):
					w.WriteHeader(http.StatusNoContent)
				default:
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("{}"))
				}
			}))
			defer server.Close()

			ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			cfg := DefaultConfig()
			cfg.MaxCIFixIterations = 3 // iteration = 3 >= MaxCIFixIterations = 3 → limit hit

			opt := withBoardSyncerForTest(mock, "Done", tt.failStatus, "In Review", "")
			c := NewController(cfg, ghClient, nil, "owner", "repo", opt)

			prState := &PRState{
				PRNumber:    42,
				IssueNumber: 10,
				IssueNodeID: issueNodeID,
				HeadSHA:     "abc123",
				BranchName:  "pilot/GH-10",
			}

			err := c.handleCIFailed(context.Background(), prState)
			if err != nil {
				t.Fatalf("handleCIFailed returned unexpected error: %v", err)
			}
			if prState.Stage != StageFailed {
				t.Errorf("Stage = %s, want %s", prState.Stage, StageFailed)
			}

			if len(mock.calls) != tt.wantCalls {
				t.Fatalf("board sync calls = %d, want %d (calls: %+v)", len(mock.calls), tt.wantCalls, mock.calls)
			}
			if tt.wantCalls > 0 {
				got := mock.calls[0]
				if got.issueNodeID != issueNodeID {
					t.Errorf("issueNodeID = %q, want %q", got.issueNodeID, issueNodeID)
				}
				if got.statusName != tt.failStatus {
					t.Errorf("statusName = %q, want %q", got.statusName, tt.failStatus)
				}
			}
		})
	}
}

// TestController_handleCIFailed_BoardSync_Regression_NormalPath is a regression guard
// that verifies the existing normal CI failure board sync (non-iteration-limit) still fires.
func TestController_handleCIFailed_BoardSync_Regression_NormalPath(t *testing.T) {
	const issueNodeID = "IssueNodeID_normal"
	mock := &mockBoardSyncer{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/issues/") && !strings.Contains(r.URL.Path, "/comments"):
			// Iteration = 0 → below any limit
			_, _ = fmt.Fprintf(w, `{"number":10,"body":"no-meta"}`)
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/issues"):
			// CreateFailureIssue creates a new issue
			_, _ = fmt.Fprintf(w, `{"number":99}`)
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/check-runs"):
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/check-runs"):
			_, _ = fmt.Fprintf(w, `{"check_runs":[]}`)
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/pulls/"):
			_, _ = fmt.Fprintf(w, `{"number":42,"state":"open"}`)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{}"))
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.MaxCIFixIterations = 5 // below limit

	opt := withBoardSyncerForTest(mock, "Done", "Blocked", "In Review", "")
	c := NewController(cfg, ghClient, nil, "owner", "repo", opt)

	prState := &PRState{
		PRNumber:    42,
		IssueNumber: 10,
		IssueNodeID: issueNodeID,
		HeadSHA:     "abc123",
		BranchName:  "pilot/GH-10",
	}

	// handleCIFailed should reach the normal path and fire board sync with failStatus.
	_ = c.handleCIFailed(context.Background(), prState)

	// The normal path fires one board sync call with failStatus.
	found := false
	for _, call := range mock.calls {
		if call.issueNodeID == issueNodeID && call.statusName == "Blocked" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected board sync call with issueNodeID=%q statusName=%q, got calls: %+v",
			issueNodeID, "Blocked", mock.calls)
	}
}

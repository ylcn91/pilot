package autopilot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

// TestHandleCIPassed_SizeFloorEscalation verifies that a PR exceeding the size
// floor (300 net additions > 200 threshold) is routed to StageAwaitApproval even
// when RequireApproval is false.
func TestHandleCIPassed_SizeFloorEscalation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/owner/repo/pulls/77/files" && r.Method == http.MethodGet {
			files := []*github.PRFile{
				{Filename: "a.go", Status: "modified", Additions: 300},
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mustJSON(t, files))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Environment = EnvDev // RequireApproval = false

	c := NewController(cfg, ghClient, nil, "owner", "repo")
	prState := &PRState{
		PRNumber: 77,
		PRTitle:  "fix(auth): fix bug",
		Stage:    StageCIPassed,
	}

	if err := c.handleCIPassed(context.Background(), prState); err != nil {
		t.Fatalf("handleCIPassed returned unexpected error: %v", err)
	}
	if prState.Stage != StageAwaitApproval {
		t.Errorf("expected StageAwaitApproval for oversized PR, got %v", prState.Stage)
	}
}

// TestHandleCIPassed_ScopeDriftEscalation verifies that a PR whose conventional-
// commit type diverges from the linked issue's title is routed to StageAwaitApproval
// even when RequireApproval is false.
func TestHandleCIPassed_ScopeDriftEscalation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/pulls/78/files" && r.Method == http.MethodGet:
			// Small PR — size floor should not fire.
			files := []*github.PRFile{
				{Filename: "a.go", Status: "modified", Additions: 10},
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mustJSON(t, files))
		case r.URL.Path == "/repos/owner/repo/issues/50" && r.Method == http.MethodGet:
			resp := github.Issue{Number: 50, Title: "fix(auth): fix login bug"}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mustJSON(t, resp))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{}"))
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Environment = EnvDev // RequireApproval = false

	c := NewController(cfg, ghClient, nil, "owner", "repo")
	prState := &PRState{
		PRNumber:    78,
		IssueNumber: 50,
		// feat type diverges from the fix issue title — scope drift gate fires.
		PRTitle: "feat(auth): add OAuth",
		Stage:   StageCIPassed,
	}

	if err := c.handleCIPassed(context.Background(), prState); err != nil {
		t.Fatalf("handleCIPassed returned unexpected error: %v", err)
	}
	if prState.Stage != StageAwaitApproval {
		t.Errorf("expected StageAwaitApproval for scope-drifting PR, got %v", prState.Stage)
	}
}

// TestHandleCIPassed_SmallInScopePRMerges verifies that a small, in-scope PR with
// RequireApproval=false proceeds to StageMerging (no false positives from the gates).
func TestHandleCIPassed_SmallInScopePRMerges(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/pulls/79/files" && r.Method == http.MethodGet:
			files := []*github.PRFile{
				{Filename: "a.go", Status: "modified", Additions: 50},
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mustJSON(t, files))
		case r.URL.Path == "/repos/owner/repo/issues/51" && r.Method == http.MethodGet:
			resp := github.Issue{Number: 51, Title: "fix(auth): fix login bug"}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mustJSON(t, resp))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{}"))
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Environment = EnvDev // RequireApproval = false

	c := NewController(cfg, ghClient, nil, "owner", "repo")
	prState := &PRState{
		PRNumber:    79,
		IssueNumber: 51,
		// Same type+scope as the issue — no drift.
		PRTitle: "fix(auth): fix login bug",
		Stage:   StageCIPassed,
	}

	if err := c.handleCIPassed(context.Background(), prState); err != nil {
		t.Fatalf("handleCIPassed returned unexpected error: %v", err)
	}
	if prState.Stage != StageMerging {
		t.Errorf("expected StageMerging for small in-scope PR, got %v", prState.Stage)
	}
}

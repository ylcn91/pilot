package autopilot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

// --- GH-2588: CI fix size guard tests ---

// TestCIFixSizeGuard_OversizedPR_BlocksFixIssue is a cascade-2 reproduction:
// a failing PR with 512 additions must NOT spawn a fix(ci) issue and must be closed.
func TestCIFixSizeGuard_OversizedPR_BlocksFixIssue(t *testing.T) {
	issueCreated := false
	prClosed := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/commits/abc1234/check-runs":
			resp := github.CheckRunsResponse{
				TotalCount: 1,
				CheckRuns: []github.CheckRun{
					{Name: "build", Status: "completed", Conclusion: "failure"},
				},
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mustJSON(t, resp))
		case r.URL.Path == "/repos/owner/repo/issues/10" && r.Method == http.MethodGet:
			resp := github.Issue{Number: 10, Body: "<!-- autopilot-meta branch:pilot/GH-5 pr:99 iteration:1 -->"}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mustJSON(t, resp))
		case r.URL.Path == "/repos/owner/repo/pulls/42/files" && r.Method == http.MethodGet:
			// Simulate cascade-2 PR: 512 net additions
			files := []*github.PRFile{
				{Filename: "internal/gateway/oauth.go", Status: "added", Additions: 244},
				{Filename: "internal/gateway/oauth_test.go", Status: "added", Additions: 240},
				{Filename: "internal/gateway/server.go", Status: "modified", Additions: 28},
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mustJSON(t, files))
		case r.URL.Path == "/repos/owner/repo/issues" && r.Method == http.MethodPost:
			issueCreated = true
			resp := github.Issue{Number: 200}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write(mustJSON(t, resp))
		case r.URL.Path == "/repos/owner/repo/pulls/42" && r.Method == http.MethodPatch:
			prClosed = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{}"))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{}"))
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Environment = EnvStage
	cfg.MaxCIFixPRSize = 200
	cfg.AutoCreateIssues = true

	c := NewController(cfg, ghClient, nil, "owner", "repo")

	prState := &PRState{
		PRNumber:    42,
		IssueNumber: 10,
		HeadSHA:     "abc1234",
		Stage:       StageCIFailed,
	}

	err := c.handleCIFailed(context.Background(), prState)
	if err != nil {
		t.Fatalf("handleCIFailed returned unexpected error: %v", err)
	}

	if issueCreated {
		t.Error("fix issue must NOT be created when failing PR exceeds size floor")
	}
	if !prClosed {
		t.Error("oversized failing PR must be closed by the size guard")
	}
	if prState.Stage != StageFailed {
		t.Errorf("Stage = %s, want %s", prState.Stage, StageFailed)
	}
	if !strings.Contains(prState.Error, "CI fix size guard") {
		t.Errorf("error should mention size guard, got: %s", prState.Error)
	}
}

// TestCIFixSizeGuard_SmallPR_AllowsFixIssue verifies that a small failing PR (50 additions)
// still gets a fix(ci) issue created — existing behavior must be unchanged.
func TestCIFixSizeGuard_SmallPR_AllowsFixIssue(t *testing.T) {
	issueCreated := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/commits/abc5678/check-runs":
			resp := github.CheckRunsResponse{
				TotalCount: 1,
				CheckRuns: []github.CheckRun{
					{Name: "build", Status: "completed", Conclusion: "failure"},
				},
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mustJSON(t, resp))
		case r.URL.Path == "/repos/owner/repo/issues/20" && r.Method == http.MethodGet:
			resp := github.Issue{Number: 20, Body: "<!-- autopilot-meta branch:pilot/GH-20 pr:55 iteration:1 -->"}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mustJSON(t, resp))
		case r.URL.Path == "/repos/owner/repo/pulls/55/files" && r.Method == http.MethodGet:
			files := []*github.PRFile{
				{Filename: "internal/foo.go", Status: "modified", Additions: 50},
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mustJSON(t, files))
		case r.URL.Path == "/repos/owner/repo/issues" && r.Method == http.MethodPost:
			issueCreated = true
			resp := github.Issue{Number: 300}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write(mustJSON(t, resp))
		case r.URL.Path == "/repos/owner/repo/pulls/55" && r.Method == http.MethodPatch:
			// PR closed after fix issue created (normal flow)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{}"))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{}"))
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Environment = EnvStage
	cfg.MaxCIFixPRSize = 200
	cfg.AutoCreateIssues = true

	c := NewController(cfg, ghClient, nil, "owner", "repo")

	prState := &PRState{
		PRNumber:    55,
		IssueNumber: 20,
		HeadSHA:     "abc5678",
		Stage:       StageCIFailed,
	}

	err := c.handleCIFailed(context.Background(), prState)
	if err != nil {
		t.Fatalf("handleCIFailed returned unexpected error: %v", err)
	}

	if !issueCreated {
		t.Error("fix issue MUST be created for a small failing PR")
	}
}

// TestCIFixSizeGuard_APIError_FailOpen verifies that a ListPullRequestFiles API error
// does not block fix issue creation — the guard must fail open.
func TestCIFixSizeGuard_APIError_FailOpen(t *testing.T) {
	issueCreated := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/commits/abc9999/check-runs":
			resp := github.CheckRunsResponse{
				TotalCount: 1,
				CheckRuns: []github.CheckRun{
					{Name: "build", Status: "completed", Conclusion: "failure"},
				},
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mustJSON(t, resp))
		case r.URL.Path == "/repos/owner/repo/issues/30" && r.Method == http.MethodGet:
			resp := github.Issue{Number: 30, Body: "<!-- autopilot-meta branch:pilot/GH-30 pr:66 iteration:1 -->"}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(mustJSON(t, resp))
		case r.URL.Path == "/repos/owner/repo/pulls/66/files" && r.Method == http.MethodGet:
			// Simulate transient API error
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"server error"}`))
		case r.URL.Path == "/repos/owner/repo/issues" && r.Method == http.MethodPost:
			issueCreated = true
			resp := github.Issue{Number: 400}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write(mustJSON(t, resp))
		case r.URL.Path == "/repos/owner/repo/pulls/66" && r.Method == http.MethodPatch:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{}"))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{}"))
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Environment = EnvStage
	cfg.MaxCIFixPRSize = 200
	cfg.AutoCreateIssues = true

	c := NewController(cfg, ghClient, nil, "owner", "repo")

	prState := &PRState{
		PRNumber:    66,
		IssueNumber: 30,
		HeadSHA:     "abc9999",
		Stage:       StageCIFailed,
	}

	err := c.handleCIFailed(context.Background(), prState)
	if err != nil {
		t.Fatalf("handleCIFailed returned unexpected error: %v", err)
	}

	if !issueCreated {
		t.Error("fix issue MUST be created when ListPullRequestFiles fails (fail-open)")
	}
}

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

func TestFeedbackLoop_CreateFailureIssue_Deployment(t *testing.T) {
	capturedTitle := ""
	capturedBody := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/owner/repo/issues" && r.Method == "POST" {
			var input github.IssueInput
			_ = json.NewDecoder(r.Body).Decode(&input)
			capturedTitle = input.Title
			capturedBody = input.Body

			resp := github.Issue{Number: 103}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.AutoCreateIssues = true

	fl := NewFeedbackLoop(ghClient, "owner", "repo", cfg)

	prState := &PRState{
		PRNumber: 42,
		HeadSHA:  "abc1234",
	}

	issueNum, err := fl.CreateFailureIssue(
		context.Background(),
		prState,
		FailureDeployment,
		nil,
		"Deployment failed: container health check failed",
		0,
	)

	if err != nil {
		t.Fatalf("CreateFailureIssue() error = %v", err)
	}
	if issueNum != 103 {
		t.Errorf("CreateFailureIssue() = %d, want 103", issueNum)
	}

	expectedTitle := "fix(deploy): resolve deployment failure from PR #42"
	if capturedTitle != expectedTitle {
		t.Errorf("title = %q, want %q", capturedTitle, expectedTitle)
	}

	if !strings.Contains(capturedBody, "deployment failed") {
		t.Error("body should contain deployment instructions")
	}
}

func TestFeedbackLoop_CreateFailureIssue_UnknownType(t *testing.T) {
	capturedTitle := ""
	capturedBody := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/owner/repo/issues" && r.Method == "POST" {
			var input github.IssueInput
			_ = json.NewDecoder(r.Body).Decode(&input)
			capturedTitle = input.Title
			capturedBody = input.Body

			resp := github.Issue{Number: 104}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.AutoCreateIssues = true

	fl := NewFeedbackLoop(ghClient, "owner", "repo", cfg)

	prState := &PRState{
		PRNumber: 42,
		HeadSHA:  "abc1234",
	}

	issueNum, err := fl.CreateFailureIssue(
		context.Background(),
		prState,
		FailureType("unknown"),
		nil,
		"",
		0,
	)

	if err != nil {
		t.Fatalf("CreateFailureIssue() error = %v", err)
	}
	if issueNum != 104 {
		t.Errorf("CreateFailureIssue() = %d, want 104", issueNum)
	}

	expectedTitle := "fix(autopilot): resolve issue from PR #42"
	if capturedTitle != expectedTitle {
		t.Errorf("title = %q, want %q", capturedTitle, expectedTitle)
	}

	if !strings.Contains(capturedBody, "Investigate and fix") {
		t.Error("body should contain generic instructions")
	}
}

func TestFeedbackLoop_CreateFailureIssue_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message": "Internal Server Error"}`))
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.AutoCreateIssues = true

	fl := NewFeedbackLoop(ghClient, "owner", "repo", cfg)

	prState := &PRState{
		PRNumber: 42,
		HeadSHA:  "abc1234",
	}

	_, err := fl.CreateFailureIssue(
		context.Background(),
		prState,
		FailureCIPreMerge,
		[]string{"build"},
		"",
		0,
	)

	if err == nil {
		t.Error("CreateFailureIssue() should return error on API failure")
	}
}

func TestFeedbackLoop_CreateFailureIssue_WithKnownPatterns(t *testing.T) {
	capturedBody := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/owner/repo/issues" && r.Method == "POST" {
			var input github.IssueInput
			_ = json.NewDecoder(r.Body).Decode(&input)
			capturedBody = input.Body

			resp := github.Issue{Number: 120}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.AutoCreateIssues = true
	cfg.IssueLabels = []string{"pilot"}

	fl := NewFeedbackLoop(ghClient, "owner", "repo", cfg)

	// No learning loop set — body should NOT contain "Known Patterns"
	prState := &PRState{
		PRNumber: 42,
		HeadSHA:  "abc1234567890",
	}

	_, err := fl.CreateFailureIssue(
		context.Background(),
		prState,
		FailureCIPreMerge,
		[]string{"build"},
		"Error: build failed",
		0,
	)
	if err != nil {
		t.Fatalf("CreateFailureIssue() error = %v", err)
	}

	if strings.Contains(capturedBody, "Known Patterns") {
		t.Error("body should NOT contain Known Patterns section when no learning loop is set")
	}
}

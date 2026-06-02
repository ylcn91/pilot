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

func TestFeedbackLoop_CreateFailureIssue_CIFailed(t *testing.T) {
	capturedTitle := ""
	capturedBody := ""
	capturedLabels := []string{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/owner/repo/issues" && r.Method == "POST" {
			var input github.IssueInput
			_ = json.NewDecoder(r.Body).Decode(&input)
			capturedTitle = input.Title
			capturedBody = input.Body
			capturedLabels = input.Labels

			resp := github.Issue{Number: 100}
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
	cfg.IssueLabels = []string{"pilot", "autopilot-fix"}

	fl := NewFeedbackLoop(ghClient, "owner", "repo", cfg)

	prState := &PRState{
		PRNumber:    42,
		PRURL:       "https://github.com/owner/repo/pull/42",
		IssueNumber: 10,
		HeadSHA:     "abc1234567890",
	}

	issueNum, err := fl.CreateFailureIssue(
		context.Background(),
		prState,
		FailureCIPreMerge,
		[]string{"build", "test"},
		"Error: build failed\nNPM ERR! code 1",
		0,
	)

	if err != nil {
		t.Fatalf("CreateFailureIssue() error = %v", err)
	}
	if issueNum != 100 {
		t.Errorf("CreateFailureIssue() = %d, want 100", issueNum)
	}

	// Verify title
	expectedTitle := "fix(ci): resolve CI failure from PR #42"
	if capturedTitle != expectedTitle {
		t.Errorf("title = %q, want %q", capturedTitle, expectedTitle)
	}

	// Verify body contains expected sections
	if !strings.Contains(capturedBody, "Autopilot: Auto-Generated Fix Request") {
		t.Error("body should contain header")
	}
	if !strings.Contains(capturedBody, "Original PR**: #42") {
		t.Error("body should contain PR reference")
	}
	if !strings.Contains(capturedBody, "Original Issue**: #10") {
		t.Error("body should contain issue reference")
	}
	if !strings.Contains(capturedBody, "abc1234") {
		t.Error("body should contain SHA (truncated)")
	}
	if !strings.Contains(capturedBody, "- [ ] build") {
		t.Error("body should contain failed checks")
	}
	if !strings.Contains(capturedBody, "- [ ] test") {
		t.Error("body should contain failed checks")
	}
	if !strings.Contains(capturedBody, "Error: build failed") {
		t.Error("body should contain error logs")
	}
	// GH-1567: Logs should be in collapsible details block
	if !strings.Contains(capturedBody, "<details><summary>CI Error Logs</summary>") {
		t.Error("body should wrap logs in collapsible <details> block")
	}
	if !strings.Contains(capturedBody, "</details>") {
		t.Error("body should close </details> tag")
	}
	if !strings.Contains(capturedBody, "Fix the CI failures") {
		t.Error("body should contain task instructions")
	}
	// GH-1798: Verify dependency annotation for parent issue
	if !strings.Contains(capturedBody, "Depends on: #10") {
		t.Error("body should contain dependency annotation for parent issue")
	}

	// Verify labels
	if len(capturedLabels) != 2 || capturedLabels[0] != "pilot" {
		t.Errorf("labels = %v, want [pilot autopilot-fix]", capturedLabels)
	}
}

func TestFeedbackLoop_CreateFailureIssue_PostMerge(t *testing.T) {
	capturedTitle := ""
	capturedBody := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/owner/repo/issues" && r.Method == "POST" {
			var input github.IssueInput
			_ = json.NewDecoder(r.Body).Decode(&input)
			capturedTitle = input.Title
			capturedBody = input.Body

			resp := github.Issue{Number: 101}
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
		PRURL:    "https://github.com/owner/repo/pull/42",
		HeadSHA:  "abc1234567890",
	}

	issueNum, err := fl.CreateFailureIssue(
		context.Background(),
		prState,
		FailureCIPostMerge,
		[]string{"deploy"},
		"",
		0,
	)

	if err != nil {
		t.Fatalf("CreateFailureIssue() error = %v", err)
	}
	if issueNum != 101 {
		t.Errorf("CreateFailureIssue() = %d, want 101", issueNum)
	}

	// Verify different title for post-merge
	expectedTitle := "fix(ci): resolve post-merge CI failure from PR #42"
	if capturedTitle != expectedTitle {
		t.Errorf("title = %q, want %q", capturedTitle, expectedTitle)
	}

	// Verify task instructions for post-merge
	if !strings.Contains(capturedBody, "PR was merged but CI failed afterward") {
		t.Error("body should contain post-merge specific instructions")
	}
}

func TestFeedbackLoop_CreateFailureIssue_MergeConflict(t *testing.T) {
	capturedTitle := ""
	capturedBody := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/owner/repo/issues" && r.Method == "POST" {
			var input github.IssueInput
			_ = json.NewDecoder(r.Body).Decode(&input)
			capturedTitle = input.Title
			capturedBody = input.Body

			resp := github.Issue{Number: 102}
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
		FailureMerge,
		nil,
		"",
		0,
	)

	if err != nil {
		t.Fatalf("CreateFailureIssue() error = %v", err)
	}
	if issueNum != 102 {
		t.Errorf("CreateFailureIssue() = %d, want 102", issueNum)
	}

	expectedTitle := "fix(merge): resolve merge conflict for PR #42"
	if capturedTitle != expectedTitle {
		t.Errorf("title = %q, want %q", capturedTitle, expectedTitle)
	}

	if !strings.Contains(capturedBody, "Resolve the merge conflicts") {
		t.Error("body should contain merge conflict instructions")
	}
}

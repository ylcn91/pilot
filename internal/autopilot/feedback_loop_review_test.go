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

func TestFormatReviewFeedback_FormatsComments(t *testing.T) {
	reviews := []*github.PullRequestReview{
		{ID: 1, User: github.User{Login: "alice"}, Body: "Please fix the error handling", State: "CHANGES_REQUESTED"},
	}
	comments := []*github.PRReviewComment{
		{ID: 10, Body: "This function needs a nil check", Path: "internal/foo.go", Line: 42, User: github.User{Login: "alice"}},
		{ID: 11, Body: "Missing test coverage", Path: "internal/foo.go", Line: 55, User: github.User{Login: "alice"}},
		{ID: 12, Body: "Rename this variable", Path: "internal/bar.go", Line: 10, User: github.User{Login: "bob"}},
	}

	result := formatReviewFeedback(reviews, comments)

	// Check that review body is included in a details block
	if !strings.Contains(result, "Review by alice") {
		t.Error("expected review body to reference alice")
	}
	if !strings.Contains(result, "Please fix the error handling") {
		t.Error("expected review body text")
	}

	// Check file-grouped comments
	if !strings.Contains(result, "internal/foo.go") {
		t.Error("expected foo.go file grouping")
	}
	if !strings.Contains(result, "internal/bar.go") {
		t.Error("expected bar.go file grouping")
	}
	if !strings.Contains(result, "Line 42") {
		t.Error("expected line number reference")
	}
	if !strings.Contains(result, "This function needs a nil check") {
		t.Error("expected comment body")
	}

	// Check <details> blocks
	detailsCount := strings.Count(result, "<details>")
	if detailsCount != 3 { // 1 review + 2 files
		t.Errorf("expected 3 <details> blocks, got %d", detailsCount)
	}
}

func TestFormatReviewFeedback_Truncation(t *testing.T) {
	// Create a very long review body that exceeds 4000 chars
	longBody := strings.Repeat("x", 5000)
	reviews := []*github.PullRequestReview{
		{ID: 1, User: github.User{Login: "alice"}, Body: longBody, State: "CHANGES_REQUESTED"},
	}

	result := formatReviewFeedback(reviews, nil)

	if len(result) > 4100 { // 4000 + truncation message
		t.Errorf("expected truncation, got length %d", len(result))
	}
	if !strings.Contains(result, "... (truncated)") {
		t.Error("expected truncation marker")
	}
}

func TestGenerateTitle_ReviewRequested(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AutoCreateIssues = true
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	fl := NewFeedbackLoop(ghClient, "owner", "repo", cfg)

	prState := &PRState{PRNumber: 42}
	title := fl.generateTitle(prState, FailureReviewRequested)

	expected := "fix(review): address review feedback on PR #42"
	if title != expected {
		t.Errorf("title = %q, want %q", title, expected)
	}
}

func TestCreateReviewIssue(t *testing.T) {
	issueCreated := false
	var createdTitle string
	var createdBody string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			issueCreated = true
			var input github.IssueInput
			_ = json.NewDecoder(r.Body).Decode(&input)
			createdTitle = input.Title
			createdBody = input.Body
			resp := github.Issue{Number: 100, Title: input.Title}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write(mustFLJSON(t, resp))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.AutoCreateIssues = true
	fl := NewFeedbackLoop(ghClient, "owner", "repo", cfg)

	prState := &PRState{
		PRNumber:    42,
		IssueNumber: 10,
		BranchName:  "pilot/GH-10",
	}

	reviews := []*github.PullRequestReview{
		{ID: 1, User: github.User{Login: "alice"}, Body: "Fix this", State: "CHANGES_REQUESTED"},
	}
	comments := []*github.PRReviewComment{
		{ID: 10, Body: "Bad code", Path: "foo.go", Line: 5, User: github.User{Login: "alice"}},
	}

	issueNum, err := fl.CreateReviewIssue(context.Background(), prState, reviews, comments, 1)
	if err != nil {
		t.Fatalf("CreateReviewIssue error: %v", err)
	}

	if !issueCreated {
		t.Fatal("expected issue to be created")
	}
	if issueNum != 100 {
		t.Errorf("issue number = %d, want 100", issueNum)
	}
	if !strings.Contains(createdTitle, "review feedback") {
		t.Errorf("title should contain 'review feedback': %s", createdTitle)
	}
	if !strings.Contains(createdBody, "Fix this") {
		t.Error("body should contain review text")
	}
	if !strings.Contains(createdBody, "foo.go") {
		t.Error("body should contain file path from comment")
	}
	if !strings.Contains(createdBody, "autopilot-meta") {
		t.Error("body should contain autopilot-meta")
	}
}

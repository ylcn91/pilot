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

func TestFeedbackLoop_IssueBody_TruncatesLogs(t *testing.T) {
	capturedBody := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/owner/repo/issues" && r.Method == "POST" {
			var input github.IssueInput
			_ = json.NewDecoder(r.Body).Decode(&input)
			capturedBody = input.Body

			resp := github.Issue{Number: 105}
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

	// Create very long logs (over 2000 chars)
	longLogs := strings.Repeat("ERROR: This is a very long log line that repeats. ", 100)
	if len(longLogs) <= 2000 {
		t.Fatal("test logs should be longer than 2000 chars")
	}

	_, err := fl.CreateFailureIssue(
		context.Background(),
		prState,
		FailureCIPreMerge,
		[]string{"build"},
		longLogs,
		0,
	)

	if err != nil {
		t.Fatalf("CreateFailureIssue() error = %v", err)
	}

	// Verify logs are truncated
	if !strings.Contains(capturedBody, "... (truncated)") {
		t.Error("body should indicate logs were truncated")
	}

	// Body should not contain the full logs
	if strings.Contains(capturedBody, longLogs) {
		t.Error("body should have truncated logs")
	}
}

func TestFeedbackLoop_IssueBody_NoLogs(t *testing.T) {
	capturedBody := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/owner/repo/issues" && r.Method == "POST" {
			var input github.IssueInput
			_ = json.NewDecoder(r.Body).Decode(&input)
			capturedBody = input.Body

			resp := github.Issue{Number: 106}
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

	_, err := fl.CreateFailureIssue(
		context.Background(),
		prState,
		FailureCIPreMerge,
		[]string{"build"},
		"", // No logs
		0,
	)

	if err != nil {
		t.Fatalf("CreateFailureIssue() error = %v", err)
	}

	// Should not have error logs section
	if strings.Contains(capturedBody, "Error Logs") {
		t.Error("body should not contain Error Logs section when no logs provided")
	}
}

func TestFeedbackLoop_IssueBody_NoFailedChecks(t *testing.T) {
	capturedBody := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/owner/repo/issues" && r.Method == "POST" {
			var input github.IssueInput
			_ = json.NewDecoder(r.Body).Decode(&input)
			capturedBody = input.Body

			resp := github.Issue{Number: 107}
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

	_, err := fl.CreateFailureIssue(
		context.Background(),
		prState,
		FailureCIPreMerge,
		nil, // No failed checks
		"Some error",
		0,
	)

	if err != nil {
		t.Fatalf("CreateFailureIssue() error = %v", err)
	}

	// Should not have failed checks section
	if strings.Contains(capturedBody, "Failed Checks") {
		t.Error("body should not contain Failed Checks section when no checks provided")
	}
}

func TestFeedbackLoop_IssueBody_NoIssueNumber(t *testing.T) {
	capturedBody := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/owner/repo/issues" && r.Method == "POST" {
			var input github.IssueInput
			_ = json.NewDecoder(r.Body).Decode(&input)
			capturedBody = input.Body

			resp := github.Issue{Number: 108}
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
		PRNumber:    42,
		HeadSHA:     "abc1234",
		IssueNumber: 0, // No linked issue
	}

	_, err := fl.CreateFailureIssue(
		context.Background(),
		prState,
		FailureCIPreMerge,
		[]string{"build"},
		"",
		0,
	)

	if err != nil {
		t.Fatalf("CreateFailureIssue() error = %v", err)
	}

	// Should not reference original issue
	if strings.Contains(capturedBody, "Original Issue") {
		t.Error("body should not contain Original Issue when no issue number")
	}
	// GH-1798: Should not contain dependency annotation when no parent issue
	if strings.Contains(capturedBody, "Depends on:") {
		t.Error("body should not contain dependency annotation when IssueNumber is 0")
	}
}

func TestFeedbackLoop_IssueBody_ShortSHA(t *testing.T) {
	capturedBody := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/owner/repo/issues" && r.Method == "POST" {
			var input github.IssueInput
			_ = json.NewDecoder(r.Body).Decode(&input)
			capturedBody = input.Body

			resp := github.Issue{Number: 109}
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
		HeadSHA:  "abc", // Very short SHA (less than 7 chars)
	}

	_, err := fl.CreateFailureIssue(
		context.Background(),
		prState,
		FailureCIPreMerge,
		[]string{"build"},
		"",
		0,
	)

	if err != nil {
		t.Fatalf("CreateFailureIssue() error = %v", err)
	}

	// Should not include SHA when too short
	if strings.Contains(capturedBody, "SHA") {
		t.Error("body should not contain SHA when SHA is too short")
	}
}

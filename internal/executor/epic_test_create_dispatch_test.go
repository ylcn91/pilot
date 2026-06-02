package executor

import (
	"context"
	"errors"
	"testing"
)

func TestCreateSubIssues_FallsBackToGitHubWhenNoAdapter(t *testing.T) {
	// This test verifies the dispatch logic chooses GitHub path when SourceAdapter is empty.
	// We test this by verifying the mock is NOT called, regardless of gh CLI outcome.
	runner := NewRunner()
	runner.SetIssueCreationEnabled(true)

	mock := &mockSubIssueCreator{
		Returns: []mockCreateIssueReturn{
			{Identifier: "APP-101", URL: "https://linear.app/test/issue/APP-101"},
		},
	}
	runner.SetSubIssueCreator(mock)

	// No SourceAdapter set - should use GitHub path
	plan := &EpicPlan{
		ParentTask: &Task{
			ID: "GH-100",
			// SourceAdapter not set - defaults to empty string
		},
		Subtasks: []PlannedSubtask{
			{Title: "Add test subtask", Description: "Test", Order: 1},
		},
	}

	ctx := context.Background()
	// Run in a non-existent directory to ensure gh CLI fails
	// The important thing is that the mock adapter is NOT called
	_, _ = runner.CreateSubIssues(ctx, plan, "/nonexistent/path")

	// Mock should NOT have been called since we fall back to GitHub
	if len(mock.Called) != 0 {
		t.Errorf("Expected 0 calls to adapter when SourceAdapter is empty, got %d", len(mock.Called))
	}
}

func TestCreateSubIssues_FallsBackToGitHubWhenAdapterIsGitHub(t *testing.T) {
	// This test verifies the dispatch logic chooses GitHub path when SourceAdapter is "github".
	// We test this by verifying the mock is NOT called, regardless of gh CLI outcome.
	runner := NewRunner()
	runner.SetIssueCreationEnabled(true)

	mock := &mockSubIssueCreator{
		Returns: []mockCreateIssueReturn{
			{Identifier: "101", URL: "https://github.com/test/issue/101"},
		},
	}
	runner.SetSubIssueCreator(mock)

	// SourceAdapter is "github" - should use GitHub path, not adapter
	plan := &EpicPlan{
		ParentTask: &Task{
			ID:            "GH-100",
			SourceAdapter: "github",
			SourceIssueID: "100",
		},
		Subtasks: []PlannedSubtask{
			{Title: "Add test subtask", Description: "Test", Order: 1},
		},
	}

	ctx := context.Background()
	// Run in a non-existent directory to ensure gh CLI fails
	// The important thing is that the mock adapter is NOT called
	_, _ = runner.CreateSubIssues(ctx, plan, "/nonexistent/path")

	// Mock should NOT have been called since adapter is "github"
	if len(mock.Called) != 0 {
		t.Errorf("Expected 0 calls to adapter when SourceAdapter is 'github', got %d", len(mock.Called))
	}
}

func TestCreateSubIssues_FallsBackToGitHubWhenNoCreator(t *testing.T) {
	// This test verifies that when SubIssueCreator is nil, even with a non-GitHub
	// SourceAdapter, we fall back to the GitHub path (and don't panic).
	runner := NewRunner()
	runner.SetIssueCreationEnabled(true)
	// SubIssueCreator not set

	plan := &EpicPlan{
		ParentTask: &Task{
			ID:            "APP-100",
			SourceAdapter: "linear",
			SourceIssueID: "APP-100",
		},
		Subtasks: []PlannedSubtask{
			{Title: "Add test subtask", Description: "Test", Order: 1},
		},
	}

	ctx := context.Background()
	// Run in a non-existent directory to ensure the GitHub path fails before creation.
	_, err := runner.CreateSubIssues(ctx, plan, "/nonexistent/path")

	if err == nil {
		t.Fatal("expected GitHub fallback to fail in non-repo dir")
	}
	if !errors.Is(err, ErrRepoNotInConfig) {
		t.Errorf("expected repo guardrail error, got: %v", err)
	}
}

func TestCreatedIssue_IdentifierField(t *testing.T) {
	// Test that Identifier field is properly set for different adapters
	tests := []struct {
		name       string
		issue      CreatedIssue
		wantNumber int
		wantIdent  string
	}{
		{
			name: "github issue",
			issue: CreatedIssue{
				Number:     123,
				Identifier: "123",
				URL:        "https://github.com/owner/repo/issues/123",
			},
			wantNumber: 123,
			wantIdent:  "123",
		},
		{
			name: "linear issue",
			issue: CreatedIssue{
				Number:     0,
				Identifier: "APP-456",
				URL:        "https://linear.app/team/issue/APP-456",
			},
			wantNumber: 0,
			wantIdent:  "APP-456",
		},
		{
			name: "jira issue",
			issue: CreatedIssue{
				Number:     0,
				Identifier: "PROJ-789",
				URL:        "https://jira.example.com/browse/PROJ-789",
			},
			wantNumber: 0,
			wantIdent:  "PROJ-789",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.issue.Number != tt.wantNumber {
				t.Errorf("Number = %d, want %d", tt.issue.Number, tt.wantNumber)
			}
			if tt.issue.Identifier != tt.wantIdent {
				t.Errorf("Identifier = %q, want %q", tt.issue.Identifier, tt.wantIdent)
			}
		})
	}
}

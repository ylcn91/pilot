package executor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestSetSubIssueCreator(t *testing.T) {
	runner := NewRunner()
	runner.SetIssueCreationEnabled(true)

	// Should be nil by default
	if runner.subIssueCreator != nil {
		t.Error("subIssueCreator should be nil by default")
	}

	// Set creator
	mock := &mockSubIssueCreator{}
	runner.SetSubIssueCreator(mock)

	if runner.subIssueCreator == nil {
		t.Fatal("subIssueCreator should be set after SetSubIssueCreator")
	}
}

func TestCreateSubIssues_DisabledByDefault(t *testing.T) {
	runner := NewRunner()
	plan := &EpicPlan{
		ParentTask: &Task{
			ID:            "APP-100",
			SourceAdapter: "linear",
			SourceIssueID: "APP-100",
		},
		Subtasks: []PlannedSubtask{
			{Title: "feat(linear): add first subtask", Description: "Do first thing", Order: 1},
		},
	}

	_, err := runner.CreateSubIssues(context.Background(), plan, "")
	if !errors.Is(err, ErrIssueCreationDisabled) {
		t.Fatalf("CreateSubIssues error = %v, want ErrIssueCreationDisabled", err)
	}
}

func TestCreateSubIssues_UsesAdapterForNonGitHub(t *testing.T) {
	runner := NewRunner()
	runner.SetIssueCreationEnabled(true)

	mock := &mockSubIssueCreator{
		Returns: []mockCreateIssueReturn{
			{Identifier: "APP-101", URL: "https://linear.app/test/issue/APP-101"},
			{Identifier: "APP-102", URL: "https://linear.app/test/issue/APP-102"},
		},
	}
	runner.SetSubIssueCreator(mock)

	plan := &EpicPlan{
		ParentTask: &Task{
			ID:            "APP-100",
			Title:         "feat(linear): implement workflow",
			SourceAdapter: "linear",
			SourceIssueID: "APP-100",
		},
		Subtasks: []PlannedSubtask{
			{Title: "feat(linear): add first subtask", Description: "Do first thing", Order: 1},
			{Title: "feat(linear): add second subtask", Description: "Do second thing", Order: 2},
		},
	}

	ctx := context.Background()
	created, err := runner.CreateSubIssues(ctx, plan, "")

	if err != nil {
		t.Fatalf("CreateSubIssues failed: %v", err)
	}

	// Should have called the mock twice
	if len(mock.Called) != 2 {
		t.Errorf("Expected 2 calls to CreateIssue, got %d", len(mock.Called))
	}

	// Verify first call
	if mock.Called[0].ParentID != "APP-100" {
		t.Errorf("First call parentID = %q, want APP-100", mock.Called[0].ParentID)
	}
	if mock.Called[0].Title != "feat(linear): add first subtask" {
		t.Errorf("First call title = %q, want 'feat(linear): add first subtask'", mock.Called[0].Title)
	}

	// Verify second call
	if mock.Called[1].ParentID != "APP-100" {
		t.Errorf("Second call parentID = %q, want APP-100", mock.Called[1].ParentID)
	}

	// Verify returned issues
	if len(created) != 2 {
		t.Fatalf("Expected 2 created issues, got %d", len(created))
	}

	if created[0].Identifier != "APP-101" {
		t.Errorf("First issue Identifier = %q, want APP-101", created[0].Identifier)
	}
	if created[0].Number != 0 {
		t.Errorf("First issue Number = %d, want 0 (non-GitHub)", created[0].Number)
	}
	if created[0].URL != "https://linear.app/test/issue/APP-101" {
		t.Errorf("First issue URL = %q, want linear URL", created[0].URL)
	}

	if created[1].Identifier != "APP-102" {
		t.Errorf("Second issue Identifier = %q, want APP-102", created[1].Identifier)
	}
}

func TestCreateSubIssues_AdapterError(t *testing.T) {
	runner := NewRunner()
	runner.SetIssueCreationEnabled(true)

	expectedErr := fmt.Errorf("Linear API error: rate limited")
	mock := &mockSubIssueCreator{
		Returns: []mockCreateIssueReturn{
			{Err: expectedErr},
		},
	}
	runner.SetSubIssueCreator(mock)

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
	_, err := runner.CreateSubIssues(ctx, plan, "")

	if err == nil {
		t.Fatal("Expected error from adapter")
	}
	if !strings.Contains(err.Error(), "Linear API error") {
		t.Errorf("Expected adapter error in message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "linear adapter") {
		t.Errorf("Expected adapter name in error, got: %v", err)
	}
}

func TestCreateSubIssues_AdapterWiresDependsOnAnnotations(t *testing.T) {
	// GH-1794: Verify DependsOn annotations are written into sub-issue bodies
	runner := NewRunner()
	runner.SetIssueCreationEnabled(true)

	mock := &mockSubIssueCreator{
		Returns: []mockCreateIssueReturn{
			{Identifier: "APP-101", URL: "https://linear.app/test/issue/APP-101"},
			{Identifier: "APP-102", URL: "https://linear.app/test/issue/APP-102"},
			{Identifier: "APP-103", URL: "https://linear.app/test/issue/APP-103"},
		},
	}
	runner.SetSubIssueCreator(mock)

	plan := &EpicPlan{
		ParentTask: &Task{
			ID:            "APP-100",
			SourceAdapter: "linear",
			SourceIssueID: "APP-100",
		},
		Subtasks: []PlannedSubtask{
			{Title: "Setup infrastructure", Description: "Create base", Order: 1},
			{Title: "Add feature", Description: "Build feature", Order: 2, DependsOn: []int{1}},
			{Title: "Add tests", Description: "Write tests", Order: 3, DependsOn: []int{1, 2}},
		},
	}

	ctx := context.Background()
	created, err := runner.CreateSubIssues(ctx, plan, "")
	if err != nil {
		t.Fatalf("CreateSubIssues failed: %v", err)
	}

	if len(created) != 3 {
		t.Fatalf("Expected 3 created issues, got %d", len(created))
	}

	// First issue: no dependencies
	if strings.Contains(mock.Called[0].Body, "Depends on:") {
		t.Errorf("First issue should not have dependency annotation, body: %s", mock.Called[0].Body)
	}

	// Second issue: depends on APP-101
	if !strings.Contains(mock.Called[1].Body, "Depends on: APP-101") {
		t.Errorf("Second issue body should contain 'Depends on: APP-101', got: %s", mock.Called[1].Body)
	}

	// Third issue: depends on both APP-101 and APP-102
	if !strings.Contains(mock.Called[2].Body, "Depends on: APP-101") {
		t.Errorf("Third issue body should contain 'Depends on: APP-101', got: %s", mock.Called[2].Body)
	}
	if !strings.Contains(mock.Called[2].Body, "Depends on: APP-102") {
		t.Errorf("Third issue body should contain 'Depends on: APP-102', got: %s", mock.Called[2].Body)
	}
}

func TestCreateSubIssues_AdapterNoDependsOnWhenEmpty(t *testing.T) {
	// GH-1794: Verify no annotation is added when DependsOn is empty
	runner := NewRunner()
	runner.SetIssueCreationEnabled(true)

	mock := &mockSubIssueCreator{
		Returns: []mockCreateIssueReturn{
			{Identifier: "APP-101", URL: "https://linear.app/test/issue/APP-101"},
			{Identifier: "APP-102", URL: "https://linear.app/test/issue/APP-102"},
		},
	}
	runner.SetSubIssueCreator(mock)

	plan := &EpicPlan{
		ParentTask: &Task{
			ID:            "APP-100",
			SourceAdapter: "linear",
			SourceIssueID: "APP-100",
		},
		Subtasks: []PlannedSubtask{
			{Title: "Add first", Description: "Do first", Order: 1},
			{Title: "Add second", Description: "Do second", Order: 2},
		},
	}

	ctx := context.Background()
	_, err := runner.CreateSubIssues(ctx, plan, "")
	if err != nil {
		t.Fatalf("CreateSubIssues failed: %v", err)
	}

	// Neither issue should have dependency annotations
	for i, call := range mock.Called {
		if strings.Contains(call.Body, "Depends on:") {
			t.Errorf("Issue %d should not have dependency annotation when DependsOn is empty, body: %s", i+1, call.Body)
		}
	}
}

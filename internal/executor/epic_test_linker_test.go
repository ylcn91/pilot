package executor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSetSubIssueLinker_WiresField(t *testing.T) {
	r := NewRunner()
	r.SetIssueCreationEnabled(true)
	if r.subIssueLinker != nil {
		t.Fatal("expected nil subIssueLinker before Set")
	}
	mock := &mockSubIssueLinker{}
	r.SetSubIssueLinker(mock)
	if r.subIssueLinker == nil {
		t.Fatal("expected non-nil subIssueLinker after Set")
	}
}

func TestCreateSubIssues_LinkerInvokedAfterGhCreate(t *testing.T) {
	// Use a fake "gh" binary that echoes a fake issue URL so the CLI path succeeds.
	fakeBin := t.TempDir()
	script := filepath.Join(fakeBin, "gh")
	err := os.WriteFile(script, []byte("#!/bin/sh\necho https://github.com/owner/testrepo/issues/42\n"), 0o755)
	if err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	origPATH := os.Getenv("PATH")
	t.Setenv("PATH", fakeBin+string(filepath.ListSeparator)+origPATH)

	mock := &mockSubIssueLinker{}
	runner := NewRunner()
	runner.SetIssueCreationEnabled(true)
	runner.SetSubIssueLinker(mock)
	runner.SetRepoAllowlist(&staticAllowlist{repos: []string{"ylcn91/pilot"}})
	worktree := makeAllowedGitHubWorktree(t, "ylcn91/pilot")

	plan := &EpicPlan{
		ParentTask: &Task{
			ID:            "GH-10",
			SourceRepo:    "owner/testrepo",
			SourceIssueID: "10",
		},
		Subtasks: []PlannedSubtask{
			{Title: "Add child task", Description: "Do it", Order: 1},
		},
	}

	ctx := context.Background()
	created, err := runner.CreateSubIssues(ctx, plan, worktree)
	if err != nil {
		t.Fatalf("CreateSubIssues failed: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("expected 1 created issue, got %d", len(created))
	}
	if created[0].Number != 42 {
		t.Errorf("issue number = %d, want 42", created[0].Number)
	}

	// Linker must have been called exactly once with the right args
	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 LinkSubIssue call, got %d", len(mock.Calls))
	}
	call := mock.Calls[0]
	if call.Owner != "owner" {
		t.Errorf("owner = %q, want owner", call.Owner)
	}
	if call.Repo != "testrepo" {
		t.Errorf("repo = %q, want testrepo", call.Repo)
	}
	if call.ParentNum != 10 {
		t.Errorf("parentNum = %d, want 10", call.ParentNum)
	}
	if call.ChildNum != 42 {
		t.Errorf("childNum = %d, want 42", call.ChildNum)
	}
}

func TestCreateSubIssues_LinkerErrorIsNonFatal(t *testing.T) {
	// Fake "gh" binary returns a valid URL; linker returns an error.
	// CreateSubIssues must still succeed (linker error is warn-only).
	fakeBin := t.TempDir()
	script := filepath.Join(fakeBin, "gh")
	err := os.WriteFile(script, []byte("#!/bin/sh\necho https://github.com/owner/testrepo/issues/99\n"), 0o755)
	if err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	origPATH := os.Getenv("PATH")
	t.Setenv("PATH", fakeBin+string(filepath.ListSeparator)+origPATH)

	mock := &mockSubIssueLinker{
		ErrFn: func(_, _ string, _, _ int) error {
			return fmt.Errorf("graphql mutation failed")
		},
	}
	runner := NewRunner()
	runner.SetIssueCreationEnabled(true)
	runner.SetSubIssueLinker(mock)
	runner.SetRepoAllowlist(&staticAllowlist{repos: []string{"ylcn91/pilot"}})
	worktree := makeAllowedGitHubWorktree(t, "ylcn91/pilot")

	plan := &EpicPlan{
		ParentTask: &Task{
			ID:            "GH-5",
			SourceRepo:    "owner/testrepo",
			SourceIssueID: "5",
		},
		Subtasks: []PlannedSubtask{
			{Title: "Add child", Description: "child", Order: 1},
		},
	}

	ctx := context.Background()
	created, err := runner.CreateSubIssues(ctx, plan, worktree)
	if err != nil {
		t.Fatalf("CreateSubIssues must succeed even when linker errors: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("expected 1 created issue, got %d", len(created))
	}
	// Linker was called (and returned error) but creation succeeded
	if len(mock.Calls) != 1 {
		t.Errorf("expected linker called once, got %d", len(mock.Calls))
	}
}

func TestCreateSubIssues_LinkerSkippedWhenSourceRepoEmpty(t *testing.T) {
	// When SourceRepo is empty, linker must NOT be called even if set.
	fakeBin := t.TempDir()
	script := filepath.Join(fakeBin, "gh")
	err := os.WriteFile(script, []byte("#!/bin/sh\necho https://github.com/owner/testrepo/issues/7\n"), 0o755)
	if err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	origPATH := os.Getenv("PATH")
	t.Setenv("PATH", fakeBin+string(filepath.ListSeparator)+origPATH)

	mock := &mockSubIssueLinker{}
	runner := NewRunner()
	runner.SetIssueCreationEnabled(true)
	runner.SetSubIssueLinker(mock)
	runner.SetRepoAllowlist(&staticAllowlist{repos: []string{"ylcn91/pilot"}})
	worktree := makeAllowedGitHubWorktree(t, "ylcn91/pilot")

	plan := &EpicPlan{
		ParentTask: &Task{
			ID: "GH-3",
			// SourceRepo intentionally empty
		},
		Subtasks: []PlannedSubtask{
			{Title: "Add child", Description: "child", Order: 1},
		},
	}

	ctx := context.Background()
	_, _ = runner.CreateSubIssues(ctx, plan, worktree)

	if len(mock.Calls) != 0 {
		t.Errorf("linker must not be called when SourceRepo is empty, got %d calls", len(mock.Calls))
	}
}

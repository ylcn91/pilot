package executor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestCreateSubIssuesViaGitHub_PropagatesNoDecomposeLabel verifies that a parent
// carrying the no-decompose label causes createSubIssuesViaGitHub to pass
// --label no-decompose to the gh CLI in addition to --label pilot.
func TestCreateSubIssuesViaGitHub_PropagatesNoDecomposeLabel(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "captured_args.txt")

	fakeBin := t.TempDir()
	script := filepath.Join(fakeBin, "gh")
	// Write all CLI arguments to argsFile, one per line, then emit a valid URL.
	scriptContent := fmt.Sprintf(`#!/bin/sh
for arg in "$@"; do printf '%%s\n' "$arg"; done > %q
echo https://github.com/owner/repo/issues/99
`, argsFile)
	if err := os.WriteFile(script, []byte(scriptContent), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	origPATH := os.Getenv("PATH")
	t.Setenv("PATH", fakeBin+string(filepath.ListSeparator)+origPATH)

	runner := NewRunner()
	runner.SetIssueCreationEnabled(true)
	runner.SetRepoAllowlist(&staticAllowlist{repos: []string{"ylcn91/pilot"}})
	worktree := makeAllowedGitHubWorktree(t, "ylcn91/pilot")
	plan := &EpicPlan{
		ParentTask: &Task{
			ID:            "GH-99",
			SourceRepo:    "owner/repo",
			SourceIssueID: "99",
			Labels:        []string{"pilot", "no-decompose"},
		},
		Subtasks: []PlannedSubtask{
			{Title: "feat(epic): implement sub-task", Description: "Do the thing.", Order: 1},
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

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read captured args: %v", err)
	}
	args := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")

	foundPilot, foundNoDecompose := false, false
	for i, a := range args {
		if a == "--label" && i+1 < len(args) {
			switch args[i+1] {
			case "pilot":
				foundPilot = true
			case "no-decompose":
				foundNoDecompose = true
			}
		}
	}
	if !foundPilot {
		t.Errorf("gh args missing --label pilot; args = %v", args)
	}
	if !foundNoDecompose {
		t.Errorf("gh args missing --label no-decompose; args = %v", args)
	}
}

// TestCreateSubIssuesViaAdapter_PropagatesParentLabels verifies that the adapter
// path passes propagatable parent labels (area:foo) to CreateIssue alongside pilot.
func TestCreateSubIssuesViaAdapter_PropagatesParentLabels(t *testing.T) {
	mock := &mockSubIssueCreator{
		Returns: []mockCreateIssueReturn{
			{Identifier: "APP-77", URL: "https://linear.app/team/issue/APP-77"},
		},
	}

	runner := NewRunner()
	runner.SetIssueCreationEnabled(true)
	runner.SetSubIssueCreator(mock)

	plan := &EpicPlan{
		ParentTask: &Task{
			ID:            "APP-50",
			SourceAdapter: "linear",
			SourceIssueID: "APP-50",
			Title:         "Parent epic",
			Labels:        []string{"pilot", "area:foo"},
		},
		Subtasks: []PlannedSubtask{
			{Title: "feat(api): add endpoint", Description: "Implement endpoint.", Order: 1},
		},
	}

	ctx := context.Background()
	if _, err := runner.CreateSubIssues(ctx, plan, ""); err != nil {
		t.Fatalf("CreateSubIssues failed: %v", err)
	}
	if len(mock.Called) != 1 {
		t.Fatalf("expected 1 CreateIssue call, got %d", len(mock.Called))
	}

	gotLabels := mock.Called[0].Labels
	wantLabels := []string{"pilot", "area:foo"}
	if !reflect.DeepEqual(gotLabels, wantLabels) {
		t.Errorf("CreateIssue labels = %v, want %v", gotLabels, wantLabels)
	}
}

// TestCreateSubIssues_RefusesClosedParent verifies that CreateSubIssues returns
// ErrParentDone when the parent task carries a terminal label or closed state.
func TestCreateSubIssues_RefusesClosedParent(t *testing.T) {
	cases := []struct {
		name   string
		labels []string
		state  string
	}{
		{name: "pilot-done label", labels: []string{"pilot-done"}},
		{name: "pilot-skip label", labels: []string{"pilot-skip"}},
		{name: "closed state", state: "closed"},
		{name: "merged state", state: "merged"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRunner()
			r.SetIssueCreationEnabled(true)
			r.dryRun = true
			r.openSubIssueCheck = func(_ context.Context, _, _ string) (bool, error) {
				return false, nil // dedup guard must not be reached
			}
			plan := &EpicPlan{
				ParentTask: &Task{
					ID:     "GH-999",
					Title:  "feat(scope): some epic",
					Labels: tc.labels,
					State:  tc.state,
				},
				Subtasks: []PlannedSubtask{
					{Order: 1, Title: "feat(scope): sub-task one"},
				},
			}
			_, err := r.CreateSubIssues(context.Background(), plan, "")
			if err != ErrParentDone {
				t.Errorf("expected ErrParentDone, got %v", err)
			}
		})
	}
}

// TestCreateSubIssues_RefusesRecentlyClosedSiblings verifies that CreateSubIssues returns
// ErrSubIssuesAlreadyExist when the injectable checker reports a recent sibling exists,
// even when the parent itself is open.
func TestCreateSubIssues_RefusesRecentlyClosedSiblings(t *testing.T) {
	r := NewRunner()
	r.SetIssueCreationEnabled(true)
	r.dryRun = true
	r.SetRepoAllowlist(&staticAllowlist{repos: []string{"ylcn91/pilot"}})
	worktree := makeAllowedGitHubWorktree(t, "ylcn91/pilot")
	r.openSubIssueCheck = func(_ context.Context, _, _ string) (bool, error) {
		return true, nil // simulate a recently-closed sibling
	}
	plan := &EpicPlan{
		ParentTask: &Task{
			ID:    "GH-1000",
			Title: "feat(scope): another epic",
			State: "open",
		},
		Subtasks: []PlannedSubtask{
			{Order: 1, Title: "feat(scope): sub-task one"},
		},
	}
	_, err := r.CreateSubIssues(context.Background(), plan, worktree)
	if err != ErrSubIssuesAlreadyExist {
		t.Errorf("expected ErrSubIssuesAlreadyExist, got %v", err)
	}
}

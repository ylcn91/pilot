package executor

import (
	"context"
	"errors"
	"testing"
)

// TestIsParentDone_LiveFallback exercises the GH-201 defensive path: whenever
// State is empty and the task ID looks like a GitHub issue, isParentDone must
// consult a live lookup — Labels alone are inconclusive because dispatcher-
// restored Tasks carry stale labels from queue time. Without this fallback,
// stale rows bypass the gate and spawn spurious sub-issues (the 2026-05-08
// GH-201 OAuth incident, 70+ dupes).
func TestIsParentDone_LiveFallback(t *testing.T) {
	cases := []struct {
		name           string
		task           *Task
		fallbackResult bool
		fallbackCalled bool
		want           bool
	}{
		{
			name:           "empty fields + GH- id + live says done",
			task:           &Task{ID: "GH-201"},
			fallbackResult: true,
			fallbackCalled: true,
			want:           true,
		},
		{
			name:           "empty fields + GH- id + live says open",
			task:           &Task{ID: "GH-201"},
			fallbackResult: false,
			fallbackCalled: true,
			want:           false,
		},
		{
			name:           "non-GH id skips fallback",
			task:           &Task{ID: "LIN-42"},
			fallbackResult: true,
			fallbackCalled: false,
			want:           false,
		},
		{
			name:           "populated state skips fallback",
			task:           &Task{ID: "GH-201", State: "open"},
			fallbackResult: true,
			fallbackCalled: false,
			want:           false,
		},
		{
			// Stale-labels case — the GH-201 residual hole. Labels populated
			// with non-terminal values from queue time, current GitHub state
			// is closed. Must consult fallback because labels are inconclusive.
			name:           "non-terminal labels still consult fallback when state empty",
			task:           &Task{ID: "GH-201", Labels: []string{"pilot", "area:executor"}},
			fallbackResult: true,
			fallbackCalled: true,
			want:           true,
		},
		{
			name:           "non-terminal labels + live says open → not done",
			task:           &Task{ID: "GH-201", Labels: []string{"pilot"}},
			fallbackResult: false,
			fallbackCalled: true,
			want:           false,
		},
		{
			name: "terminal label short-circuits before fallback",
			task: &Task{ID: "GH-201", Labels: []string{"pilot-done"}},
			// fallback should not be reached when a terminal label is present
			fallbackResult: false,
			fallbackCalled: false,
			want:           true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			orig := parentStateResolver
			parentStateResolver = func(taskID, dir string) bool {
				called = true
				return tc.fallbackResult
			}
			t.Cleanup(func() { parentStateResolver = orig })

			got := isParentDone(tc.task)
			if got != tc.want {
				t.Errorf("isParentDone = %v, want %v", got, tc.want)
			}
			if called != tc.fallbackCalled {
				t.Errorf("fallback called = %v, want %v", called, tc.fallbackCalled)
			}
		})
	}
}

// TestMustParentBeActionable verifies the #32 chokepoint: it returns
// ErrParentDone for done parents (closed/merged state or terminal label) and
// nil for actionable parents. parentStateResolver is pinned to a no-op by
// TestMain, so empty-State GH-* tasks resolve to actionable here.
func TestMustParentBeActionable(t *testing.T) {
	cases := []struct {
		name    string
		task    *Task
		wantErr bool
	}{
		{"nil task is actionable", nil, false},
		{"open state actionable", &Task{ID: "GH-1", State: "open"}, false},
		{"closed state done", &Task{ID: "GH-1", State: "closed"}, true},
		{"merged state done", &Task{ID: "GH-1", State: "merged"}, true},
		{"pilot-done label done", &Task{ID: "GH-1", Labels: []string{"pilot-done"}}, true},
		{"pilot-skip label done", &Task{ID: "GH-1", Labels: []string{"pilot-skip"}}, true},
		{"non-terminal label actionable", &Task{ID: "GH-1", Labels: []string{"pilot"}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := MustParentBeActionable(tc.task)
			if tc.wantErr && err == nil {
				t.Fatalf("MustParentBeActionable = nil, want ErrParentDone")
			}
			if tc.wantErr && !errors.Is(err, ErrParentDone) {
				t.Fatalf("MustParentBeActionable = %v, want ErrParentDone", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("MustParentBeActionable = %v, want nil", err)
			}
		})
	}
}

// TestRunner_Execute_EpicRecoversExistingSubIssues verifies that when
// CreateSubIssues returns ErrSubIssuesAlreadyExist and all recovered sub-issues
// are already closed, Execute returns a successful no-op without calling ExecuteSubIssues.
func TestRunner_Execute_EpicRecoversExistingSubIssues(t *testing.T) {
	r := NewRunner()
	r.SetIssueCreationEnabled(true)
	r.skipPreflightChecks = true
	r.dryRun = true
	r.SetRepoAllowlist(&staticAllowlist{repos: []string{"ylcn91/pilot"}})
	worktree := makeAllowedGitHubWorktree(t, "ylcn91/pilot")

	// openSubIssueCheck returns true → CreateSubIssues returns ErrSubIssuesAlreadyExist.
	r.openSubIssueCheck = func(_ context.Context, _, _ string) (bool, error) {
		return true, nil
	}

	// All recovered children are already closed — epic is done.
	r.recoverSubIssuesFn = func(_ context.Context, _, _ string) ([]CreatedIssue, error) {
		return []CreatedIssue{
			{Number: 10, Identifier: "10", URL: "https://github.com/o/r/issues/10", State: "closed"},
			{Number: 11, Identifier: "11", URL: "https://github.com/o/r/issues/11", State: "closed"},
		}, nil
	}

	// Track whether ExecuteSubIssues was entered — it must NOT be called.
	execCalled := false
	r.executeFunc = func(_ context.Context, _ *Task) (*ExecutionResult, error) {
		execCalled = true
		return &ExecutionResult{Success: true}, nil
	}

	// planEpicFn returns a multi-package plan (different directories) so CreateSubIssues is attempted.
	// Descriptions include file paths with surrounding text so isSinglePackageScope sees 2 directories.
	r.planEpicFn = func(_ context.Context, _ *Task, _ string) (*EpicPlan, error) {
		return &EpicPlan{
			ParentTask: &Task{ID: "GH-9000"},
			Subtasks: []PlannedSubtask{
				{Order: 1, Title: "feat(gateway): add websocket handler", Description: "Implement upgrade handler in internal/gateway/server.go"},
				{Order: 2, Title: "feat(adapters): add telegram bot", Description: "Wire bot client in internal/adapters/telegram/bot.go"},
			},
		}, nil
	}

	task := &Task{
		ID:          "GH-9000",
		Title:       "[epic] recover closed sub-issues test",
		ProjectPath: worktree,
	}

	result, err := r.Execute(context.Background(), task)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("expected successful result, got: %+v", result)
	}
	if execCalled {
		t.Error("ExecuteSubIssues should NOT have been called when all children are closed")
	}
	if !result.IsEpic {
		t.Error("expected IsEpic=true on recovered epic result")
	}
}

// TestRunner_Execute_EpicRecoversThenExecutesOpenChildren verifies that when
// CreateSubIssues returns ErrSubIssuesAlreadyExist and some recovered sub-issues
// are still open, Execute calls ExecuteSubIssues with only the open children.
func TestRunner_Execute_EpicRecoversThenExecutesOpenChildren(t *testing.T) {
	r := NewRunner()
	r.SetIssueCreationEnabled(true)
	r.skipPreflightChecks = true
	r.dryRun = true
	r.SetRepoAllowlist(&staticAllowlist{repos: []string{"ylcn91/pilot"}})
	worktree := makeAllowedGitHubWorktree(t, "ylcn91/pilot")

	// openSubIssueCheck returns true → CreateSubIssues returns ErrSubIssuesAlreadyExist.
	r.openSubIssueCheck = func(_ context.Context, _, _ string) (bool, error) {
		return true, nil
	}

	// Mix of open and closed children — only the open one should be executed.
	r.recoverSubIssuesFn = func(_ context.Context, _, _ string) ([]CreatedIssue, error) {
		return []CreatedIssue{
			{Number: 20, Identifier: "20", URL: "https://github.com/o/r/issues/20", State: "closed"},
			{Number: 21, Identifier: "21", URL: "https://github.com/o/r/issues/21", State: "open"},
		}, nil
	}

	// Capture which issue IDs were executed.
	var executedIDs []string
	r.executeFunc = func(_ context.Context, task *Task) (*ExecutionResult, error) {
		executedIDs = append(executedIDs, task.ID)
		return &ExecutionResult{TaskID: task.ID, Success: true}, nil
	}

	// planEpicFn returns a multi-package plan (different directories) so CreateSubIssues is attempted.
	// Descriptions include file paths with surrounding text so isSinglePackageScope sees 2 directories.
	r.planEpicFn = func(_ context.Context, _ *Task, _ string) (*EpicPlan, error) {
		return &EpicPlan{
			ParentTask: &Task{ID: "GH-9001"},
			Subtasks: []PlannedSubtask{
				{Order: 1, Title: "feat(gateway): add websocket handler", Description: "Implement upgrade handler in internal/gateway/server.go"},
				{Order: 2, Title: "feat(adapters): add telegram bot", Description: "Wire bot client in internal/adapters/telegram/bot.go"},
			},
		}, nil
	}

	task := &Task{
		ID:          "GH-9001",
		Title:       "[epic] recover open sub-issues test",
		ProjectPath: worktree,
	}

	result, err := r.Execute(context.Background(), task)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(executedIDs) == 0 {
		t.Error("ExecuteSubIssues should have been called for the open child")
	}
	// Exactly one execution: the open child GH-21. IDs are formatted as "GH-<number>".
	for _, id := range executedIDs {
		if id == "GH-20" {
			t.Errorf("closed child GH-20 should not have been executed")
		}
	}
}

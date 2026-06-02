package executor

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestSubIssueMergeWait_BlocksBetweenSubIssues verifies that the merge-wait function
// is called once per sub-issue except the last, and each call receives the correct PR number.
func TestSubIssueMergeWait_BlocksBetweenSubIssues(t *testing.T) {
	subIssues := makeSubIssues(3, 1000)

	prURLs := []string{
		"https://github.com/owner/repo/pull/2000",
		"https://github.com/owner/repo/pull/2001",
		"https://github.com/owner/repo/pull/2002",
	}

	callIdx := 0
	execFn := func(ctx context.Context, task *Task) (*ExecutionResult, error) {
		idx := callIdx
		callIdx++
		return &ExecutionResult{
			TaskID:  task.ID,
			Success: true,
			PRUrl:   prURLs[idx],
		}, nil
	}

	runner := newTestRunnerWithExecFunc(execFn)

	var waitedPRs []int
	runner.SetSubIssueMergeWait(func(ctx context.Context, prNumber int) error {
		waitedPRs = append(waitedPRs, prNumber)
		return nil
	})

	parent := &Task{ID: "GH-MW1", Title: "[epic] merge-wait ordering"}

	err := runner.ExecuteSubIssues(context.Background(), parent, subIssues, "", "")
	if err != nil {
		t.Fatalf("ExecuteSubIssues returned error: %v", err)
	}

	// Wait should be called for sub-issues 1 and 2, not 3 (last).
	if len(waitedPRs) != 2 {
		t.Fatalf("expected 2 merge-wait calls (all but last), got %d: %v", len(waitedPRs), waitedPRs)
	}
	if waitedPRs[0] != 2000 {
		t.Errorf("first wait PR = %d, want 2000", waitedPRs[0])
	}
	if waitedPRs[1] != 2001 {
		t.Errorf("second wait PR = %d, want 2001", waitedPRs[1])
	}
}

// TestSubIssueMergeWait_FailureAbortsEpic verifies that when the merge-wait function
// returns an error, ExecuteSubIssues aborts immediately and surfaces that error.
func TestSubIssueMergeWait_FailureAbortsEpic(t *testing.T) {
	subIssues := makeSubIssues(3, 1100)

	execCallCount := 0
	execFn := func(ctx context.Context, task *Task) (*ExecutionResult, error) {
		execCallCount++
		return &ExecutionResult{
			TaskID:  task.ID,
			Success: true,
			PRUrl:   fmt.Sprintf("https://github.com/owner/repo/pull/%d", 3000+execCallCount),
		}, nil
	}

	runner := newTestRunnerWithExecFunc(execFn)
	runner.SetSubIssueMergeWait(func(ctx context.Context, prNumber int) error {
		return fmt.Errorf("PR #%d was closed without merging", prNumber)
	})

	parent := &Task{ID: "GH-MW2", Title: "[epic] merge-wait abort"}

	err := runner.ExecuteSubIssues(context.Background(), parent, subIssues, "", "")
	if err == nil {
		t.Fatal("expected error when merge-wait fails, got nil")
	}

	// Only the first sub-issue should have been executed; the second must not start.
	if execCallCount != 1 {
		t.Errorf("expected 1 exec call before abort, got %d", execCallCount)
	}

	if !strings.Contains(err.Error(), "merge wait failed") {
		t.Errorf("error = %q, want substring 'merge wait failed'", err.Error())
	}
}

// TestSubIssueMergeWait_LastSubIssueSkipsWait verifies that with a single sub-issue
// (it is the last one), the merge-wait function is never called.
func TestSubIssueMergeWait_LastSubIssueSkipsWait(t *testing.T) {
	subIssues := makeSubIssues(1, 1200)

	execFn := func(ctx context.Context, task *Task) (*ExecutionResult, error) {
		return &ExecutionResult{
			TaskID:  task.ID,
			Success: true,
			PRUrl:   "https://github.com/owner/repo/pull/4000",
		}, nil
	}

	runner := newTestRunnerWithExecFunc(execFn)

	waitCalled := false
	runner.SetSubIssueMergeWait(func(ctx context.Context, prNumber int) error {
		waitCalled = true
		return nil
	})

	parent := &Task{ID: "GH-MW3", Title: "[epic] single sub-issue skip wait"}

	err := runner.ExecuteSubIssues(context.Background(), parent, subIssues, "", "")
	if err != nil {
		t.Fatalf("ExecuteSubIssues returned error: %v", err)
	}

	if waitCalled {
		t.Error("merge-wait should not be called for the last (only) sub-issue")
	}
}

// TestSubIssueMergeWait_NilFnDegradesgracefully verifies that when no merge-wait
// function is set, ExecuteSubIssues completes without panicking.
func TestSubIssueMergeWait_NilFnDegradesgracefully(t *testing.T) {
	subIssues := makeSubIssues(2, 1300)

	execFn := func(ctx context.Context, task *Task) (*ExecutionResult, error) {
		return &ExecutionResult{
			TaskID:  task.ID,
			Success: true,
			PRUrl:   "https://github.com/owner/repo/pull/5000",
		}, nil
	}

	runner := newTestRunnerWithExecFunc(execFn)
	// Intentionally NOT calling SetSubIssueMergeWait — nil fn must not panic.

	parent := &Task{ID: "GH-MW4", Title: "[epic] nil merge-wait no panic"}

	err := runner.ExecuteSubIssues(context.Background(), parent, subIssues, "", "")
	if err != nil {
		t.Fatalf("ExecuteSubIssues should not error with nil merge-wait fn: %v", err)
	}
}

package executor

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestSequentialEpicFlow is a table-driven integration test covering
// the full ExecuteSubIssues lifecycle: execution ordering, callback
// invocation, progress tracking, and failure handling.
func TestSequentialEpicFlow(t *testing.T) {
	tests := []struct {
		name string

		// numSubIssues is the number of sub-issues to create.
		numSubIssues int

		// resultFn returns the ExecutionResult for the i-th sub-issue (0-indexed).
		resultFn func(idx int, task *Task) (*ExecutionResult, error)

		// wantErr is true if ExecuteSubIssues should return an error.
		wantErr bool

		// wantErrContains is a substring expected in the error message (if wantErr).
		wantErrContains string

		// wantExecCount is the expected number of executeFunc invocations.
		wantExecCount int

		// wantPRCallbackCount is the expected number of PR callback invocations.
		wantPRCallbackCount int

		// wantPRNumbers lists the expected PR numbers from callbacks, in order.
		wantPRNumbers []int

		// wantBranches lists the expected branch names from exec calls, in order.
		wantBranches []string
	}{
		{
			name:         "happy path - 3 sub-issues all succeed",
			numSubIssues: 3,
			resultFn: func(idx int, task *Task) (*ExecutionResult, error) {
				prNum := 200 + idx
				return &ExecutionResult{
					TaskID:    task.ID,
					Success:   true,
					Output:    fmt.Sprintf("Completed %s", task.Title),
					PRUrl:     fmt.Sprintf("https://github.com/owner/repo/pull/%d", prNum),
					CommitSHA: fmt.Sprintf("sha-%d", idx),
				}, nil
			},
			wantErr:             false,
			wantExecCount:       3,
			wantPRCallbackCount: 3,
			wantPRNumbers:       []int{200, 201, 202},
			wantBranches: []string{
				"pilot/GH-100",
				"pilot/GH-101",
				"pilot/GH-102",
			},
		},
		{
			name:         "middle sub-issue fails - execution stops at failure",
			numSubIssues: 3,
			resultFn: func(idx int, task *Task) (*ExecutionResult, error) {
				if idx == 1 { // second sub-issue fails
					return &ExecutionResult{
						TaskID:  task.ID,
						Success: false,
						Error:   "CI check failed: lint errors",
					}, nil
				}
				prNum := 300 + idx
				return &ExecutionResult{
					TaskID:    task.ID,
					Success:   true,
					Output:    "done",
					PRUrl:     fmt.Sprintf("https://github.com/owner/repo/pull/%d", prNum),
					CommitSHA: fmt.Sprintf("sha-%d", idx),
				}, nil
			},
			wantErr:         true,
			wantErrContains: "sub-issue 101 failed",
			// Current behavior: abort on first failure.
			// First sub-issue succeeds (exec+callback), second fails (exec only), third never runs.
			wantExecCount:       2,
			wantPRCallbackCount: 1,
			wantPRNumbers:       []int{300},
			wantBranches: []string{
				"pilot/GH-100",
				"pilot/GH-101",
			},
		},
		{
			name:         "first sub-issue returns exec error - immediate abort",
			numSubIssues: 3,
			resultFn: func(idx int, task *Task) (*ExecutionResult, error) {
				if idx == 0 {
					return nil, fmt.Errorf("backend unavailable: connection refused")
				}
				return &ExecutionResult{
					TaskID:    task.ID,
					Success:   true,
					PRUrl:     "https://github.com/owner/repo/pull/999",
					CommitSHA: "sha-x",
				}, nil
			},
			wantErr:             true,
			wantErrContains:     "sub-issue 100 failed",
			wantExecCount:       1,
			wantPRCallbackCount: 0,
			wantPRNumbers:       nil,
			wantBranches: []string{
				"pilot/GH-100",
			},
		},
		{
			name:         "all sub-issues fail - first failure stops execution",
			numSubIssues: 3,
			resultFn: func(idx int, task *Task) (*ExecutionResult, error) {
				return &ExecutionResult{
					TaskID:  task.ID,
					Success: false,
					Error:   fmt.Sprintf("compilation error in sub-issue %d", idx+1),
				}, nil
			},
			wantErr:             true,
			wantErrContains:     "sub-issue 100 failed",
			wantExecCount:       1, // Stops at first failure
			wantPRCallbackCount: 0,
			wantPRNumbers:       nil,
			wantBranches: []string{
				"pilot/GH-100",
			},
		},
		{
			name:         "context cancellation - stops before next sub-issue",
			numSubIssues: 3,
			resultFn: func(idx int, task *Task) (*ExecutionResult, error) {
				// All would succeed, but context gets cancelled externally
				return &ExecutionResult{
					TaskID:    task.ID,
					Success:   true,
					Output:    "done",
					PRUrl:     fmt.Sprintf("https://github.com/owner/repo/pull/%d", 400+idx),
					CommitSHA: fmt.Sprintf("sha-%d", idx),
				}, nil
			},
			// Special handling: we cancel context after first execution.
			// See test body below for override.
			wantErr:             true,
			wantErrContains:     "execution cancelled",
			wantExecCount:       1,
			wantPRCallbackCount: 1,
			wantPRNumbers:       []int{400},
			wantBranches: []string{
				"pilot/GH-100",
			},
		},
		{
			// TASK-356 #1: a child commits real work but produces no PR — its work is
			// stranded in a worktree cleanup will discard. The work-loss guard halts the
			// epic loudly so the issue stays open for recovery, rather than silently
			// closing it and marching on (which lost the studio-sdk #17 port).
			name:         "sub-issue commits but no PR URL - work-loss guard halts epic",
			numSubIssues: 2,
			resultFn: func(idx int, task *Task) (*ExecutionResult, error) {
				if idx == 0 {
					// First sub-issue: committed work but PR creation never landed.
					return &ExecutionResult{
						TaskID:    task.ID,
						Success:   true,
						Output:    "committed but no PR",
						PRUrl:     "",
						CommitSHA: "sha-docs",
					}, nil
				}
				return &ExecutionResult{
					TaskID:    task.ID,
					Success:   true,
					Output:    "code updated",
					PRUrl:     "https://github.com/owner/repo/pull/500",
					CommitSHA: "sha-code",
				}, nil
			},
			wantErr:             true,
			wantErrContains:     "no PR",
			wantExecCount:       1, // halts after the first child; second never runs
			wantPRCallbackCount: 0, // callback never fires for a PR-less child
			wantBranches: []string{
				"pilot/GH-100",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issues := makeSubIssues(tt.numSubIssues, 100)
			parent := &Task{
				ID:    "GH-50",
				Title: "[epic] Integration test epic",
			}

			// Special case: context cancellation test
			if tt.name == "context cancellation - stops before next sub-issue" {
				ctx, cancel := context.WithCancel(context.Background())

				callCount := 0
				sr := newSequentialRunner(func(idx int, task *Task) (*ExecutionResult, error) {
					callCount++
					result, err := tt.resultFn(idx, task)
					// Cancel after first successful execution
					if callCount == 1 {
						cancel()
					}
					return result, err
				})

				err := sr.Runner.ExecuteSubIssues(ctx, parent, issues, parent.ProjectPath, "")

				if err == nil {
					t.Fatal("expected error from cancelled context")
				}
				if !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Errorf("error = %q, want substring %q", err.Error(), tt.wantErrContains)
				}
				if len(sr.ExecCalls) != tt.wantExecCount {
					t.Errorf("exec call count = %d, want %d", len(sr.ExecCalls), tt.wantExecCount)
				}
				if len(sr.PRCalls) != tt.wantPRCallbackCount {
					t.Errorf("PR callback count = %d, want %d", len(sr.PRCalls), tt.wantPRCallbackCount)
				}
				return
			}

			sr := newSequentialRunner(tt.resultFn)
			err := sr.Runner.ExecuteSubIssues(context.Background(), parent, issues, parent.ProjectPath, "")

			// Check error expectation
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.wantErrContains != "" && !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Errorf("error = %q, want substring %q", err.Error(), tt.wantErrContains)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}

			// Verify execution count
			if len(sr.ExecCalls) != tt.wantExecCount {
				t.Errorf("exec call count = %d, want %d", len(sr.ExecCalls), tt.wantExecCount)
			}

			// Verify PR callback count
			if len(sr.PRCalls) != tt.wantPRCallbackCount {
				t.Errorf("PR callback count = %d, want %d", len(sr.PRCalls), tt.wantPRCallbackCount)
			}

			// Verify PR numbers in order
			if tt.wantPRNumbers != nil {
				for i, want := range tt.wantPRNumbers {
					if i >= len(sr.PRCalls) {
						t.Errorf("missing PR callback at index %d", i)
						continue
					}
					if sr.PRCalls[i].PRNumber != want {
						t.Errorf("PR callback[%d].PRNumber = %d, want %d", i, sr.PRCalls[i].PRNumber, want)
					}
				}
			}

			// Verify branches in execution order
			if tt.wantBranches != nil {
				for i, want := range tt.wantBranches {
					if i >= len(sr.ExecCalls) {
						t.Errorf("missing exec call at index %d", i)
						continue
					}
					if sr.ExecCalls[i].Branch != want {
						t.Errorf("exec call[%d].Branch = %q, want %q", i, sr.ExecCalls[i].Branch, want)
					}
				}
			}
		})
	}
}

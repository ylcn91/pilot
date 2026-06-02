package executor

import (
	"context"
	"fmt"
	"sync"
)

// execCall records the arguments of an executeFunc invocation.
type execCall struct {
	TaskID string
	Branch string
	Title  string
	Order  int // 1-indexed execution order
}

// makeSubIssues creates n CreatedIssues numbered starting from startNum.
func makeSubIssues(n, startNum int) []CreatedIssue {
	issues := make([]CreatedIssue, n)
	for i := 0; i < n; i++ {
		num := startNum + i
		issues[i] = CreatedIssue{
			Number: num,
			URL:    fmt.Sprintf("https://github.com/owner/repo/issues/%d", num),
			Subtask: PlannedSubtask{
				Title:       fmt.Sprintf("Sub-issue %d", i+1),
				Description: fmt.Sprintf("Description for sub-issue %d", i+1),
				Order:       i + 1,
			},
		}
	}
	return issues
}

// sequentialRunner builds a Runner with a mock executeFunc and PR-callback
// collector. The executeFunc records each invocation order and delegates to
// the caller-supplied resultFn to decide success/failure per sub-issue.
type sequentialRunner struct {
	Runner    *Runner
	ExecCalls []execCall
	PRCalls   []subIssuePRCall
	mu        sync.Mutex
}

func newSequentialRunner(
	resultFn func(idx int, task *Task) (*ExecutionResult, error),
) *sequentialRunner {
	sr := &sequentialRunner{}

	callIdx := 0
	execFn := func(ctx context.Context, task *Task) (*ExecutionResult, error) {
		sr.mu.Lock()
		idx := callIdx
		callIdx++
		sr.ExecCalls = append(sr.ExecCalls, execCall{
			TaskID: task.ID,
			Branch: task.Branch,
			Title:  task.Title,
			Order:  idx + 1,
		})
		sr.mu.Unlock()
		return resultFn(idx, task)
	}

	runner := newTestRunnerWithExecFunc(execFn)
	runner.SetOnSubIssuePRCreated(func(prNumber int, prURL string, issueNumber int, commitSHA, branchName string, issueNodeID string) {
		sr.mu.Lock()
		defer sr.mu.Unlock()
		sr.PRCalls = append(sr.PRCalls, subIssuePRCall{
			PRNumber:    prNumber,
			PRURL:       prURL,
			IssueNumber: issueNumber,
			CommitSHA:   commitSHA,
			BranchName:  branchName,
		})
	})

	sr.Runner = runner
	return sr
}

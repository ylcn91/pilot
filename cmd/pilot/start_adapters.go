package main

import (
	"context"
	"fmt"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/memory"
)

// storeTaskChecker adapts memory.Store to the github.TaskChecker interface.
// GH-2201: Used by the poller to check if a task is still queued/in-progress
// before allowing retry after the grace period expires.
type storeTaskChecker struct {
	store *memory.Store
}

func (s storeTaskChecker) IsTaskQueued(taskID string) bool {
	queued, err := s.store.IsTaskQueued(taskID)
	if err != nil {
		return false // Don't block retry on DB errors
	}
	return queued
}

// preFlightJudgeShim adapts *executor.IntentJudge to the github.PreFlightJudger interface.
// GH-2802: Keeps the poller package decoupled from the executor package.
type preFlightJudgeShim struct {
	judge *executor.IntentJudge
}

func (s preFlightJudgeShim) JudgeIssue(ctx context.Context, title, body, repoContext string) (github.Verdict, error) {
	v, err := s.judge.JudgeIssue(ctx, title, body, repoContext)
	if err != nil {
		return github.Verdict{}, err
	}
	return github.Verdict{
		Accepted:   !v.IsRejection(),
		Decision:   string(v.Decision),
		Reason:     v.Reason,
		Confidence: v.Confidence,
	}, nil
}

// storeExecutionSaver adapts *memory.Store to the github.ExecutionSaver interface.
// GH-2802: Persists pre-flight rejection records for observability.
type storeExecutionSaver struct {
	store *memory.Store
}

func (s storeExecutionSaver) SaveDeclinedExecution(taskID, projectPath, status, reason string) error {
	now := time.Now()
	return s.store.SaveExecution(&memory.Execution{
		ID:          fmt.Sprintf("%s-preflight-%d", taskID, now.UnixNano()),
		TaskID:      taskID,
		ProjectPath: projectPath,
		Status:      status,
		Error:       reason,
		CreatedAt:   now,
		CompletedAt: &now,
	})
}

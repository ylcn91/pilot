package briefs

import (
	"time"
)

func createTestBrief() *Brief {
	now := time.Date(2026, 1, 26, 9, 0, 0, 0, time.UTC)
	completedAt := now.Add(-30 * time.Minute)

	return &Brief{
		GeneratedAt: now,
		Period: BriefPeriod{
			Start: now.Add(-24 * time.Hour),
			End:   now,
		},
		Completed: []TaskSummary{
			{
				ID:          "TASK-001",
				Title:       "Add user auth",
				ProjectPath: "/test/project",
				Status:      "completed",
				PRUrl:       "https://github.com/test/pr/1",
				DurationMs:  120000,
				CompletedAt: &completedAt,
			},
			{
				ID:          "TASK-002",
				Title:       "Fix login bug",
				ProjectPath: "/test/project",
				Status:      "completed",
				DurationMs:  60000,
				CompletedAt: &completedAt,
			},
		},
		InProgress: []TaskSummary{
			{
				ID:          "TASK-003",
				Title:       "Add payments",
				ProjectPath: "/test/project",
				Status:      "running",
				Progress:    65,
			},
		},
		Blocked: []BlockedTask{
			{
				TaskSummary: TaskSummary{
					ID:          "TASK-004",
					Title:       "API refactor",
					ProjectPath: "/test/project",
					Status:      "failed",
				},
				Error:    "tests failed: auth_test.go:42: expected 200, got 401",
				FailedAt: now.Add(-1 * time.Hour),
			},
		},
		Upcoming: []TaskSummary{
			{
				ID:          "TASK-005",
				Title:       "Dashboard redesign",
				ProjectPath: "/test/project",
				Status:      "queued",
			},
		},
		Metrics: BriefMetrics{
			TotalTasks:     20,
			CompletedCount: 17,
			FailedCount:    3,
			SuccessRate:    0.85,
			AvgDurationMs:  720000, // 12 minutes
			PRsCreated:     15,
		},
	}
}

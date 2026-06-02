package briefs

import (
	"fmt"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/memory"
)

func TestGeneratorGenerate(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Seed test data
	now := time.Now()
	executions := []*memory.Execution{
		{
			ID:          "exec-1",
			TaskID:      "TASK-001",
			ProjectPath: "/test/project",
			Status:      "completed",
			DurationMs:  120000,
			PRUrl:       "https://github.com/test/pr/1",
			CreatedAt:   now.Add(-1 * time.Hour),
			CompletedAt: timePtr(now.Add(-30 * time.Minute)),
		},
		{
			ID:          "exec-2",
			TaskID:      "TASK-002",
			ProjectPath: "/test/project",
			Status:      "completed",
			DurationMs:  60000,
			CreatedAt:   now.Add(-2 * time.Hour),
			CompletedAt: timePtr(now.Add(-1 * time.Hour)),
		},
		{
			ID:          "exec-3",
			TaskID:      "TASK-003",
			ProjectPath: "/test/project",
			Status:      "failed",
			Error:       "tests failed: auth_test.go:42",
			CreatedAt:   now.Add(-3 * time.Hour),
			CompletedAt: timePtr(now.Add(-2 * time.Hour)),
		},
	}

	for _, exec := range executions {
		if err := store.SaveExecution(exec); err != nil {
			t.Fatalf("failed to save execution: %v", err)
		}
	}

	// Create generator
	config := DefaultBriefConfig()
	config.Content.IncludeErrors = true
	generator := NewGenerator(store, config)

	// Generate brief for the past 24 hours
	period := BriefPeriod{
		Start: now.Add(-24 * time.Hour),
		End:   now,
	}

	brief, err := generator.Generate(period)
	if err != nil {
		t.Fatalf("failed to generate brief: %v", err)
	}

	// Verify results
	if len(brief.Completed) != 2 {
		t.Fatalf("expected 2 completed tasks, got %d", len(brief.Completed))
	}

	if len(brief.Blocked) != 1 {
		t.Fatalf("expected 1 blocked task, got %d", len(brief.Blocked))
	}

	if brief.Blocked[0].Error != "tests failed: auth_test.go:42" {
		t.Errorf("expected error message, got %s", brief.Blocked[0].Error)
	}

	if brief.Metrics.CompletedCount != 2 {
		t.Errorf("expected 2 completed in metrics, got %d", brief.Metrics.CompletedCount)
	}

	if brief.Metrics.FailedCount != 1 {
		t.Errorf("expected 1 failed in metrics, got %d", brief.Metrics.FailedCount)
	}
}

func TestGeneratorGenerateDaily(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	config := DefaultBriefConfig()
	config.Timezone = "UTC"
	generator := NewGenerator(store, config)

	brief, err := generator.GenerateDaily()
	if err != nil {
		t.Fatalf("failed to generate daily brief: %v", err)
	}

	if brief == nil {
		t.Fatal("expected brief, got nil")
	}

	// Period should be 24 hours
	duration := brief.Period.End.Sub(brief.Period.Start)
	if duration != 24*time.Hour {
		t.Errorf("expected 24h period, got %v", duration)
	}
}

func TestGeneratorGenerateWeekly(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	config := DefaultBriefConfig()
	config.Timezone = "UTC"
	generator := NewGenerator(store, config)

	brief, err := generator.GenerateWeekly()
	if err != nil {
		t.Fatalf("failed to generate weekly brief: %v", err)
	}

	if brief == nil {
		t.Fatal("expected brief, got nil")
	}

	// Period should be 7 days
	duration := brief.Period.End.Sub(brief.Period.Start)
	if duration != 7*24*time.Hour {
		t.Errorf("expected 168h (7 day) period, got %v", duration)
	}
}

func TestGeneratorWithProjectFilter(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Seed test data with different projects
	now := time.Now()
	executions := []*memory.Execution{
		{
			ID:          "exec-1",
			TaskID:      "TASK-001",
			ProjectPath: "/project/alpha",
			Status:      "completed",
			CreatedAt:   now.Add(-1 * time.Hour),
			CompletedAt: timePtr(now.Add(-30 * time.Minute)),
		},
		{
			ID:          "exec-2",
			TaskID:      "TASK-002",
			ProjectPath: "/project/beta",
			Status:      "completed",
			CreatedAt:   now.Add(-2 * time.Hour),
			CompletedAt: timePtr(now.Add(-1 * time.Hour)),
		},
	}

	for _, exec := range executions {
		if err := store.SaveExecution(exec); err != nil {
			t.Fatalf("failed to save execution: %v", err)
		}
	}

	// Create generator with filter
	config := DefaultBriefConfig()
	config.Filters.Projects = []string{"/project/alpha"}
	generator := NewGenerator(store, config)

	period := BriefPeriod{
		Start: now.Add(-24 * time.Hour),
		End:   now,
	}

	brief, err := generator.Generate(period)
	if err != nil {
		t.Fatalf("failed to generate brief: %v", err)
	}

	// Should only have alpha project. Fatalf (not Errorf) so the rare SQLite timing
	// flake that yields 0 rows fails cleanly here instead of panicking on Completed[0]
	// below (the "index out of range [0]" red). TASK-353 / learning_flaky_briefs_generator_test.
	if len(brief.Completed) != 1 {
		t.Fatalf("expected 1 completed task (filtered), got %d", len(brief.Completed))
	}

	if brief.Completed[0].ProjectPath != "/project/alpha" {
		t.Errorf("expected alpha project, got %s", brief.Completed[0].ProjectPath)
	}
}

func TestGeneratorGenerateWithActiveExecutions(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Seed active execution
	now := time.Now()
	exec := &memory.Execution{
		ID:          "exec-active",
		TaskID:      "TASK-ACTIVE",
		ProjectPath: "/test/project",
		Status:      "running",
		DurationMs:  60000, // 1 minute
		CreatedAt:   now.Add(-5 * time.Minute),
	}
	if err := store.SaveExecution(exec); err != nil {
		t.Fatalf("failed to save execution: %v", err)
	}

	config := DefaultBriefConfig()
	generator := NewGenerator(store, config)

	brief, err := generator.GenerateDaily()
	if err != nil {
		t.Fatalf("failed to generate brief: %v", err)
	}

	// Should have 0 completed (running is not in period query)
	if len(brief.Completed) != 0 {
		t.Errorf("expected 0 completed tasks, got %d", len(brief.Completed))
	}
}

func TestGeneratorGenerateWithQueuedTasks(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Seed queued task
	exec := &memory.Execution{
		ID:          "exec-queued",
		TaskID:      "TASK-QUEUED",
		ProjectPath: "/test/project",
		Status:      "queued",
		CreatedAt:   time.Now(),
	}
	if err := store.SaveExecution(exec); err != nil {
		t.Fatalf("failed to save execution: %v", err)
	}

	config := DefaultBriefConfig()
	generator := NewGenerator(store, config)

	brief, err := generator.GenerateDaily()
	if err != nil {
		t.Fatalf("failed to generate brief: %v", err)
	}

	// The queued task might appear in upcoming depending on store implementation
	// Just verify no error occurs
	if brief == nil {
		t.Fatal("expected brief, got nil")
	}
}

func TestGeneratorGenerateWithMaxItemsLimit(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Seed more tasks than MaxItemsPerSection
	now := time.Now()
	for i := 0; i < 15; i++ {
		exec := &memory.Execution{
			ID:          fmt.Sprintf("exec-%d", i),
			TaskID:      fmt.Sprintf("TASK-%03d", i),
			ProjectPath: "/test/project",
			Status:      "completed",
			CreatedAt:   now.Add(-time.Duration(i) * time.Hour),
			CompletedAt: timePtr(now.Add(-time.Duration(i)*time.Hour + 30*time.Minute)),
		}
		if err := store.SaveExecution(exec); err != nil {
			t.Fatalf("failed to save execution: %v", err)
		}
	}

	config := DefaultBriefConfig()
	config.Content.MaxItemsPerSection = 5
	generator := NewGenerator(store, config)

	period := BriefPeriod{
		Start: now.Add(-24 * time.Hour),
		End:   now.Add(time.Hour),
	}

	brief, err := generator.Generate(period)
	if err != nil {
		t.Fatalf("failed to generate brief: %v", err)
	}

	// Should be limited to max items
	if len(brief.Completed) > 5 {
		t.Errorf("expected at most 5 completed tasks, got %d", len(brief.Completed))
	}
}

func TestGeneratorGenerateWithoutErrors(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Seed a failed task
	now := time.Now()
	exec := &memory.Execution{
		ID:          "exec-failed",
		TaskID:      "TASK-FAILED",
		ProjectPath: "/test/project",
		Status:      "failed",
		Error:       "something went wrong",
		CreatedAt:   now.Add(-1 * time.Hour),
		CompletedAt: timePtr(now.Add(-30 * time.Minute)),
	}
	if err := store.SaveExecution(exec); err != nil {
		t.Fatalf("failed to save execution: %v", err)
	}

	// Disable errors in config
	config := DefaultBriefConfig()
	config.Content.IncludeErrors = false
	generator := NewGenerator(store, config)

	period := BriefPeriod{
		Start: now.Add(-24 * time.Hour),
		End:   now,
	}

	brief, err := generator.Generate(period)
	if err != nil {
		t.Fatalf("failed to generate brief: %v", err)
	}

	// Should not include blocked tasks when errors are disabled
	if len(brief.Blocked) != 0 {
		t.Errorf("expected 0 blocked tasks with IncludeErrors=false, got %d", len(brief.Blocked))
	}
}

func TestGeneratorGenerateWeeklyOnDifferentDays(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	config := DefaultBriefConfig()
	config.Timezone = "UTC"
	generator := NewGenerator(store, config)

	brief, err := generator.GenerateWeekly()
	if err != nil {
		t.Fatalf("failed to generate weekly brief: %v", err)
	}

	// Period should be exactly 7 days
	duration := brief.Period.End.Sub(brief.Period.Start)
	if duration != 7*24*time.Hour {
		t.Errorf("expected 7 day period, got %v", duration)
	}
}

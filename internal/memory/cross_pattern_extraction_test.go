package memory

import (
	"context"
	"os"
	"testing"
)

func TestPatternExtractor(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pilot-test-extractor-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	patternStore, err := NewGlobalPatternStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create pattern store: %v", err)
	}

	extractor := NewPatternExtractor(patternStore, store)
	ctx := context.Background()

	// Create a completed execution with pattern-rich output
	exec := &Execution{
		ID:          "extract_test_1",
		TaskID:      "task_1",
		ProjectPath: "/test/project",
		Status:      "completed",
		Output:      "Using context.Context in handlers. Added error handling for GetUser. Created tests for auth module.",
	}

	result, err := extractor.ExtractFromExecution(ctx, exec)
	if err != nil {
		t.Fatalf("ExtractFromExecution failed: %v", err)
	}

	if len(result.Patterns) == 0 {
		t.Error("expected patterns to be extracted")
	}

	// Test extraction from error output
	failedExec := &Execution{
		ID:          "extract_test_2",
		TaskID:      "task_2",
		ProjectPath: "/test/project",
		Status:      "completed",
		Output:      "Build succeeded",
		Error:       "panic: nil pointer dereference",
	}

	errorResult, err := extractor.ExtractFromExecution(ctx, failedExec)
	if err != nil {
		t.Fatalf("ExtractFromExecution (error) failed: %v", err)
	}

	if len(errorResult.AntiPatterns) == 0 {
		t.Error("expected anti-patterns to be extracted from error")
	}
}

func TestPatternQueryService(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pilot-test-query-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create test patterns
	patterns := []*CrossPattern{
		{ID: "q1", Type: "code", Title: "High confidence", Confidence: 0.9, Occurrences: 10, Scope: "org"},
		{ID: "q2", Type: "code", Title: "Low confidence", Confidence: 0.4, Occurrences: 2, Scope: "org"},
		{ID: "q3", Type: "workflow", Title: "Medium confidence", Confidence: 0.7, Occurrences: 5, Scope: "org"},
	}

	for _, p := range patterns {
		if err := store.SaveCrossPattern(p); err != nil {
			t.Fatalf("SaveCrossPattern failed: %v", err)
		}
	}

	queryService := NewPatternQueryService(store)
	ctx := context.Background()

	// Test query with minimum confidence
	result, err := queryService.Query(ctx, &PatternQuery{
		MinConfidence: 0.6,
		MaxResults:    10,
	})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(result.Patterns) != 2 {
		t.Errorf("expected 2 patterns with confidence >= 0.6, got %d", len(result.Patterns))
	}

	// Verify sorted by confidence
	if result.Patterns[0].Confidence < result.Patterns[1].Confidence {
		t.Error("patterns should be sorted by confidence descending")
	}

	// Test FormatForPrompt
	promptBlock, err := queryService.FormatForPrompt(ctx, "/test/project", "feat", "implementing a new handler")
	if err != nil {
		t.Fatalf("FormatForPrompt failed: %v", err)
	}

	if promptBlock == "" {
		t.Log("No patterns formatted (may be expected if filtering)")
	}
}

func TestLearningLoop(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pilot-test-learning-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	patternStore, err := NewGlobalPatternStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create pattern store: %v", err)
	}

	extractor := NewPatternExtractor(patternStore, store)
	learner := NewLearningLoop(store, extractor, nil)
	ctx := context.Background()

	// Create a pattern
	pattern := &CrossPattern{
		ID:         "learn_test_1",
		Type:       "code",
		Title:      "Test pattern",
		Confidence: 0.6,
		Scope:      "org",
	}
	if err := store.SaveCrossPattern(pattern); err != nil {
		t.Fatalf("SaveCrossPattern failed: %v", err)
	}
	if err := store.LinkPatternToProject("learn_test_1", "/test/project"); err != nil {
		t.Fatalf("LinkPatternToProject failed: %v", err)
	}

	// Create execution
	exec := &Execution{
		ID:          "learn_exec_1",
		TaskID:      "task_1",
		ProjectPath: "/test/project",
		Status:      "completed",
	}
	if err := store.SaveExecution(exec); err != nil {
		t.Fatalf("SaveExecution failed: %v", err)
	}

	// Record execution with applied patterns
	if err := learner.RecordExecution(ctx, exec, []string{"learn_test_1"}); err != nil {
		t.Fatalf("RecordExecution failed: %v", err)
	}

	// Check pattern performance
	perf, err := learner.GetPatternPerformance(ctx, "learn_test_1")
	if err != nil {
		t.Fatalf("GetPatternPerformance failed: %v", err)
	}

	if perf.SuccessCount != 1 {
		t.Errorf("expected 1 success count, got %d", perf.SuccessCount)
	}
}

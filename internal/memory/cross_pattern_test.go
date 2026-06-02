package memory

import (
	"context"
	"os"
	"testing"
)

func TestCrossPatternCRUD(t *testing.T) {
	// Create temp directory for test database
	tmpDir, err := os.MkdirTemp("", "pilot-test-cross-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()

	// Test SaveCrossPattern
	pattern := &CrossPattern{
		ID:          "test_pattern_1",
		Type:        "code",
		Title:       "Use context.Context",
		Description: "Always pass context for cancellation",
		Context:     "Go handlers",
		Examples:    []string{"ctx, cancel := context.WithTimeout(...)"},
		Confidence:  0.75,
		Occurrences: 3,
		Scope:       "org",
	}

	if err := store.SaveCrossPattern(pattern); err != nil {
		t.Fatalf("SaveCrossPattern failed: %v", err)
	}

	// Test GetCrossPattern
	retrieved, err := store.GetCrossPattern("test_pattern_1")
	if err != nil {
		t.Fatalf("GetCrossPattern failed: %v", err)
	}

	if retrieved.Title != "Use context.Context" {
		t.Errorf("expected title 'Use context.Context', got '%s'", retrieved.Title)
	}
	if retrieved.Confidence != 0.75 {
		t.Errorf("expected confidence 0.75, got %f", retrieved.Confidence)
	}
	if len(retrieved.Examples) != 1 {
		t.Errorf("expected 1 example, got %d", len(retrieved.Examples))
	}

	// Test GetCrossPatternsByType
	patterns, err := store.GetCrossPatternsByType("code")
	if err != nil {
		t.Fatalf("GetCrossPatternsByType failed: %v", err)
	}
	if len(patterns) != 1 {
		t.Errorf("expected 1 pattern, got %d", len(patterns))
	}

	// Test SearchCrossPatterns
	searchResults, err := store.SearchCrossPatterns("context", 10)
	if err != nil {
		t.Fatalf("SearchCrossPatterns failed: %v", err)
	}
	if len(searchResults) != 1 {
		t.Errorf("expected 1 search result, got %d", len(searchResults))
	}

	// Test DeleteCrossPattern
	if err := store.DeleteCrossPattern("test_pattern_1"); err != nil {
		t.Fatalf("DeleteCrossPattern failed: %v", err)
	}

	_, err = store.GetCrossPattern("test_pattern_1")
	if err == nil {
		t.Error("expected error after delete, got nil")
	}

	_ = ctx
}

func TestPatternProjectLink(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pilot-test-link-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create a pattern
	pattern := &CrossPattern{
		ID:          "link_test_pattern",
		Type:        "workflow",
		Title:       "Run tests",
		Description: "Always run tests before commit",
		Confidence:  0.8,
		Scope:       "org",
	}
	if err := store.SaveCrossPattern(pattern); err != nil {
		t.Fatalf("SaveCrossPattern failed: %v", err)
	}

	// Link to multiple projects
	if err := store.LinkPatternToProject("link_test_pattern", "/project/a"); err != nil {
		t.Fatalf("LinkPatternToProject failed: %v", err)
	}
	if err := store.LinkPatternToProject("link_test_pattern", "/project/b"); err != nil {
		t.Fatalf("LinkPatternToProject failed: %v", err)
	}
	// Link same project again (should increment uses)
	if err := store.LinkPatternToProject("link_test_pattern", "/project/a"); err != nil {
		t.Fatalf("LinkPatternToProject (duplicate) failed: %v", err)
	}

	// Get projects for pattern
	links, err := store.GetProjectsForPattern("link_test_pattern")
	if err != nil {
		t.Fatalf("GetProjectsForPattern failed: %v", err)
	}
	if len(links) != 2 {
		t.Errorf("expected 2 project links, got %d", len(links))
	}

	// Check that project/a has 2 uses
	for _, link := range links {
		if link.ProjectPath == "/project/a" && link.Uses != 2 {
			t.Errorf("expected project/a to have 2 uses, got %d", link.Uses)
		}
	}
}

func TestCrossPatternStats(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pilot-test-stats-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create various patterns
	patterns := []*CrossPattern{
		{ID: "p1", Type: "code", Title: "Pattern 1", Confidence: 0.8, Scope: "org"},
		{ID: "p2", Type: "code", Title: "Pattern 2", Confidence: 0.7, Scope: "org"},
		{ID: "p3", Type: "workflow", Title: "Workflow 1", Confidence: 0.9, Scope: "org"},
		{ID: "p4", Type: "error", Title: "Anti 1", Confidence: 0.75, IsAntiPattern: true, Scope: "org"},
	}

	for _, p := range patterns {
		if err := store.SaveCrossPattern(p); err != nil {
			t.Fatalf("SaveCrossPattern failed: %v", err)
		}
	}

	stats, err := store.GetCrossPatternStats()
	if err != nil {
		t.Fatalf("GetCrossPatternStats failed: %v", err)
	}

	if stats.TotalPatterns != 4 {
		t.Errorf("expected 4 total patterns, got %d", stats.TotalPatterns)
	}
	if stats.Patterns != 3 {
		t.Errorf("expected 3 regular patterns, got %d", stats.Patterns)
	}
	if stats.AntiPatterns != 1 {
		t.Errorf("expected 1 anti-pattern, got %d", stats.AntiPatterns)
	}
	if stats.ByType["code"] != 2 {
		t.Errorf("expected 2 code patterns, got %d", stats.ByType["code"])
	}
}

func TestDeleteCrossPatternCascade(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pilot-test-cascade-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Insert a cross_pattern with child rows in pattern_projects and pattern_feedback.
	pattern := &CrossPattern{
		ID:         "cascade_pattern_1",
		Type:       "code",
		Title:      "Cascade test",
		Confidence: 0.7,
		Scope:      "org",
	}
	if err := store.SaveCrossPattern(pattern); err != nil {
		t.Fatalf("SaveCrossPattern: %v", err)
	}
	if err := store.LinkPatternToProject("cascade_pattern_1", "/cascade/project"); err != nil {
		t.Fatalf("LinkPatternToProject: %v", err)
	}

	exec := &Execution{
		ID:          "cascade_exec_1",
		TaskID:      "cascade_task_1",
		ProjectPath: "/cascade/project",
		Status:      "completed",
	}
	if err := store.SaveExecution(exec); err != nil {
		t.Fatalf("SaveExecution: %v", err)
	}
	if err := store.RecordPatternFeedback(&PatternFeedback{
		PatternID:       "cascade_pattern_1",
		ExecutionID:     "cascade_exec_1",
		ProjectPath:     "/cascade/project",
		Outcome:         "success",
		ConfidenceDelta: 0.05,
	}); err != nil {
		t.Fatalf("RecordPatternFeedback: %v", err)
	}

	// Sanity-check: children exist before delete.
	links, err := store.GetProjectsForPattern("cascade_pattern_1")
	if err != nil {
		t.Fatalf("GetProjectsForPattern (before): %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 project link before delete, got %d", len(links))
	}

	// Delete the parent.
	if err := store.DeleteCrossPattern("cascade_pattern_1"); err != nil {
		t.Fatalf("DeleteCrossPattern: %v", err)
	}

	// pattern_projects children must be gone (FK cascade).
	links, err = store.GetProjectsForPattern("cascade_pattern_1")
	if err != nil {
		t.Fatalf("GetProjectsForPattern (after): %v", err)
	}
	if len(links) != 0 {
		t.Errorf("expected 0 pattern_projects rows after cascade delete, got %d", len(links))
	}

	// pattern_feedback children must be gone (FK cascade).
	var feedbackCount int
	if err := store.db.QueryRow(
		`SELECT COUNT(*) FROM pattern_feedback WHERE pattern_id = ?`, "cascade_pattern_1",
	).Scan(&feedbackCount); err != nil {
		t.Fatalf("count pattern_feedback: %v", err)
	}
	if feedbackCount != 0 {
		t.Errorf("expected 0 pattern_feedback rows after cascade delete, got %d", feedbackCount)
	}
}

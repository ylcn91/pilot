package memory

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestPatternCRUD(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	pattern := &Pattern{
		ProjectPath: "/path/to/project",
		Type:        "code",
		Content:     "Always use error wrapping",
		Confidence:  0.9,
	}

	if err := store.SavePattern(pattern); err != nil {
		t.Fatalf("SavePattern failed: %v", err)
	}

	if pattern.ID == 0 {
		t.Error("Pattern ID not set after save")
	}

	patterns, err := store.GetPatterns("/path/to/project")
	if err != nil {
		t.Fatalf("GetPatterns failed: %v", err)
	}

	if len(patterns) != 1 {
		t.Errorf("Expected 1 pattern, got %d", len(patterns))
	}
}

func TestPattern_Update(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	// Create pattern
	pattern := &Pattern{
		ProjectPath: "/path/to/project",
		Type:        "code",
		Content:     "Original content",
		Confidence:  0.7,
	}

	if err := store.SavePattern(pattern); err != nil {
		t.Fatalf("SavePattern (create) failed: %v", err)
	}

	originalID := pattern.ID
	if originalID == 0 {
		t.Fatal("Pattern ID should be set after create")
	}

	// Update pattern
	pattern.Content = "Updated content"
	pattern.Confidence = 0.9

	if err := store.SavePattern(pattern); err != nil {
		t.Fatalf("SavePattern (update) failed: %v", err)
	}

	// Verify update
	patterns, err := store.GetPatterns("/path/to/project")
	if err != nil {
		t.Fatalf("GetPatterns failed: %v", err)
	}

	if len(patterns) != 1 {
		t.Fatalf("Expected 1 pattern, got %d", len(patterns))
	}

	if patterns[0].Content != "Updated content" {
		t.Errorf("Content = %q, want 'Updated content'", patterns[0].Content)
	}
	if patterns[0].Confidence != 0.9 {
		t.Errorf("Confidence = %f, want 0.9", patterns[0].Confidence)
	}
}

func TestGetCrossPattern_InvalidExamplesJSON(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	// Insert pattern with invalid examples JSON
	_, err := store.db.Exec(`
		INSERT INTO cross_patterns (id, pattern_type, title, description, context, examples, confidence, occurrences, is_anti_pattern, scope)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "pat-1", "testing", "Test Pattern", "desc", "ctx", "invalid[json", 0.9, 5, false, "global")
	if err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	var buf bytes.Buffer
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(oldLogger)

	pattern, err := store.GetCrossPattern("pat-1")
	if err != nil {
		t.Errorf("GetCrossPattern should not error: %v", err)
	}
	if pattern == nil {
		t.Fatal("pattern should not be nil")
	}
	if pattern.Examples != nil {
		t.Errorf("Examples should be nil after unmarshal failure")
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "failed to unmarshal cross pattern examples") {
		t.Errorf("expected warning log, got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "pat-1") {
		t.Errorf("expected pattern ID in log, got: %s", logOutput)
	}
}

func TestScanCrossPatterns_InvalidExamplesJSON(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	// Insert patterns with valid and invalid examples
	_, _ = store.db.Exec(`
		INSERT INTO cross_patterns (id, pattern_type, title, description, context, examples, confidence, occurrences, is_anti_pattern, scope)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "pat-valid", "testing", "Valid", "desc", "ctx", `["example1","example2"]`, 0.9, 3, false, "global")
	_, _ = store.db.Exec(`
		INSERT INTO cross_patterns (id, pattern_type, title, description, context, examples, confidence, occurrences, is_anti_pattern, scope)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "pat-invalid", "testing", "Invalid", "desc", "ctx", "{broken", 0.8, 2, false, "global")

	var buf bytes.Buffer
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(oldLogger)

	patterns, err := store.GetCrossPatternsByType("testing")
	if err != nil {
		t.Errorf("GetCrossPatternsByType should not error: %v", err)
	}
	if len(patterns) != 2 {
		t.Errorf("expected 2 patterns, got %d", len(patterns))
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "failed to unmarshal cross pattern examples") {
		t.Errorf("expected warning log, got: %s", logOutput)
	}
}

func TestGetTopCrossPatterns(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	// Create patterns with varying confidence
	patterns := []*CrossPattern{
		{ID: "high", Type: "code", Title: "High Confidence", Confidence: 0.95, Occurrences: 10, Scope: "org"},
		{ID: "medium", Type: "code", Title: "Medium Confidence", Confidence: 0.7, Occurrences: 5, Scope: "org"},
		{ID: "low", Type: "code", Title: "Low Confidence", Confidence: 0.4, Occurrences: 2, Scope: "org"},
	}

	for _, p := range patterns {
		_ = store.SaveCrossPattern(p)
	}

	tests := []struct {
		name          string
		limit         int
		minConfidence float64
		wantCount     int
	}{
		{name: "all patterns", limit: 10, minConfidence: 0, wantCount: 3},
		{name: "high confidence only", limit: 10, minConfidence: 0.9, wantCount: 1},
		{name: "medium and above", limit: 10, minConfidence: 0.6, wantCount: 2},
		{name: "limited results", limit: 2, minConfidence: 0, wantCount: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := store.GetTopCrossPatterns(tt.limit, tt.minConfidence)
			if err != nil {
				t.Fatalf("GetTopCrossPatterns failed: %v", err)
			}

			if len(results) != tt.wantCount {
				t.Errorf("got %d patterns, want %d", len(results), tt.wantCount)
			}
		})
	}
}

func TestGetCrossPatternsForProject(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	// Create patterns with different scopes
	_ = store.SaveCrossPattern(&CrossPattern{ID: "org-1", Type: "code", Title: "Org Pattern", Scope: "org"})
	_ = store.SaveCrossPattern(&CrossPattern{ID: "global-1", Type: "code", Title: "Global Pattern", Scope: "global"})
	_ = store.SaveCrossPattern(&CrossPattern{ID: "project-1", Type: "code", Title: "Project Pattern", Scope: "project"})

	// Link project pattern
	_ = store.LinkPatternToProject("project-1", "/project/a")

	tests := []struct {
		name          string
		projectPath   string
		includeGlobal bool
		wantMin       int
	}{
		{name: "with global", projectPath: "/project/a", includeGlobal: true, wantMin: 2},
		{name: "without global", projectPath: "/project/a", includeGlobal: false, wantMin: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := store.GetCrossPatternsForProject(tt.projectPath, tt.includeGlobal)
			if err != nil {
				t.Fatalf("GetCrossPatternsForProject failed: %v", err)
			}

			if len(results) < tt.wantMin {
				t.Errorf("got %d patterns, want at least %d", len(results), tt.wantMin)
			}
		})
	}
}

func TestCrossPatternsIndexes(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pilot-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Insert test data so the planner has something to work with
	pattern := &CrossPattern{
		ID:    "test-idx-1",
		Type:  "naming",
		Title: "Use camelCase",
		Scope: "org",
	}
	if err := store.SaveCrossPattern(pattern); err != nil {
		t.Fatalf("SaveCrossPattern failed: %v", err)
	}

	tests := []struct {
		name  string
		query string
		index string
	}{
		{
			name:  "scope filter uses index",
			query: `EXPLAIN QUERY PLAN SELECT * FROM cross_patterns WHERE scope = 'org'`,
			index: "idx_cross_patterns_scope",
		},
		{
			name:  "updated_at filter uses index",
			query: `EXPLAIN QUERY PLAN SELECT * FROM cross_patterns WHERE updated_at > '2025-01-01'`,
			index: "idx_cross_patterns_updated",
		},
		{
			name:  "title filter uses index",
			query: `EXPLAIN QUERY PLAN SELECT * FROM cross_patterns WHERE title = 'Use camelCase'`,
			index: "idx_cross_patterns_title",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := store.db.Query(tt.query)
			if err != nil {
				t.Fatalf("EXPLAIN QUERY PLAN failed: %v", err)
			}
			defer func() { _ = rows.Close() }()

			var plan strings.Builder
			for rows.Next() {
				var id, parent, notused int
				var detail string
				if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
					t.Fatalf("scan failed: %v", err)
				}
				_, _ = fmt.Fprintf(&plan, "%s\n", detail)
			}
			if err := rows.Err(); err != nil {
				t.Fatalf("rows iteration failed: %v", err)
			}

			if !strings.Contains(plan.String(), tt.index) {
				t.Errorf("expected query plan to use %s, got:\n%s", tt.index, plan.String())
			}
		})
	}
}

// TestRecordPatternFeedback_TransactionAtomicity verifies D6: all three writes
// inside RecordPatternFeedback succeed together — the feedback row, the
// confidence update, and the project-link update.
func TestRecordPatternFeedback_TransactionAtomicity(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Prerequisites: a cross pattern and a project link must exist for the
	// confidence/link UPDATE statements to match rows.
	pattern := &CrossPattern{
		ID:          "pat-tx-1",
		Type:        "code",
		Title:       "test pattern",
		Description: "desc",
		Confidence:  0.5,
		Occurrences: 1,
		Scope:       "org",
	}
	if err := store.SaveCrossPattern(pattern); err != nil {
		t.Fatalf("SaveCrossPattern: %v", err)
	}
	if err := store.LinkPatternToProject("pat-tx-1", "/proj/tx"); err != nil {
		t.Fatalf("LinkPatternToProject: %v", err)
	}
	// Seed an execution row so the FK constraint on pattern_feedback is satisfied.
	if err := store.SaveExecution(&Execution{ID: "exec-tx-1", TaskID: "GH-TX", ProjectPath: "/proj/tx", Status: "completed"}); err != nil {
		t.Fatalf("SaveExecution: %v", err)
	}

	fb := &PatternFeedback{
		PatternID:       "pat-tx-1",
		ExecutionID:     "exec-tx-1",
		ProjectPath:     "/proj/tx",
		Outcome:         "success",
		ConfidenceDelta: 0.1,
	}
	if err := store.RecordPatternFeedback(fb); err != nil {
		t.Fatalf("RecordPatternFeedback: %v", err)
	}
	if fb.ID == 0 {
		t.Error("expected feedback ID to be set after insert")
	}

	// Confidence should have increased.
	updated, err := store.GetCrossPattern("pat-tx-1")
	if err != nil {
		t.Fatalf("GetCrossPattern: %v", err)
	}
	if updated.Confidence <= 0.5 {
		t.Errorf("confidence = %.3f, want > 0.5", updated.Confidence)
	}

	// Project link success_count should be 1.
	links, err := store.GetProjectsForPattern("pat-tx-1")
	if err != nil {
		t.Fatalf("GetProjectsForPattern: %v", err)
	}
	if len(links) == 0 || links[0].SuccessCount != 1 {
		t.Errorf("success_count = %d, want 1", func() int {
			if len(links) > 0 {
				return links[0].SuccessCount
			}
			return -1
		}())
	}
}

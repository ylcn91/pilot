package memory

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestSaveExtractedPatterns(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "extractor-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	patternStore, _ := NewGlobalPatternStore(tmpDir)
	extractor := NewPatternExtractor(patternStore, store)
	ctx := context.Background()

	result := &ExtractionResult{
		ExecutionID: "test-exec",
		ProjectPath: "/test/project",
		Patterns: []*ExtractedPattern{
			{
				Type:        PatternTypeCode,
				Title:       "Test Pattern",
				Description: "A test pattern",
				Examples:    []string{"example1"},
				Confidence:  0.8,
				Context:     "Go code",
			},
		},
		AntiPatterns: []*ExtractedPattern{
			{
				Type:        PatternTypeError,
				Title:       "Nil pointer",
				Description: "Nil pointer dereference",
				Confidence:  0.7,
			},
		},
	}

	if err := extractor.SaveExtractedPatterns(ctx, result); err != nil {
		t.Fatalf("SaveExtractedPatterns() error = %v", err)
	}

	// Verify patterns were saved (1 pattern + 1 anti-pattern)
	if patternStore.Count() != 2 {
		t.Errorf("PatternStore.Count() = %d, want 2", patternStore.Count())
	}

	// Verify anti-pattern has correct prefix
	errorPatterns := patternStore.GetByType(PatternTypeError)
	if len(errorPatterns) != 1 {
		t.Fatalf("Expected 1 error pattern, got %d", len(errorPatterns))
	}
	if !strings.HasPrefix(errorPatterns[0].Title, "[ANTI]") {
		t.Errorf("Anti-pattern title should have [ANTI] prefix, got %q", errorPatterns[0].Title)
	}
}

func TestExtractAndSave(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "extractor-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	patternStore, _ := NewGlobalPatternStore(tmpDir)
	extractor := NewPatternExtractor(patternStore, store)
	ctx := context.Background()

	exec := &Execution{
		ID:          "extract-save-exec",
		ProjectPath: "/test/project",
		Status:      "completed",
		Output:      "using context.Context in Handler added error handling for Validate",
		Error:       "",
	}

	if err := extractor.ExtractAndSave(ctx, exec); err != nil {
		t.Fatalf("ExtractAndSave() error = %v", err)
	}

	// Should have extracted at least the context pattern
	if patternStore.Count() < 1 {
		t.Errorf("Expected at least 1 pattern to be saved, got %d", patternStore.Count())
	}
}

func TestExtractAndSave_NoPatterns(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "extractor-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	patternStore, _ := NewGlobalPatternStore(tmpDir)
	extractor := NewPatternExtractor(patternStore, store)
	ctx := context.Background()

	exec := &Execution{
		ID:          "no-patterns-exec",
		ProjectPath: "/test/project",
		Status:      "completed",
		Output:      "Just built the binary.",
	}

	// ExtractAndSave with no patterns should not call save, so no deadlock
	err = extractor.ExtractAndSave(ctx, exec)
	if err != nil {
		t.Fatalf("ExtractAndSave failed: %v", err)
	}

	// Should not error even with no patterns - no save was triggered
	if patternStore.Count() != 0 {
		t.Error("expected no patterns to be saved")
	}
}

func TestMergePattern_DeduplicatesProjects(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "extractor-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	patternStore, _ := NewGlobalPatternStore(tmpDir)
	extractor := NewPatternExtractor(patternStore, store)
	ctx := context.Background()

	// Save pattern twice from same project
	result := &ExtractionResult{
		ExecutionID: "test-exec",
		ProjectPath: "/test/project",
		Patterns: []*ExtractedPattern{
			{Type: PatternTypeCode, Title: "Dedupe Test", Description: "Test", Confidence: 0.8},
		},
	}

	if err := extractor.SaveExtractedPatterns(ctx, result); err != nil {
		t.Fatalf("SaveExtractedPatterns() first call error = %v", err)
	}

	// Save again with same project
	if err := extractor.SaveExtractedPatterns(ctx, result); err != nil {
		t.Fatalf("SaveExtractedPatterns() second call error = %v", err)
	}

	// Should still only have 1 pattern with 1 project (deduplicated)
	patterns := patternStore.GetByType(PatternTypeCode)
	if len(patterns) != 1 {
		t.Fatalf("Expected 1 pattern, got %d", len(patterns))
	}

	if len(patterns[0].Projects) != 1 {
		t.Errorf("Expected 1 project (deduplicated), got %d", len(patterns[0].Projects))
	}
}

func TestMergePattern_AddsNewProjects(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "extractor-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	patternStore, _ := NewGlobalPatternStore(tmpDir)
	extractor := NewPatternExtractor(patternStore, store)
	ctx := context.Background()

	// Save pattern from first project
	result1 := &ExtractionResult{
		ExecutionID: "exec-1",
		ProjectPath: "/project/one",
		Patterns: []*ExtractedPattern{
			{Type: PatternTypeCode, Title: "Multi-Project Pattern", Description: "Test", Confidence: 0.8},
		},
	}

	if err := extractor.SaveExtractedPatterns(ctx, result1); err != nil {
		t.Fatalf("SaveExtractedPatterns() first project error = %v", err)
	}

	// Save same pattern from second project
	result2 := &ExtractionResult{
		ExecutionID: "exec-2",
		ProjectPath: "/project/two",
		Patterns: []*ExtractedPattern{
			{Type: PatternTypeCode, Title: "Multi-Project Pattern", Description: "Test", Confidence: 0.8},
		},
	}

	if err := extractor.SaveExtractedPatterns(ctx, result2); err != nil {
		t.Fatalf("SaveExtractedPatterns() second project error = %v", err)
	}

	// Should have 1 pattern with 2 projects
	patterns := patternStore.GetByType(PatternTypeCode)
	if len(patterns) != 1 {
		t.Fatalf("Expected 1 merged pattern, got %d", len(patterns))
	}

	if len(patterns[0].Projects) != 2 {
		t.Errorf("Expected 2 projects, got %d", len(patterns[0].Projects))
	}
}

func TestSaveExtractedPatterns_TierInfluencesSavedPattern(t *testing.T) {
	tests := []struct {
		name           string
		output         string
		wantTier       string
		wantConfidence float64
	}{
		{
			name:           "blocker tier raises confidence and persists tier",
			output:         "STANDARD_VIOLATION: blocker no-secrets — config.go:12",
			wantTier:       "blocker",
			wantConfidence: 0.9,
		},
		{
			name:           "must tier raises confidence",
			output:         "STANDARD_VIOLATION: must error-check — handler.go:88",
			wantTier:       "must",
			wantConfidence: 0.75,
		},
		{
			name:           "nice tier keeps baseline confidence but persists tier",
			output:         "STANDARD_VIOLATION: nice naming — store.go:5",
			wantTier:       "nice",
			wantConfidence: 0.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp("", "extractor-tier-*")
			if err != nil {
				t.Fatalf("failed to create temp dir: %v", err)
			}
			defer func() { _ = os.RemoveAll(tmpDir) }()

			store, _ := NewStore(tmpDir)
			defer func() { _ = store.Close() }()
			patternStore, _ := NewGlobalPatternStore(tmpDir)
			extractor := NewPatternExtractor(patternStore, store)
			ctx := context.Background()

			result, err := extractor.ExtractFromSelfReview(ctx, tt.output, "/test/project")
			if err != nil {
				t.Fatalf("ExtractFromSelfReview() error = %v", err)
			}
			if result.Tier != tt.wantTier {
				t.Fatalf("Tier = %q, want %q", result.Tier, tt.wantTier)
			}

			if err := extractor.SaveExtractedPatterns(ctx, result); err != nil {
				t.Fatalf("SaveExtractedPatterns() error = %v", err)
			}

			saved := patternStore.GetByType(PatternTypeWorkflow)
			if len(saved) != 1 {
				t.Fatalf("got %d saved patterns, want 1", len(saved))
			}
			got := saved[0]
			if got.Confidence != tt.wantConfidence {
				t.Errorf("saved confidence = %v, want %v", got.Confidence, tt.wantConfidence)
			}
			if got.Metadata["tier"] != tt.wantTier {
				t.Errorf("metadata tier = %v, want %q", got.Metadata["tier"], tt.wantTier)
			}
		})
	}
}

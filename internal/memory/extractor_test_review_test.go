package memory

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestExtractFromReviewComments_TestingFeedback(t *testing.T) {
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

	comments := []string{"add tests for edge cases", "test coverage is important"}

	result, err := extractor.ExtractFromReviewComments(ctx, comments, "/test/project")
	if err != nil {
		t.Fatalf("ExtractFromReviewComments failed: %v", err)
	}

	// Should extract testing anti-pattern
	if len(result.AntiPatterns) < 1 {
		t.Errorf("Expected at least 1 anti-pattern for testing feedback, got %d", len(result.AntiPatterns))
	}

	// Verify it's a workflow pattern
	found := false
	for _, p := range result.AntiPatterns {
		if p.Type == PatternTypeWorkflow {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected workflow anti-pattern for testing feedback")
	}
}

func TestExtractFromReviewComments_NamingFeedback(t *testing.T) {
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

	comments := []string{"unclear variable names", "please rename"}

	result, err := extractor.ExtractFromReviewComments(ctx, comments, "/test/project")
	if err != nil {
		t.Fatalf("ExtractFromReviewComments failed: %v", err)
	}

	// Should extract naming anti-pattern
	found := false
	for _, p := range result.AntiPatterns {
		if p.Type == PatternTypeNaming {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected naming anti-pattern for unclear names feedback")
	}
}

func TestExtractFromReviewComments_ErrorHandling(t *testing.T) {
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

	comments := []string{"unchecked error in line 42", "handle error cases"}

	result, err := extractor.ExtractFromReviewComments(ctx, comments, "/test/project")
	if err != nil {
		t.Fatalf("ExtractFromReviewComments failed: %v", err)
	}

	// Should extract error handling anti-pattern
	found := false
	for _, p := range result.AntiPatterns {
		if p.Type == PatternTypeError {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected error handling anti-pattern")
	}
}

func TestExtractFromReviewComments_PositiveFeedback(t *testing.T) {
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

	comments := []string{"nice approach", "well done implementation"}

	result, err := extractor.ExtractFromReviewComments(ctx, comments, "/test/project")
	if err != nil {
		t.Fatalf("ExtractFromReviewComments failed: %v", err)
	}

	// Should extract positive pattern, not anti-pattern
	if len(result.Patterns) < 1 {
		t.Errorf("Expected at least 1 positive pattern, got %d", len(result.Patterns))
	}

	if len(result.AntiPatterns) > 0 {
		t.Errorf("Expected no anti-patterns for positive feedback, got %d", len(result.AntiPatterns))
	}
}

func TestExtractFromReviewComments_NoPatterns(t *testing.T) {
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

	comments := []string{"looks good to merge"}

	result, err := extractor.ExtractFromReviewComments(ctx, comments, "/test/project")
	if err != nil {
		t.Fatalf("ExtractFromReviewComments failed: %v", err)
	}

	// No recognizable patterns
	if len(result.Patterns) > 0 || len(result.AntiPatterns) > 0 {
		t.Errorf("Expected no patterns for generic comment, got %d patterns and %d anti-patterns",
			len(result.Patterns), len(result.AntiPatterns))
	}
}

func TestExtractFromReviewComments_Documentation(t *testing.T) {
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

	comments := []string{"add documentation for this function", "please explain this logic"}

	result, err := extractor.ExtractFromReviewComments(ctx, comments, "/test/project")
	if err != nil {
		t.Fatalf("ExtractFromReviewComments failed: %v", err)
	}

	// Should extract documentation anti-pattern
	found := false
	for _, p := range result.AntiPatterns {
		if p.Type == PatternTypeCode && strings.Contains(strings.ToLower(p.Title), "doc") {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected documentation anti-pattern")
	}
}

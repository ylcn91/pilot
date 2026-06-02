package memory

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestLearnFromCIFailure(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "feedback-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	patternStore, _ := NewGlobalPatternStore(tmpDir)
	extractor := NewPatternExtractor(patternStore, store)
	loop := NewLearningLoop(store, extractor, nil)
	ctx := context.Background()

	ciLogs := `./internal/handler.go:15:2: undefined: processItem
--- FAIL: TestHandler (0.01s)
    handler_test.go:42: expected 200, got 500
golangci-lint: errcheck violation at store.go:55`

	err = loop.LearnFromCIFailure(ctx, "/test/project", ciLogs, []string{"build", "test", "lint"})
	if err != nil {
		t.Fatalf("LearnFromCIFailure failed: %v", err)
	}

	// Should have extracted CI-related anti-patterns
	if patternStore.Count() < 1 {
		t.Errorf("Expected at least 1 pattern from CI logs, got %d", patternStore.Count())
	}

	// Verify all saved patterns have [ANTI] prefix (CI patterns are anti-patterns)
	for _, pt := range []PatternType{PatternTypeError, PatternTypeWorkflow} {
		for _, p := range patternStore.GetByType(pt) {
			if !strings.HasPrefix(p.Title, "[ANTI]") {
				t.Errorf("CI pattern %q should have [ANTI] prefix", p.Title)
			}
		}
	}
}

func TestLearnFromCIFailure_EmptyLogs(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "feedback-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	patternStore, _ := NewGlobalPatternStore(tmpDir)
	extractor := NewPatternExtractor(patternStore, store)
	loop := NewLearningLoop(store, extractor, nil)
	ctx := context.Background()

	err = loop.LearnFromCIFailure(ctx, "/test/project", "", []string{"build"})
	if err != nil {
		t.Fatalf("LearnFromCIFailure with empty logs should not error: %v", err)
	}

	if patternStore.Count() != 0 {
		t.Errorf("Empty CI logs should produce no patterns, got %d", patternStore.Count())
	}
}

func TestLearnFromCIFailure_NoExtractor(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "feedback-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	loop := NewLearningLoop(store, nil, nil) // No extractor
	ctx := context.Background()

	err = loop.LearnFromCIFailure(ctx, "/test/project", "undefined: foo", []string{"build"})
	if err == nil {
		t.Error("LearnFromCIFailure should fail without extractor")
	}
}

func TestLearnFromCIFailure_NoMatchingPatterns(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "feedback-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	patternStore, _ := NewGlobalPatternStore(tmpDir)
	extractor := NewPatternExtractor(patternStore, store)
	loop := NewLearningLoop(store, extractor, nil)
	ctx := context.Background()

	err = loop.LearnFromCIFailure(ctx, "/test/project", "All checks passed", []string{"build"})
	if err != nil {
		t.Fatalf("LearnFromCIFailure with no matches should not error: %v", err)
	}

	if patternStore.Count() != 0 {
		t.Errorf("Non-matching CI logs should produce no patterns, got %d", patternStore.Count())
	}
}

func TestLearnFromCIFailure_CheckNameTagging(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "feedback-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	patternStore, _ := NewGlobalPatternStore(tmpDir)
	extractor := NewPatternExtractor(patternStore, store)
	loop := NewLearningLoop(store, extractor, nil)
	ctx := context.Background()

	err = loop.LearnFromCIFailure(ctx, "/test/project", "undefined: myFunc", []string{"go-build", "compile"})
	if err != nil {
		t.Fatalf("LearnFromCIFailure failed: %v", err)
	}

	// Verify patterns were saved (check names tag is embedded in Context during extraction,
	// then stored as metadata by SaveExtractedPatterns)
	if patternStore.Count() < 1 {
		t.Errorf("Expected at least 1 pattern, got %d", patternStore.Count())
	}
}

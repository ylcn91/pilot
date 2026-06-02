package memory

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestLearnFromDiff(t *testing.T) {
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

	// Provide a diff that matches pattern extraction rules
	diff := `+ using context.Context in Handler
+ added error handling for validateInput`

	err = loop.LearnFromDiff(ctx, "/test/project", diff, true)
	if err != nil {
		t.Fatalf("LearnFromDiff failed: %v", err)
	}

	// Should have extracted patterns from the diff
	if patternStore.Count() < 1 {
		t.Errorf("Expected at least 1 pattern to be extracted from diff, got %d", patternStore.Count())
	}
}

func TestLearnFromDiff_Failed(t *testing.T) {
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

	// Provide a diff with error patterns for failed execution
	diff := `- forgot to handle error
+ nil pointer dereference`

	// LearnFromDiff with success=false creates a "failed" execution
	// The extractor only works on "completed" executions, so this will error
	err = loop.LearnFromDiff(ctx, "/test/project", diff, false)
	if err == nil {
		t.Error("LearnFromDiff with failed execution should return error")
	}

	// Verify the error is about execution status
	if err != nil && !strings.Contains(err.Error(), "completed") {
		t.Errorf("Expected error about completed executions, got: %v", err)
	}
}

func TestLearnFromReview_ChangesRequested(t *testing.T) {
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

	reviews := []*ReviewData{
		{
			Body:     "add tests for edge cases",
			State:    "CHANGES_REQUESTED",
			Reviewer: "reviewer1",
		},
	}

	err = loop.LearnFromReview(ctx, "/test/project", reviews, "https://github.com/test/pr/1")
	if err != nil {
		t.Fatalf("LearnFromReview failed: %v", err)
	}

	// Should have extracted testing anti-pattern
	if patternStore.Count() < 1 {
		t.Errorf("Expected at least 1 pattern extracted from review, got %d", patternStore.Count())
	}

	// Verify anti-pattern was created
	patterns := patternStore.GetByType(PatternTypeWorkflow)
	if len(patterns) < 1 {
		t.Error("Expected workflow anti-pattern for testing feedback")
	}
}

func TestLearnFromReview_Approved(t *testing.T) {
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

	reviews := []*ReviewData{
		{
			Body:     "this is a nice approach to the problem",
			State:    "APPROVED",
			Reviewer: "reviewer2",
		},
	}

	err = loop.LearnFromReview(ctx, "/test/project", reviews, "https://github.com/test/pr/2")
	if err != nil {
		t.Fatalf("LearnFromReview failed: %v", err)
	}

	// Should have extracted positive pattern
	patterns := patternStore.GetByType(PatternTypeCode)
	if len(patterns) < 1 {
		t.Error("Expected positive pattern for approved review")
	}

	// Confidence should be boosted
	if len(patterns) > 0 && patterns[0].Confidence < 0.8 {
		t.Errorf("Approved pattern should have high confidence, got %f", patterns[0].Confidence)
	}
}

func TestLearnFromReview_EmptyBody(t *testing.T) {
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

	reviews := []*ReviewData{
		{
			Body:     "", // Empty body (approval click without text)
			State:    "APPROVED",
			Reviewer: "reviewer3",
		},
	}

	err = loop.LearnFromReview(ctx, "/test/project", reviews, "https://github.com/test/pr/3")
	if err != nil {
		t.Fatalf("LearnFromReview failed: %v", err)
	}

	// Empty review should be skipped
	if patternStore.Count() != 0 {
		t.Errorf("Empty body review should be skipped, but got %d patterns", patternStore.Count())
	}
}

func TestLearnFromReview_MultipleReviews(t *testing.T) {
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

	reviews := []*ReviewData{
		{
			Body:     "add tests and improve naming",
			State:    "CHANGES_REQUESTED",
			Reviewer: "reviewer1",
		},
		{
			Body:     "error handling looks good",
			State:    "APPROVED",
			Reviewer: "reviewer2",
		},
		{
			Body:     "",
			State:    "APPROVED",
			Reviewer: "reviewer3",
		},
	}

	err = loop.LearnFromReview(ctx, "/test/project", reviews, "https://github.com/test/pr/4")
	if err != nil {
		t.Fatalf("LearnFromReview failed: %v", err)
	}

	// Should have extracted multiple patterns
	if patternStore.Count() < 2 {
		t.Errorf("Expected at least 2 patterns from multiple reviews, got %d", patternStore.Count())
	}
}

func TestLearnFromReview_NoExtractor(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "feedback-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	loop := NewLearningLoop(store, nil, nil) // No extractor
	ctx := context.Background()

	reviews := []*ReviewData{
		{
			Body:     "add tests",
			State:    "CHANGES_REQUESTED",
			Reviewer: "reviewer",
		},
	}

	err = loop.LearnFromReview(ctx, "/test/project", reviews, "https://github.com/test/pr/5")
	if err == nil {
		t.Error("LearnFromReview should fail without extractor")
	}
}

func TestLearnFromReview_NoReviews(t *testing.T) {
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

	err = loop.LearnFromReview(ctx, "/test/project", nil, "https://github.com/test/pr/6")
	if err != nil {
		t.Fatalf("LearnFromReview with no reviews should not error: %v", err)
	}

	if patternStore.Count() != 0 {
		t.Errorf("No reviews should result in no patterns, got %d", patternStore.Count())
	}
}
